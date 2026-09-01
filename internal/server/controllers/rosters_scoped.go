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
// Returns an empty-but-valid PositionGroupView (Quantity 0, no
// candidates) if teamID/teamPositionName can't be resolved to a real
// needed-position or existing assignment at all - genuinely shouldn't
// happen from any of this page's own forms (they only ever post values
// the page itself just rendered), but mirrors groupNeededPositions' own
// "never silently drop data, never error out over a merge miss" stance
// rather than returning an error the caller would have nothing useful to
// do with.
func (sc *SchedulingController) buildRosterPositionGroupData(ctx context.Context, organizationID, serviceTypeID, planID, teamID, teamPositionName string) (views.PositionGroupView, string) {
	errMsg := ""

	roster, err := sc.store.GetOrCreateRoster(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		return views.PositionGroupView{TeamID: teamID, TeamPositionName: teamPositionName}, err.Error()
	}
	assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID)
	if err != nil {
		return views.PositionGroupView{TeamID: teamID, TeamPositionName: teamPositionName}, err.Error()
	}

	needed, err := pco.GetNeededPositions(ctx, serviceTypeID, planID, nil)
	if err != nil {
		errMsg = appendErr(errMsg, "loading needed positions from Planning Center: "+err.Error())
	}
	groups := groupNeededPositions(needed.Data, assignments)

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
	if members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil); err == nil {
		for _, m := range members.Data {
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
	// sidebar detail card) - GetTeams is a whole-service-type bulk call
	// like buildRosterBuilderData's, just used here for one lookup instead
	// of every team on the roster.
	teamName := teamID
	if teams, err := pco.GetTeams(ctx, serviceTypeID, nil); err == nil {
		for _, t := range teams.Data {
			if t.ID == teamID {
				teamName = t.Attributes.Name
				break
			}
		}
	}

	var candidates []pco.PersonTeamPositionAssignmentData
	if posID != "" {
		if people, err := pco.GetPersonTeamPositionAssignments(ctx, teamID, posID, nil); err != nil {
			errMsg = appendErr(errMsg, "loading eligible people from Planning Center: "+err.Error())
		} else {
			candidates = people.Data
		}
	}

	relevantPersonIDs := map[string]bool{}
	for _, ra := range g.Assignments {
		relevantPersonIDs[ra.PersonID] = true
	}
	for _, pa := range candidates {
		if pa.Relationships.Person.Data != nil {
			relevantPersonIDs[pa.Relationships.Person.Data.ID] = true
		}
	}
	relevantIDs := make([]string, 0, len(relevantPersonIDs))
	for id := range relevantPersonIDs {
		relevantIDs = append(relevantIDs, id)
	}
	names := personNameMapForIDs(ctx, relevantIDs)

	blockoutsByPerson := map[string][]pco.BlockoutData{}
	var blockoutsMu sync.Mutex
	scheduling.RunConcurrently(relevantIDs, func(id string) {
		bo, err := pco.GetBlockouts(ctx, id, &pco.BlockoutsParams{Filter: "future"})
		if err != nil {
			return
		}
		blockoutsMu.Lock()
		blockoutsByPerson[id] = bo.Data
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
			labels := crossTeamAssignmentsThisWeek(ctx, personID, planDate, planID)
			crossTeamMu.Lock()
			crossTeamCache[personID] = labels
			crossTeamMu.Unlock()
		})

		var otherDatesMu sync.Mutex
		scheduling.RunConcurrently(relevantIDs, func(personID string) {
			if schedulePreferenceFor(personID) == "" {
				return
			}
			dates := otherAssignmentDates(ctx, personID, teamID, teamPositionName, planID)
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

		pg.Assigned = append(pg.Assigned, views.AssignmentView{
			ID:               ra.ID,
			PersonID:         ra.PersonID,
			Name:             titleOrID(names, ra.PersonID),
			Status:           ra.Status,
			TeamID:           teamID,
			TeamName:         teamName,
			TeamPositionName: teamPositionName,
			Published:        ra.PCOPlanPersonID != "",
			Notes:            notesByPlanPersonID[ra.PCOPlanPersonID],
			FrequencyWarning: freqWarn,
			BlockoutWarning:  blockoutWarningIfKnown(planDate, blockoutsByPerson[ra.PersonID]),
			CrossTeamWarning: crossWarn,
		})
	}

	for _, pa := range candidates {
		if pa.Relationships.Person.Data == nil {
			continue
		}
		personID := pa.Relationships.Person.Data.ID
		if assignedHere[personID] {
			continue
		}

		var freqWarn, crossWarn string
		if !planDate.IsZero() {
			if pa.Attributes.SchedulePreference != "" {
				freqWarn = frequencyWarning(pa.Attributes.SchedulePreference, planDate, otherDatesCache[personID])
			}
			crossWarn = crossTeamWarning(crossTeamCache[personID])
		}

		pg.Candidates = append(pg.Candidates, views.CandidatePersonView{
			PersonID:           personID,
			Name:               titleOrID(names, personID),
			SchedulePreference: pa.Attributes.SchedulePreference,
			FrequencyWarning:   freqWarn,
			BlockoutWarning:    blockoutWarningIfKnown(planDate, blockoutsByPerson[personID]),
			CrossTeamWarning:   crossWarn,
		})
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
func (sc *SchedulingController) buildRosterLightData(ctx context.Context, organizationID, serviceTypeID, planID string) views.RosterBuilderData {
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

	if members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil); err == nil {
		if len(assignments) == 0 && data.PendingDeleteCount == 0 {
			data.ImportableAssignmentCount = len(members.Data)
		}
	}

	needed, err := pco.GetNeededPositions(ctx, serviceTypeID, planID, nil)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading needed positions from Planning Center: "+err.Error())
	}
	groups := groupNeededPositions(needed.Data, assignments)

	teamNames := map[string]string{}
	teamSequence := map[string]int{}
	if teams, err := pco.GetTeams(ctx, serviceTypeID, nil); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading teams from Planning Center: "+err.Error())
	} else {
		for _, t := range teams.Data {
			teamNames[t.ID] = t.Attributes.Name
			teamSequence[t.ID] = t.Attributes.Sequence
		}
	}

	names := personNameMapForIDs(ctx, assignmentPersonIDs(assignments))

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
