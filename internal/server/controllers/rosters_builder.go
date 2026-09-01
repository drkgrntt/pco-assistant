package controllers

// Assembling views.RosterBuilderData: merging local roster assignments with
// PCO's needed-position/eligibility data, plus the pure grouping/parsing
// helpers this leans on.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func (sc *SchedulingController) buildRosterBuilderDataWithErr(ctx context.Context, organizationID, serviceTypeID, planID, errMsg string) views.RosterBuilderData {
	data := sc.buildRosterBuilderData(ctx, organizationID, serviceTypeID, planID)
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}
	return data
}

func assignmentPersonIDs(assignments []scheduling.RosterAssignment) []string {
	seen := map[string]bool{}
	var ids []string
	for _, a := range assignments {
		if !seen[a.PersonID] {
			seen[a.PersonID] = true
			ids = append(ids, a.PersonID)
		}
	}
	return ids
}

// positionGroup is one open slot - a team + position name, how many are
// needed, and which local assignments fill it. This is the roster page's
// equivalent of mergedRow: PCO's needed-position data merged with local
// roster_assignments rows.
type positionGroup struct {
	TeamID           string
	TeamPositionName string
	Quantity         int
	Assignments      []scheduling.RosterAssignment
}

// groupNeededPositions merges a plan's open slots (from PCO) with a
// roster's local assignments, grouped by (team_id, team_position_name). A
// position with assignments but no matching NeededPosition entry (e.g.
// Planning Center's "needed" count was never configured) still appears -
// data is never silently dropped, same principle as mergeServiceOrder.
func groupNeededPositions(needed []pco.NeededPositionData, assignments []scheduling.RosterAssignment) []positionGroup {
	type key struct{ teamID, positionName string }
	var order []key
	groups := map[key]*positionGroup{}

	group := func(teamID, positionName string) *positionGroup {
		k := key{teamID, positionName}
		g, ok := groups[k]
		if !ok {
			g = &positionGroup{TeamID: teamID, TeamPositionName: positionName}
			groups[k] = g
			order = append(order, k)
		}
		return g
	}

	for _, np := range needed {
		teamID := ""
		if np.Relationships.Team.Data != nil {
			teamID = np.Relationships.Team.Data.ID
		}
		group(teamID, np.Attributes.TeamPositionName).Quantity += np.Attributes.Quantity
	}

	for _, a := range assignments {
		g := group(a.TeamID, a.TeamPositionName)
		g.Assignments = append(g.Assignments, a)
	}

	out := make([]positionGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// unknownTeamSequence is the sort key positionTeamSequence returns for a
// team id with no entry in teamSequence (its Planning Center lookup
// failed) - deliberately larger than any real Team.Sequence rather than
// the zero value, so a lookup failure sorts that team's positions to the
// end of the roster instead of jumping them to the front.
const unknownTeamSequence = 1 << 30

// positionTeamSequence looks up teamID's PCO Team.Sequence for the
// team-group sort in buildRosterBuilderData - see unknownTeamSequence for
// the fallback.
func positionTeamSequence(teamSequence map[string]int, teamID string) int {
	if seq, ok := teamSequence[teamID]; ok {
		return seq
	}
	return unknownTeamSequence
}

// includedPlan is the shape this app reads back out of a JSON:API
// "included" array's raw entries (see planSortDatesFromIncluded) - just
// enough of Plan's shape to get each included plan's sort_date, since
// go-pco leaves Included as []any rather than a typed union.
type includedPlan struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		SortDate time.Time `json:"sort_date"`
	} `json:"attributes"`
}

// planSortDatesFromIncluded extracts plan id -> sort_date from a
// GetPersonPlanPeople response's Included array (fetched with
// Include: []string{"plan"}). Any entry that isn't a well-formed Plan is
// silently skipped - this feeds an advisory warning, not something that
// should fail the page over one malformed entry.
func planSortDatesFromIncluded(included []any) map[string]time.Time {
	dates := map[string]time.Time{}
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var p includedPlan
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		if p.Type != "Plan" || p.ID == "" || p.Attributes.SortDate.IsZero() {
			continue
		}
		dates[p.ID] = p.Attributes.SortDate
	}
	return dates
}

// includedTeam is the Team equivalent of includedPlan - just enough of
// Team's shape (see go-pco's TeamData) to resolve a team's name out of a
// GetPersonPlanPeople response's Included array when fetched with
// Include: []string{"team"}. Whether PCO's plan_people endpoint actually
// honors that include isn't confirmed live (unlike "plan", which
// GetPersonPlanPeople's own test fixtures cover) - if it doesn't, this map
// just comes back empty and crossTeamAssignmentsThisWeek falls back to
// Attributes.TeamPositionName alone, same best-effort spirit as every other
// lookup in buildRosterBuilderData.
type includedTeam struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		Name string `json:"name"`
	} `json:"attributes"`
}

// teamNamesFromIncluded extracts team id -> name from a GetPersonPlanPeople
// response's Included array, mirroring planSortDatesFromIncluded. Any entry
// that isn't a well-formed Team (or that lacks a name) is silently skipped.
func teamNamesFromIncluded(included []any) map[string]string {
	names := map[string]string{}
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var t includedTeam
		if err := json.Unmarshal(b, &t); err != nil {
			continue
		}
		if t.Type != "Team" || t.ID == "" || t.Attributes.Name == "" {
			continue
		}
		names[t.ID] = t.Attributes.Name
	}
	return names
}

// buildRosterBuilderData gathers everything the Team Builder page (and every
// htmx fragment it swaps after an add/remove action) needs to render - the
// people equivalent of buildSetBuilderData. Unlike songs, people aren't
// preloaded in full (a People directory can dwarf a song library) - instead
// each open position's candidate pool comes from PCO's own eligibility list
// (GetPersonTeamPositionAssignments), which is naturally small and bounded,
// so it gets the same pre-add warning visibility Build a Set's song
// candidates get without needing to fetch or filter the whole directory.
func (sc *SchedulingController) buildRosterBuilderData(ctx context.Context, organizationID, serviceTypeID, planID string) views.RosterBuilderData {
	data := views.RosterBuilderData{ServiceTypeID: serviceTypeID, PlanID: planID}

	var planDate time.Time
	if plan, err := pco.GetPlan(ctx, serviceTypeID, planID); err == nil {
		data.PlanTitle = plan.Data.Attributes.Title
		data.PlanDates = plan.Data.Attributes.Dates
		planDate = plan.Data.Attributes.SortDate
		// ReadOnly - see SetBuilderData.ReadOnly (sets_builder.go), which
		// this mirrors exactly.
		data.ReadOnly = !planDate.IsZero() && planDate.Before(time.Now())
	} else {
		data.ErrMsg = "loading plan from Planning Center: " + err.Error()
	}

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

	// One bulk read of every PlanPerson Planning Center already has for
	// this plan, reused for two independent things below rather than two
	// separate calls: ImportableAssignmentCount (only meaningful the first
	// time this roster is opened - see SetBuilderData.ImportableSongCount,
	// which this mirrors) and notesByPlanPersonID (every published
	// assignment's own Notes, read-only display on the person detail
	// sidebar card - see AssignmentView.Notes). This is also why
	// ImportableAssignmentCount can't reuse needed (fetched right below)
	// instead - GetNeededPositions only reports *unfilled* slots, so a plan
	// PCO already has fully staffed - the whole point of this prompt -
	// shows zero needed positions.
	notesByPlanPersonID := map[string]string{}
	if members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil); err == nil {
		if len(assignments) == 0 && data.PendingDeleteCount == 0 {
			data.ImportableAssignmentCount = len(members.Data)
		}
		for _, m := range members.Data {
			notesByPlanPersonID[m.ID] = m.Attributes.Notes
		}
	}

	needed, err := pco.GetNeededPositions(ctx, serviceTypeID, planID, nil)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading needed positions from Planning Center: "+err.Error())
	}
	groups := groupNeededPositions(needed.Data, assignments)

	teamNames := map[string]string{}
	// teamSequence backs the team-group ordering below (see
	// unknownTeamSequence's own comment) - PCO's own Team.Sequence, the
	// order a director already arranged their teams in within Planning
	// Center itself, rather than an arbitrary or alphabetical order.
	teamSequence := map[string]int{}
	if teams, err := pco.GetTeams(ctx, serviceTypeID, nil); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading teams from Planning Center: "+err.Error())
	} else {
		for _, t := range teams.Data {
			teamNames[t.ID] = t.Attributes.Name
			teamSequence[t.ID] = t.Attributes.Sequence
		}
	}

	// team_position ids aren't stored locally or on NeededPosition (both
	// only carry the position's name) - resolve name -> id per team,
	// fetched once per team and reused across every position group on it.
	positionIDsByTeam := map[string]map[string]string{}
	positionID := func(teamID, positionName string) string {
		byName, ok := positionIDsByTeam[teamID]
		if !ok {
			byName = map[string]string{}
			if positions, err := pco.GetTeamPositions(ctx, teamID, nil); err == nil {
				for _, p := range positions.Data {
					byName[p.Attributes.Name] = p.ID
				}
			}
			positionIDsByTeam[teamID] = byName
		}
		return byName[positionName]
	}

	// Each group's candidate pool (PCO's own eligibility list for that
	// position) is fetched once and reused for both "who's assigned"
	// warnings and "who could be added" candidates below. positionIDs is
	// kept alongside it - PositionGroupView.TeamPositionID needs this same
	// resolved id so the roster tab's "+ Add eligible person" quick-add can
	// post straight at a position without asking the user to pick it again.
	//
	// positionIDs is resolved first, sequentially - positionID's lazy
	// per-team cache (positionIDsByTeam) isn't safe for concurrent map
	// writes, and it's normally a cache hit anyway (a handful of distinct
	// teams). The actual per-group PCO fetch is independent of every other
	// group, so it fans out via runConcurrently instead of paying each
	// group's round trip serially - see pco-api-risks.md's concurrency
	// mitigation. candidatesByGroup[i] is each goroutine's own slice index,
	// so no lock is needed for these writes.
	positionIDs := make([]string, len(groups))
	for i, g := range groups {
		positionIDs[i] = positionID(g.TeamID, g.TeamPositionName)
	}

	candidatesByGroup := make([][]pco.PersonTeamPositionAssignmentData, len(groups))
	groupIndexes := make([]int, len(groups))
	for i := range groups {
		groupIndexes[i] = i
	}
	runConcurrently(groupIndexes, func(i int) {
		posID := positionIDs[i]
		if posID == "" {
			return
		}
		people, err := pco.GetPersonTeamPositionAssignments(ctx, groups[i].TeamID, posID, nil)
		if err != nil {
			return // best-effort - one position's candidate pool failing doesn't sink the page
		}
		candidatesByGroup[i] = people.Data
	})

	relevantPersonIDs := map[string]bool{}
	for _, ra := range assignments {
		relevantPersonIDs[ra.PersonID] = true
	}
	for _, candidates := range candidatesByGroup {
		for _, pa := range candidates {
			if pa.Relationships.Person.Data != nil {
				relevantPersonIDs[pa.Relationships.Person.Data.ID] = true
			}
		}
	}

	var relevantIDs []string
	for id := range relevantPersonIDs {
		relevantIDs = append(relevantIDs, id)
	}
	names := personNameMapForIDs(ctx, relevantIDs)

	// One GetBlockouts call per relevant person, with no PCO-side batch
	// option: confirmed live that this endpoint (services/v2/people/{id}/
	// blockouts) is nested per-person with can_query_by limited to
	// group_identifier, unlike People#index's where[id]. Fanned out
	// concurrently instead, guarded by blockoutsMu since every goroutine
	// writes into the same map.
	blockoutsByPerson := map[string][]pco.BlockoutData{}
	var blockoutsMu sync.Mutex
	runConcurrently(relevantIDs, func(id string) {
		bo, err := pco.GetBlockouts(ctx, id, &pco.BlockoutsParams{Filter: "future"})
		if err != nil {
			return
		}
		blockoutsMu.Lock()
		blockoutsByPerson[id] = bo.Data
		blockoutsMu.Unlock()
	})

	schedulePreferenceFor := func(candidates []pco.PersonTeamPositionAssignmentData, personID string) string {
		for _, pa := range candidates {
			if pa.Relationships.Person.Data != nil && pa.Relationships.Person.Data.ID == personID {
				return pa.Attributes.SchedulePreference
			}
		}
		return ""
	}

	// crossTeamAssignmentsThisWeek(personID, planDate) is identical
	// everywhere a person appears, regardless of which group is asking, so
	// it's fetched once per person rather than recomputed per (person,
	// group) pair - a person eligible for several positions otherwise
	// triggers the same PCO call once per position. otherAssignmentDates is
	// scoped to a specific (team, position) slot, so it's keyed on that
	// instead. See pco-api-risks.md's mitigation #1 (the dedup) and its
	// concurrency mitigation (the fan-out below).
	//
	// Every key either loop needs is collected up front in this pass -
	// mirroring the assigned/candidates walk below, minus the "already
	// assigned in this group" skip (a candidate who turns out to be
	// assigned just means one warmed-but-unread cache entry, harmless) -
	// so both caches can be filled by a bounded concurrent fetch instead of
	// a live PCO call on every miss inside the sequential view-building
	// loop that follows. That loop below only ever reads these two maps.
	type otherDatesKey struct{ personID, teamID, teamPositionName string }
	crossTeamPersonIDs := map[string]bool{}
	otherDatesKeys := map[otherDatesKey]bool{}
	if !planDate.IsZero() {
		for i, g := range groups {
			for _, ra := range g.Assignments {
				crossTeamPersonIDs[ra.PersonID] = true
				if schedulePreferenceFor(candidatesByGroup[i], ra.PersonID) != "" {
					otherDatesKeys[otherDatesKey{ra.PersonID, g.TeamID, g.TeamPositionName}] = true
				}
			}
			for _, pa := range candidatesByGroup[i] {
				if pa.Relationships.Person.Data == nil {
					continue
				}
				personID := pa.Relationships.Person.Data.ID
				crossTeamPersonIDs[personID] = true
				if pa.Attributes.SchedulePreference != "" {
					otherDatesKeys[otherDatesKey{personID, g.TeamID, g.TeamPositionName}] = true
				}
			}
		}
	}

	crossTeamPersonIDList := make([]string, 0, len(crossTeamPersonIDs))
	for id := range crossTeamPersonIDs {
		crossTeamPersonIDList = append(crossTeamPersonIDList, id)
	}
	crossTeamCache := map[string][]string{}
	var crossTeamMu sync.Mutex
	runConcurrently(crossTeamPersonIDList, func(personID string) {
		labels := crossTeamAssignmentsThisWeek(ctx, personID, planDate, planID)
		crossTeamMu.Lock()
		crossTeamCache[personID] = labels
		crossTeamMu.Unlock()
	})
	crossTeamWarningsFor := func(personID string) []string {
		return crossTeamCache[personID]
	}

	otherDatesKeyList := make([]otherDatesKey, 0, len(otherDatesKeys))
	for k := range otherDatesKeys {
		otherDatesKeyList = append(otherDatesKeyList, k)
	}
	otherDatesCache := map[string][]time.Time{}
	var otherDatesMu sync.Mutex
	runConcurrently(otherDatesKeyList, func(k otherDatesKey) {
		dates := otherAssignmentDates(ctx, k.personID, k.teamID, k.teamPositionName, planID)
		otherDatesMu.Lock()
		otherDatesCache[k.personID+"|"+k.teamID+"|"+k.teamPositionName] = dates
		otherDatesMu.Unlock()
	})
	otherAssignmentDatesFor := func(personID, teamID, teamPositionName string) []time.Time {
		return otherDatesCache[personID+"|"+teamID+"|"+teamPositionName]
	}

	for i, g := range groups {
		pg := views.PositionGroupView{
			TeamID:           g.TeamID,
			TeamName:         titleOrID(teamNames, g.TeamID),
			TeamPositionName: g.TeamPositionName,
			TeamPositionID:   positionIDs[i],
			Quantity:         g.Quantity,
		}

		assignedHere := map[string]bool{}
		for _, ra := range g.Assignments {
			assignedHere[ra.PersonID] = true

			pref := schedulePreferenceFor(candidatesByGroup[i], ra.PersonID)
			var freqWarn string
			var crossWarn string
			if !planDate.IsZero() {
				if pref != "" {
					freqWarn = frequencyWarning(pref, planDate, otherAssignmentDatesFor(ra.PersonID, g.TeamID, g.TeamPositionName))
				}
				crossWarn = crossTeamWarning(crossTeamWarningsFor(ra.PersonID))
			}

			pg.Assigned = append(pg.Assigned, views.AssignmentView{
				ID:               ra.ID,
				PersonID:         ra.PersonID,
				Name:             titleOrID(names, ra.PersonID),
				Status:           ra.Status,
				TeamName:         pg.TeamName,
				TeamPositionName: pg.TeamPositionName,
				Published:        ra.PCOPlanPersonID != "",
				Notes:            notesByPlanPersonID[ra.PCOPlanPersonID],
				FrequencyWarning: freqWarn,
				BlockoutWarning:  blockoutWarningIfKnown(planDate, blockoutsByPerson[ra.PersonID]),
				CrossTeamWarning: crossWarn,
			})
		}

		for _, pa := range candidatesByGroup[i] {
			if pa.Relationships.Person.Data == nil {
				continue
			}
			personID := pa.Relationships.Person.Data.ID
			if assignedHere[personID] {
				continue
			}

			var freqWarn string
			var crossWarn string
			if !planDate.IsZero() {
				if pa.Attributes.SchedulePreference != "" {
					freqWarn = frequencyWarning(pa.Attributes.SchedulePreference, planDate, otherAssignmentDatesFor(personID, g.TeamID, g.TeamPositionName))
				}
				crossWarn = crossTeamWarning(crossTeamWarningsFor(personID))
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

		if len(pg.Assigned) < pg.Quantity {
			data.OpenPositionCount++
		}
		data.Positions = append(data.Positions, pg)
	}

	// Grouped by team for display (see views.groupPositionsByTeam) - sorted
	// here, not there, since the view layer just walks contiguous runs
	// rather than re-sorting. Stable so within a team, positions keep
	// whatever order groupNeededPositions already gave them; across teams,
	// unknownTeamSequence pushes anything whose team lookup failed to the
	// end rather than letting it default to 0 and jump to the front.
	sort.SliceStable(data.Positions, func(i, j int) bool {
		return positionTeamSequence(teamSequence, data.Positions[i].TeamID) < positionTeamSequence(teamSequence, data.Positions[j].TeamID)
	})

	// PersonRelationshipTypes/SchedulePreferenceOptions back the "Quick add
	// a relationship" and per-position "+ Add eligible person" panels (see
	// views.RosterBuilderData) - best-effort like the lookups above, since a
	// failure here only costs those panels' dropdowns, not the page.
	data.PersonRelationshipTypes, _ = sc.store.ListPersonRelationshipTypes(ctx)
	data.SchedulePreferenceOptions = schedulePreferenceOptions()

	if conflicts, existing, err := sc.store.AvoidPairingConflicts(ctx, organizationID, roster.ID); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	} else {
		_ = conflicts // candidates aren't pre-rendered as a flat pool for people, so only the existing-pair banner applies here
		for _, ex := range existing {
			data.ExistingConflicts = append(data.ExistingConflicts, fmt.Sprintf(
				"%s conflicts with %s - both are already on this roster.",
				titleOrID(names, ex.PersonID), titleOrID(names, ex.ConflictsWith),
			))
		}
	}

	if suggestions, err := sc.store.RelationshipSuggestions(ctx, organizationID, roster.ID); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	} else {
		data.Suggestions = suggestionsForOpenPositions(suggestions, groups, candidatesByGroup, teamNames, names)
	}

	return data
}

// suggestionsForOpenPositions attaches each relationship-based suggestion to
// a specific open position they're eligible for - a suggestion is only
// actionable if there's somewhere to actually put them, so a person who
// isn't eligible for any of this plan's open positions is dropped rather
// than shown with no add button.
func suggestionsForOpenPositions(
	suggestions []scheduling.PersonSuggestion,
	groups []positionGroup,
	candidatesByGroup [][]pco.PersonTeamPositionAssignmentData,
	teamNames, personNames map[string]string,
) []views.PersonSuggestionView {
	var out []views.PersonSuggestionView
	for _, sug := range suggestions {
		for i, g := range groups {
			eligible := false
			for _, pa := range candidatesByGroup[i] {
				if pa.Relationships.Person.Data != nil && pa.Relationships.Person.Data.ID == sug.PersonID {
					eligible = true
					break
				}
			}
			if !eligible {
				continue
			}

			alreadyAssigned := false
			for _, ra := range g.Assignments {
				if ra.PersonID == sug.PersonID {
					alreadyAssigned = true
					break
				}
			}
			if alreadyAssigned {
				continue
			}

			out = append(out, views.PersonSuggestionView{
				PersonID:          sug.PersonID,
				Name:              titleOrID(personNames, sug.PersonID),
				TeamID:            g.TeamID,
				TeamPositionName:  g.TeamPositionName,
				RelationshipLabel: strings.Join(sug.RelationshipTypes, ", "),
			})
		}
	}
	return out
}
