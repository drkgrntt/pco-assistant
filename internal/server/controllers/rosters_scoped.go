package controllers

// Scoped (narrow-fragment) roster data builders - the counterparts to
// buildRosterBuilderData (rosters_builder.go) that a single mutation
// handler (rosters_detail.go) uses instead of a full rebuild, so an
// add/remove/preference-change's PCO cost scales with the one position (or
// zero positions, for a relationship-only change) it actually touched,
// not with how many other positions this roster has. See rosters_detail.go
// for which handler uses which, and roster_builder.templ's
// PositionGroupElementID/positionGroupCard/rosterTop for the htmx
// fragments these feed.

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// rosterSharedPCOData holds the two Planning Center resources
// buildRosterPositionGroupData, buildRosterLightData, and
// buildRosterBuilderData's own full-rebuild path (rosters_builder.go) all
// need for the same plan - GetTeamMembers and GetNeededPositions - fetched
// (at most once per rosterSharedCacheTTL window, see
// rosters_shared_cache.go) by fetchRosterSharedPCOData and passed into
// whichever of those need it, instead of each caller repeating the same
// live requests. See rosters_detail.go's
// AddRosterAssignment/RemoveRosterAssignment/QuickAddPositionPreference,
// the three call sites that pass one already-fetched *rosterSharedPCOData
// into both scoped builders in a single request; every other caller
// (QuickAddPersonRelationship here, buildRosterBuilderData's full-rebuild
// path) only ever needs one of the two builders and passes nil instead
// (see each function's own nil handling below) - nil still goes through
// fetchRosterSharedPCOData's own cache, it just doesn't already have a
// value in hand to skip the call with.
//
// A standalone GetTeams call used to be a third resource here, fetched
// purely to resolve team ids to names/display order - eliminated by
// sideloading team on the GetNeededPositions call below instead (see
// docs/pco-api-risks.md's include=team finding). TeamInfo is the
// pre-parsed result of that sideload (see teamInfoFromIncluded,
// rosters_builder.go), so neither caller needs to touch Needed.Included
// directly.
//
// Each PCO response is paired with its own fetch error rather than a
// single combined one: buildRosterPositionGroupData and buildRosterLightData
// don't actually agree on which of these failures is worth surfacing in
// their own errMsg (buildRosterPositionGroupData silently drops a missing
// TeamInfo entry - the team name just falls back to teamID - while
// buildRosterLightData reports NeededErr, which now also covers a failed
// team sideload) - keeping the errors separate preserves that existing
// difference instead of flattening it. It's also why only a fully
// error-free rosterSharedPCOData is ever cached (see
// rosters_shared_cache.go) - a partial success cached as a whole would
// otherwise pin one call's failure onto the other's still-good data for
// the rest of the TTL window.
type rosterSharedPCOData struct {
	Members    pco.PlanPersonListResponse
	MembersErr error
	Needed     pco.NeededPositionListResponse
	NeededErr  error
	// TeamInfo holds each team's name/sequence, resolved from Needed's own
	// Included array (see teamInfoFromIncluded) - empty (not nil) when
	// NeededErr is set.
	TeamInfo map[string]teamInfo
}

// fetchRosterSharedPCODataLive fetches the two PCO resources
// rosterSharedPCOData holds, once each, always live - never cached itself.
// fetchRosterSharedPCOData (rosters_shared_cache.go) is the cache-aside
// wrapper every real caller in this package should use instead; this stays
// unexported and exists purely to back that wrapper's cache-miss path.
// Sequential, not fanned out via scheduling.RunConcurrently - that helper
// exists for fan-outs sized to "one call per roster member/position" (see
// its own doc comment), not a fixed couple of calls, and both functions
// previously made these same calls sequentially anyway, so there's no
// latency regression to guard against here.
func fetchRosterSharedPCODataLive(ctx context.Context, serviceTypeID, planID string) *rosterSharedPCOData {
	d := &rosterSharedPCOData{}
	d.Members, d.MembersErr = pco.GetTeamMembers(ctx, serviceTypeID, planID, nil)
	d.Needed, d.NeededErr = pco.GetNeededPositions(ctx, serviceTypeID, planID, &pco.NeededPositionsParams{Include: []string{"team"}})
	d.TeamInfo = teamInfoFromIncluded(d.Needed.Included)
	return d
}

// buildRosterPositionGroupData recomputes a single position group (team id
// + team position name) with the same candidate/warning fidelity a full
// buildRosterBuilderData gives every group on a full page load - PCO's own
// eligibility list for the position, plus per-person blockout/frequency/
// cross-team warnings - but pays that cost for only this one group instead
// of fanning out across the whole roster (buildRosterBuilderData's
// candidatesByGroup/blockoutsByPerson/crossTeamCache loops, each O(number
// of positions or number of roster members)). Reuses groupNeededPositions
// (rosters_builder.go) against a plan-wide GetNeededPositions call - a
// single, size-independent PCO call either way, not worth hand-rolling a
// filtered version of - to pick out just the group being asked for,
// including groupNeededPositions' own "assignment with no matching
// needed_position entry still gets a group" fallback.
//
// shared carries GetTeamMembers/GetNeededPositions (the latter with team
// sideloaded) already fetched by the caller (see rosterSharedPCOData) -
// pass nil to have this function fetch its own copy, for a caller that
// needs only this one function.
//
// Returns an empty-but-valid PositionGroupView (Quantity 0, no
// candidates) if teamID/teamPositionName can't be resolved to a real
// needed-position or existing assignment at all - genuinely shouldn't
// happen from any of this page's own forms (they only ever post values
// the page itself just rendered), but mirrors groupNeededPositions' own
// "never silently drop data, never error out over a merge miss" stance
// rather than returning an error the caller would have nothing useful to
// do with.
func (sc *SchedulingController) buildRosterPositionGroupData(ctx context.Context, organizationID, serviceTypeID, planID, teamID, teamPositionName string, shared *rosterSharedPCOData) (views.PositionGroupView, string) {
	errMsg := ""

	roster, err := sc.store.GetOrCreateRoster(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		return views.PositionGroupView{TeamID: teamID, TeamPositionName: teamPositionName}, err.Error()
	}
	assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID)
	if err != nil {
		return views.PositionGroupView{TeamID: teamID, TeamPositionName: teamPositionName}, err.Error()
	}

	if shared == nil {
		shared = fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	}

	if shared.NeededErr != nil {
		errMsg = appendErr(errMsg, "loading needed positions from Planning Center: "+shared.NeededErr.Error())
	}
	groups := groupNeededPositions(shared.Needed.Data, assignments)

	var g *positionGroup
	for i := range groups {
		if groups[i].TeamID == teamID && groups[i].TeamPositionName == teamPositionName {
			g = &groups[i]
			break
		}
	}
	if g == nil {
		return views.PositionGroupView{TeamID: teamID, TeamPositionName: teamPositionName}, errMsg
	}

	// Best-effort, same as buildRosterBuilderData's identical fetch - feeds
	// each assigned person's own AssignmentView.VocalRangeDisplay/VocalRange*
	// tap-to-hear fields below (personVocalRangeView).
	personVocalRanges, _ := sc.store.ListPersonVocalRanges(ctx, organizationID)

	var planDate time.Time
	if plan, err := pco.GetPlan(ctx, serviceTypeID, planID); err == nil {
		planDate = plan.Data.Attributes.SortDate
	} else {
		errMsg = appendErr(errMsg, "loading plan from Planning Center: "+err.Error())
	}

	// Same bulk read buildRosterBuilderData makes, for the same two
	// reasons (Notes display) - just missing ImportableAssignmentCount's
	// use of it, which is a roster-wide (not group-scoped) concern that
	// buildRosterLightData covers instead.
	notesByPlanPersonID := map[string]string{}
	if shared.MembersErr == nil {
		for _, m := range shared.Members.Data {
			notesByPlanPersonID[m.ID] = m.Attributes.Notes
		}
	}

	// team_position id isn't stored locally (see buildRosterBuilderData's
	// own positionID resolution) - resolved here the same way, just
	// scoped to this one team rather than every team on the roster.
	posID := ""
	if positions, err := pco.GetTeamPositions(ctx, teamID, nil); err != nil {
		errMsg = appendErr(errMsg, "loading team positions from Planning Center: "+err.Error())
	} else {
		for _, p := range positions.Data {
			if p.Attributes.Name == teamPositionName {
				posID = p.ID
				break
			}
		}
	}

	// One team's own name, for AssignmentView.TeamName (shown on the
	// sidebar detail card) - resolved from shared.TeamInfo (sideloaded on
	// the GetNeededPositions call above), falling back to teamID itself if
	// that team's entry didn't come through (e.g. NeededErr).
	teamName := teamID
	if info, ok := shared.TeamInfo[teamID]; ok {
		teamName = info.Name
	}

	var candidates []pco.PersonTeamPositionAssignmentData
	if posID != "" {
		if people, err := pco.GetPersonTeamPositionAssignments(ctx, teamID, posID, nil); err != nil {
			errMsg = appendErr(errMsg, "loading eligible people from Planning Center: "+err.Error())
		} else {
			candidates = people.Data
		}
	}

	// relevantPersonIDs is scoped to people actually assigned to this
	// position, NOT every candidate in PCO's eligibility pool for it - same
	// principle as buildRosterBuilderData's identical comment (rosters_builder.go)
	// and docs/pco-api-risks.md's cold-start-eligibility-pool addendum. namedIDs
	// stays unscoped (candidates' names still render) since only the
	// PCO-backed warning fetches below narrow.
	relevantPersonIDs := map[string]bool{}
	for _, ra := range g.Assignments {
		relevantPersonIDs[ra.PersonID] = true
	}
	relevantIDs := make([]string, 0, len(relevantPersonIDs))
	for id := range relevantPersonIDs {
		relevantIDs = append(relevantIDs, id)
	}

	namedPersonIDs := map[string]bool{}
	for id := range relevantPersonIDs {
		namedPersonIDs[id] = true
	}
	for _, pa := range candidates {
		if pa.Relationships.Person.Data != nil {
			namedPersonIDs[pa.Relationships.Person.Data.ID] = true
		}
	}
	namedIDs := make([]string, 0, len(namedPersonIDs))
	for id := range namedPersonIDs {
		namedIDs = append(namedIDs, id)
	}
	lookups := personLookupMapForIDs(ctx, namedIDs)
	names := make(map[string]string, len(lookups))
	for id, l := range lookups {
		names[id] = l.Name
	}

	// Cache-first read per relevant person - see personBlockouts
	// (rosters_warnings.go); no PCO call happens here, cache hit or not.
	blockoutsByPerson := map[string][]pco.BlockoutData{}
	var blockoutsMu sync.Mutex
	scheduling.RunConcurrently(relevantIDs, func(id string) {
		bo := sc.personBlockouts(ctx, organizationID, id)
		blockoutsMu.Lock()
		blockoutsByPerson[id] = bo
		blockoutsMu.Unlock()
	})

	schedulePreferenceFor := func(personID string) string {
		for _, pa := range candidates {
			if pa.Relationships.Person.Data != nil && pa.Relationships.Person.Data.ID == personID {
				return pa.Attributes.SchedulePreference
			}
		}
		return ""
	}

	crossTeamCache := map[string][]string{}
	otherDatesCache := map[string][]time.Time{}
	if !planDate.IsZero() {
		var crossTeamMu sync.Mutex
		scheduling.RunConcurrently(relevantIDs, func(personID string) {
			labels := sc.crossTeamAssignmentsThisWeek(ctx, organizationID, personID, planDate, planID)
			crossTeamMu.Lock()
			crossTeamCache[personID] = labels
			crossTeamMu.Unlock()
		})

		var otherDatesMu sync.Mutex
		scheduling.RunConcurrently(relevantIDs, func(personID string) {
			if schedulePreferenceFor(personID) == "" {
				return
			}
			dates := sc.otherAssignmentDates(ctx, organizationID, personID, teamID, teamPositionName, planID)
			otherDatesMu.Lock()
			otherDatesCache[personID] = dates
			otherDatesMu.Unlock()
		})
	}

	pg := views.PositionGroupView{
		TeamID:           teamID,
		TeamName:         teamName,
		TeamPositionName: teamPositionName,
		TeamPositionID:   posID,
		Quantity:         g.Quantity,
	}

	assignedHere := map[string]bool{}
	for _, ra := range g.Assignments {
		assignedHere[ra.PersonID] = true

		pref := schedulePreferenceFor(ra.PersonID)
		var freqWarn, crossWarn string
		if !planDate.IsZero() {
			if pref != "" {
				freqWarn = frequencyWarning(pref, planDate, otherDatesCache[ra.PersonID])
			}
			crossWarn = crossTeamWarning(crossTeamCache[ra.PersonID])
		}

		av := views.AssignmentView{
			ID:               ra.ID,
			PersonID:         ra.PersonID,
			Name:             titleOrID(names, ra.PersonID),
			AvatarURL:        lookups[ra.PersonID].AvatarURL,
			Status:           ra.Status,
			TeamID:           teamID,
			TeamName:         teamName,
			TeamPositionName: teamPositionName,
			Published:        ra.PCOPlanPersonID != "",
			Notes:            notesByPlanPersonID[ra.PCOPlanPersonID],
			FrequencyWarning: freqWarn,
			BlockoutWarning:  blockoutWarningIfKnown(planDate, blockoutsByPerson[ra.PersonID]),
			CrossTeamWarning: crossWarn,
		}
		if display, lowPC, lowOct, highPC, highOct, ok := personVocalRangeView(personVocalRanges, ra.PersonID); ok {
			av.VocalRangeDisplay = display
			av.VocalRangeHasPlayableNotes = true
			av.VocalRangeLowPitchClass = lowPC
			av.VocalRangeLowOctave = lowOct
			av.VocalRangeHighPitchClass = highPC
			av.VocalRangeHighOctave = highOct
		}
		pg.Assigned = append(pg.Assigned, av)
	}

	// Candidates deliberately don't get FrequencyWarning/BlockoutWarning/
	// CrossTeamWarning computed here - see buildRosterBuilderData's identical
	// comment (rosters_builder.go) and docs/pco-api-risks.md's cold-start
	// addendum. Fetched on demand instead via CandidateWarnings
	// (rosters_candidate_warnings.go).
	for _, pa := range candidates {
		if pa.Relationships.Person.Data == nil {
			continue
		}
		personID := pa.Relationships.Person.Data.ID
		if assignedHere[personID] {
			continue
		}

		pg.Candidates = append(pg.Candidates, candidatePersonView(personID, titleOrID(names, personID), lookups[personID].AvatarURL, pa.Attributes.SchedulePreference))
	}

	return pg, errMsg
}

// buildRosterLightData refreshes the roster builder's shared page chrome -
// the status chips, the "Team, at a glance" sidebar card, the publish
// panel, and the existing-conflicts/import banners (see roster_builder.
// templ's rosterTop/rosterTopOOB) - after a mutation scoped to one
// position group or to a person relationship. Deliberately skips
// buildRosterBuilderData's per-position eligibility/blockout/cross-team
// fan-out (candidatesByGroup et al): none of the fragments built from this
// data ever show a candidate, a suggestion, or a frequency/blockout/
// cross-team warning - only assignment counts and who's actually on the
// roster - so none of that PCO cost is needed to keep them accurate.
// Positions here carry Assigned only (no Candidates), matching exactly
// what TeamGlanceList/RosterStatusSummary/RosterPublishPanel read.
//
// Known gap (see this branch's own PR notes): data.Suggestions is
// deliberately left empty here, not recomputed - filtering suggestions
// down to "eligible for some open position" needs exactly the per-group
// eligibility fan-out this function exists to avoid. The "Recommended
// next" panel (RosterBuilderBodyContent) is only ever rendered as part of
// a *full* RosterBuilderBody response (initial load, Publish, Revert), so
// it simply isn't touched by any of the narrow-fragment mutation
// responses that use this instead - it can go stale (e.g. still show
// someone just added) until the next full render, but never disappears or
// renders with fabricated data. Store.AddPersonToPosition refuses a
// duplicate active assignment, so acting on a stale suggestion fails
// safely (an error message) rather than corrupting the roster.
//
// shared carries GetTeamMembers/GetNeededPositions (the latter with team
// sideloaded) already fetched by the caller (see rosterSharedPCOData) -
// pass nil to have this function fetch its own copy, for a caller
// (QuickAddPersonRelationship today) that needs only this one function.
func (sc *SchedulingController) buildRosterLightData(ctx context.Context, organizationID, serviceTypeID, planID string, shared *rosterSharedPCOData) views.RosterBuilderData {
	data := views.RosterBuilderData{ServiceTypeID: serviceTypeID, PlanID: planID, SchedulePreferenceOptions: schedulePreferenceOptions()}

	roster, err := sc.store.GetOrCreateRoster(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return data
	}

	assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return data
	}
	for _, ra := range assignments {
		if ra.PCOPlanPersonID == "" {
			data.UnpublishedCount++
		}
	}

	pending, err := sc.store.ListPendingRosterDeletions(ctx, roster.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	data.PendingDeleteCount = len(pending)

	if shared == nil {
		shared = fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	}

	if shared.MembersErr == nil {
		if len(assignments) == 0 && data.PendingDeleteCount == 0 {
			data.ImportableAssignmentCount = len(shared.Members.Data)
		}
	}

	if shared.NeededErr != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading needed positions from Planning Center: "+shared.NeededErr.Error())
	}
	groups := groupNeededPositions(shared.Needed.Data, assignments)

	// Team names/sequence come from shared.TeamInfo (sideloaded on the
	// GetNeededPositions call above) rather than a separate GetTeams call -
	// a fetch failure here is already covered by shared.NeededErr, reported
	// above.
	teamNames := map[string]string{}
	teamSequence := map[string]int{}
	for id, info := range shared.TeamInfo {
		teamNames[id] = info.Name
		teamSequence[id] = info.Sequence
	}

	assignedIDs := assignmentPersonIDs(assignments)
	names := personNameMapForIDs(ctx, assignedIDs)

	// See buildRosterBuilderData's identical comment - scoped here to just
	// the assigned people, since this lighter build (deliberately, per this
	// function's own doc comment) never loads candidates at all.
	if any, err := sc.store.AnyPersonDataSynced(ctx, organizationID, assignedIDs); err == nil {
		data.PersonDataSyncPending = len(assignedIDs) > 0 && !any
	}

	for _, g := range groups {
		pg := views.PositionGroupView{
			TeamID:           g.TeamID,
			TeamName:         titleOrID(teamNames, g.TeamID),
			TeamPositionName: g.TeamPositionName,
			Quantity:         g.Quantity,
		}
		for _, ra := range g.Assignments {
			pg.Assigned = append(pg.Assigned, views.AssignmentView{
				ID:               ra.ID,
				PersonID:         ra.PersonID,
				Name:             titleOrID(names, ra.PersonID),
				Status:           ra.Status,
				TeamID:           pg.TeamID,
				TeamName:         pg.TeamName,
				TeamPositionName: pg.TeamPositionName,
				Published:        ra.PCOPlanPersonID != "",
			})
		}
		if len(pg.Assigned) < pg.Quantity {
			data.OpenPositionCount++
		}
		data.Positions = append(data.Positions, pg)
	}

	sort.SliceStable(data.Positions, func(i, j int) bool {
		return positionTeamSequence(teamSequence, data.Positions[i].TeamID) < positionTeamSequence(teamSequence, data.Positions[j].TeamID)
	})

	if _, existing, err := sc.store.AvoidPairingConflicts(ctx, organizationID, roster.ID); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	} else {
		for _, ex := range existing {
			data.ExistingConflicts = append(data.ExistingConflicts, fmt.Sprintf(
				"%s conflicts with %s - both are already on this roster.",
				titleOrID(names, ex.PersonID), titleOrID(names, ex.ConflictsWith),
			))
		}
	}

	return data
}

// assignmentInGroup finds one assignment view by row id within a single
// PositionGroupView's Assigned list - the scoped equivalent of walking
// every position in a full RosterBuilderData, used by AddRosterAssignment
// to find the AssignmentView its own scoped group data just built for the
// person it added (see rosters_detail.go).
func assignmentInGroup(group views.PositionGroupView, assignmentID int64) *views.AssignmentView {
	for i := range group.Assigned {
		if group.Assigned[i].ID == assignmentID {
			return &group.Assigned[i]
		}
	}
	return nil
}
