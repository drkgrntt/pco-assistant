package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Rosters (Team Builder) list/status --------------------------------------

// rosterStatus classifies a plan for the picker list - see the scheduling
// controller's planStatus, which this mirrors exactly (substituting
// team_members for items as the "does Planning Center already have content
// here" check).
func (sc *SchedulingController) rosterStatus(ctx context.Context, organizationID, serviceTypeID, planID string) string {
	if roster, found, err := sc.store.FindRoster(ctx, organizationID, serviceTypeID, planID); err == nil && found {
		assignments, assignErr := sc.store.ListRosterAssignments(ctx, roster.ID)
		pending, pendingErr := sc.store.ListPendingRosterDeletions(ctx, roster.ID)
		if assignErr == nil && pendingErr == nil && len(assignments) > 0 {
			for _, ra := range assignments {
				if ra.PCOPlanPersonID == "" {
					return "pending"
				}
			}
			if len(pending) > 0 {
				return "pending"
			}
			return "planned"
		}
	}

	members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil)
	if err != nil {
		return ""
	}
	if len(members.Data) > 0 {
		return "todo"
	}
	return ""
}

// --- Roster builder page ------------------------------------------------------

func (sc *SchedulingController) AddRosterAssignment(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		PersonID         string `form:"person_id"`
		TeamID           string `form:"team_id"`
		TeamPositionName string `form:"team_position_name"`
	}
	errMsg := ""
	previousPersonID := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if roster, err := sc.store.GetOrCreateRoster(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else {
		// Captured before the add, so it reflects who was already on the
		// roster, not (once AddPersonToPosition below succeeds) the person
		// just added themselves.
		previousPersonID = sc.previouslyAddedPersonID(c.Context(), roster.ID)
		if _, err := sc.store.AddPersonToPosition(c.Context(), roster.ID, body.PersonID, body.TeamID, body.TeamPositionName); err != nil {
			errMsg = err.Error()
			previousPersonID = "" // the add itself failed - nothing to prompt about
		}
	}

	data := sc.buildRosterBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)

	// PersonPairPrompt: only ever set here, right after a successful add
	// that actually had someone else already on the roster to pair with -
	// see RosterBuilderData.PersonPairPrompt's doc comment and
	// SetBuilderData.SongPairPrompt, which this mirrors. Every other
	// mutation (remove/publish/revert/quick-add, or acting on the prompt
	// itself) rebuilds RosterBuilderData fresh without it, so it disappears
	// on its own the moment you do anything else. previousPersonID != NewPersonID
	// guards the edge case where the "most recently added" person is the
	// same one just added to a second position - relating someone to
	// themselves makes no sense, so no prompt in that case.
	if previousPersonID != "" && previousPersonID != body.PersonID {
		prompt := &views.PersonPairPromptView{PreviousPersonID: previousPersonID, NewPersonID: body.PersonID}
		for _, pos := range data.Positions {
			for _, a := range pos.Assigned {
				if a.PersonID == previousPersonID {
					prompt.PreviousPersonName = a.Name
				}
				if a.PersonID == body.PersonID {
					prompt.NewPersonName = a.Name
				}
			}
		}
		if prompt.PreviousPersonName != "" && prompt.NewPersonName != "" {
			data.PersonPairPrompt = prompt
		}
	}

	return render(c, views.RosterBuilderBody(data))
}

// previouslyAddedPersonID returns the person id of whoever was most
// recently added to this roster (by creation order - see
// ListRosterAssignments' ORDER BY created_at ASC), or "" if the roster is
// empty or the lookup fails. Rosters have no song-order-style "last
// touched" pointer the way sets do (there's no sequence to chain
// people into), so "whoever I was just adding right before this one" is
// the closest equivalent signal - best-effort, same as
// firstSongFallbackAnchor: a lookup failure just means no prompt, not a
// failed add.
func (sc *SchedulingController) previouslyAddedPersonID(ctx context.Context, rosterID int64) string {
	existing, err := sc.store.ListRosterAssignments(ctx, rosterID)
	if err != nil || len(existing) == 0 {
		return ""
	}
	return existing[len(existing)-1].PersonID
}

func (sc *SchedulingController) RemoveRosterAssignment(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	if rowID, err := c.ParamsInt("assignment_id"); err != nil {
		errMsg = "assignment_id must be an integer"
	} else if roster, err := sc.store.GetOrCreateRoster(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.RemovePersonFromPosition(c.Context(), roster.ID, int64(rowID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		errMsg = err.Error()
	}

	return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// PublishRoster fully syncs the draft roster to the real Planning Center
// plan - the people equivalent of PublishSet, but simpler: there's no
// reorder step at all, since position assignments have no sequence.
func (sc *SchedulingController) PublishRoster(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	pendingDeletions, err := sc.store.ListPendingRosterDeletions(ctx, roster.ID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	var failures []string
	deleted, created := 0, 0

	names := personNameMapForIDs(ctx, assignmentPersonIDs(append(append([]scheduling.RosterAssignment{}, assignments...), pendingDeletions...)))

	for _, ra := range pendingDeletions {
		name := titleOrID(names, ra.PersonID)

		if err := pco.DeleteTeamMember(ctx, serviceTypeID, planID, ra.PCOPlanPersonID); err != nil {
			failures = append(failures, fmt.Sprintf("removing %s: %s", name, err.Error()))
			continue
		}
		if err := sc.store.DeleteAssignmentRow(ctx, ra.ID); err != nil {
			failures = append(failures, fmt.Sprintf("removed %s from Planning Center but failed to clear it locally: %s", name, err.Error()))
			continue
		}
		deleted++
	}

	for _, ra := range assignments {
		if ra.PCOPlanPersonID != "" {
			continue
		}
		name := titleOrID(names, ra.PersonID)

		member, err := pco.CreateTeamMember(ctx, serviceTypeID, planID, &pco.CreateTeamMemberParams{
			PersonID:         ra.PersonID,
			TeamID:           ra.TeamID,
			TeamPositionName: ra.TeamPositionName,
		})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", name, err.Error()))
			continue
		}
		if err := sc.store.MarkAssignmentPublished(ctx, roster.ID, ra.ID, member.Data.ID); err != nil {
			failures = append(failures, fmt.Sprintf("%s: created in Planning Center but failed to record locally: %s", name, err.Error()))
			continue
		}
		created++
	}

	var summary []string
	if created > 0 {
		summary = append(summary, fmt.Sprintf("added %d", created))
	}
	if deleted > 0 {
		summary = append(summary, fmt.Sprintf("removed %d", deleted))
	}

	publishMsg := ""
	if len(summary) > 0 {
		publishMsg = "Synced to Planning Center: " + strings.Join(summary, ", ") + "."
	} else if len(failures) == 0 {
		publishMsg = "Nothing to publish - already in sync with Planning Center."
	}

	errMsg := ""
	if len(failures) > 0 {
		errMsg = "Failed to sync: " + strings.Join(failures, "; ")
	}

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.RosterBuilderBody(data))
}

// RevertRoster discards local changes and rebuilds the roster from whatever
// Planning Center's plan currently has - see sets.RevertSet, which this
// mirrors.
func (sc *SchedulingController) RevertRoster(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID)
	errMsg := ""
	publishMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil)
		if err != nil {
			errMsg = "loading plan team members from Planning Center: " + err.Error()
		} else {
			importRows := make([]scheduling.RosterAssignment, 0, len(members.Data))
			for _, m := range members.Data {
				teamID := ""
				if m.Relationships.Team.Data != nil {
					teamID = m.Relationships.Team.Data.ID
				}
				personID := ""
				if m.Relationships.Person.Data != nil {
					personID = m.Relationships.Person.Data.ID
				}
				importRows = append(importRows, scheduling.RosterAssignment{
					PersonID:         personID,
					TeamID:           teamID,
					TeamPositionName: m.Attributes.TeamPositionName,
					PCOPlanPersonID:  m.ID,
					Status:           m.Attributes.Status,
				})
			}

			if err := sc.store.RevertRoster(ctx, roster.ID, importRows); err != nil {
				errMsg = err.Error()
			} else {
				publishMsg = "Reverted to match Planning Center."
			}
		}
	}

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.RosterBuilderBody(data))
}

// QuickAddPersonRelationship/QuickAddPositionPreference back the roster
// builder's quick-add panels (roster_builder.templ) - the same
// setup-level entities as PersonRelationshipsPage/PositionPreferencesPage's
// own create forms (same underlying operations - CreatePersonRelationship,
// setPersonTeamPositionPreference from people.go), just posted from the
// build page and re-rendering RosterBuilderBody instead of a setup-page
// table/fragment. Deliberately create-only, same as the set builder's
// three quick-add handlers in sets.go.

func (sc *SchedulingController) QuickAddPersonRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body scheduling.CreatePersonRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreatePersonRelationship(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

func (sc *SchedulingController) QuickAddPositionPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		TeamID             string `form:"team_id"`
		TeamPositionID     string `form:"team_position_id"`
		PersonID           string `form:"person_id"`
		SchedulePreference string `form:"schedule_preference"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.TeamID == "" || body.TeamPositionID == "" || body.PersonID == "" {
		errMsg = "team, position, and person are required"
	} else if err := setPersonTeamPositionPreference(middleware.RequestContext(c), body.TeamID, body.TeamPositionID, body.PersonID, body.SchedulePreference); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

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

// --- Position grouping & warnings (pure, testable) ---------------------------

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

// everyXWeeksFor maps an "every X weeks" schedule_preference value to its X.
// "Once a quarter" is treated as a rough ~13 weeks, since PCO doesn't offer
// a dedicated per-month bucket for it.
func everyXWeeksFor(schedulePreference string) (weeks int, ok bool) {
	switch schedulePreference {
	case pco.SchedulePreferenceEveryWeek:
		return 1, true
	case pco.SchedulePreferenceEveryOtherWeek:
		return 2, true
	case pco.SchedulePreferenceEvery3rdWeek:
		return 3, true
	case pco.SchedulePreferenceEvery4thWeek:
		return 4, true
	case pco.SchedulePreferenceEvery5thWeek:
		return 5, true
	case pco.SchedulePreferenceEvery6thWeek:
		return 6, true
	case pco.SchedulePreferenceOnceAQuarter:
		return 13, true
	}
	return 0, false
}

// perMonthFor maps an "X times a month" schedule_preference value to its X.
func perMonthFor(schedulePreference string) (times int, ok bool) {
	switch schedulePreference {
	case pco.SchedulePreferenceOnceAMonth:
		return 1, true
	case pco.SchedulePreferenceTwiceAMonth:
		return 2, true
	case pco.SchedulePreferenceThreeTimesAMonth:
		return 3, true
	}
	return 0, false
}

// frequencyWarning checks a person's serving-frequency preference
// (PersonTeamPositionAssignment.schedule_preference) against their other
// known assignment dates for the same position, relative to targetDate (the
// plan being built).
//
//   - "Unavailable" always warns, regardless of other dates.
//   - The "every X weeks" presets warn when the *nearest* other assignment
//     (whichever side of targetDate it falls on) is closer than X weeks -
//     serving again too soon either before or after this plan both violate
//     the same preference.
//   - The "X times a month" presets warn when targetDate's calendar month
//     already has >= X other assignments (not counting targetDate itself,
//     which isn't in otherDates - it's the plan being considered, not yet a
//     confirmed serving date).
//   - "Choose Weeks" has no numeric rule to check against and never warns -
//     this app doesn't model PCO's custom per-week list (see
//     PersonTeamPositionAssignmentAttributes.PreferredWeeks in go-pco).
func frequencyWarning(schedulePreference string, targetDate time.Time, otherDates []time.Time) string {
	if schedulePreference == pco.SchedulePreferenceUnavailable {
		return "marked Unavailable for this position"
	}

	if weeks, ok := everyXWeeksFor(schedulePreference); ok {
		if len(otherDates) == 0 {
			return ""
		}
		nearestGap := math.Abs(targetDate.Sub(otherDates[0]).Hours())
		for _, d := range otherDates[1:] {
			if gap := math.Abs(targetDate.Sub(d).Hours()); gap < nearestGap {
				nearestGap = gap
			}
		}
		gapWeeks := nearestGap / (7 * 24)
		if gapWeeks < float64(weeks) {
			return fmt.Sprintf("prefers %q - another serving is only about %.0f week(s) away", schedulePreference, gapWeeks)
		}
		return ""
	}

	if times, ok := perMonthFor(schedulePreference); ok {
		count := 0
		for _, d := range otherDates {
			if d.Year() == targetDate.Year() && d.Month() == targetDate.Month() {
				count++
			}
		}
		if count >= times {
			return fmt.Sprintf("prefers %q - already serving %d time(s) this month", schedulePreference, count)
		}
		return ""
	}

	return ""
}

// blockoutWarning flags when targetDate falls inside one of the person's
// blockout windows. Scoped to non-recurring windows for v1 - a blockout
// with a repeat_frequency set is skipped rather than expanded into its
// recurring dates, a deliberate simplification (see BlockoutAttributes' doc
// comment in go-pco), not a silent bug.
func blockoutWarning(targetDate time.Time, blockouts []pco.BlockoutData) string {
	for _, b := range blockouts {
		if b.Attributes.RepeatFrequency != "" {
			continue
		}
		if targetDate.Before(b.Attributes.StartsAt) || targetDate.After(b.Attributes.EndsAt) {
			continue
		}
		if b.Attributes.Reason == "" {
			return "marked unavailable in Planning Center for this date"
		}
		return fmt.Sprintf("marked unavailable in Planning Center for this date (%s)", b.Attributes.Reason)
	}
	return ""
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

// otherAssignmentDates fetches personID's own serving dates for the exact
// (teamID, teamPositionName) slot, excluding excludePlanID (the plan
// currently being built, if they're already on it) - the thin PCO-calling
// wrapper frequencyWarning's pure check runs against. PCO's plan_people
// endpoint filters by team_id server-side but not by position name, so the
// position match happens here client-side.
func otherAssignmentDates(ctx context.Context, personID, teamID, teamPositionName, excludePlanID string) []time.Time {
	history, err := pco.GetPersonPlanPeople(ctx, personID, &pco.PersonPlanPeopleParams{
		TeamID:  teamID,
		Include: []string{"plan"},
		PerPage: 25,
	})
	if err != nil {
		return nil
	}

	planDates := planSortDatesFromIncluded(history.Included)

	var dates []time.Time
	for _, pp := range history.Data {
		if pp.Attributes.TeamPositionName != teamPositionName {
			continue
		}
		if pp.Relationships.Plan.Data == nil {
			continue
		}
		planID := pp.Relationships.Plan.Data.ID
		if planID == excludePlanID {
			continue
		}
		if d, ok := planDates[planID]; ok {
			dates = append(dates, d)
		}
	}
	return dates
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
	if teams, err := pco.GetTeams(ctx, serviceTypeID, nil); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading teams from Planning Center: "+err.Error())
	} else {
		for _, t := range teams.Data {
			teamNames[t.ID] = t.Attributes.Name
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
	candidatesByGroup := make([][]pco.PersonTeamPositionAssignmentData, len(groups))
	positionIDs := make([]string, len(groups))
	relevantPersonIDs := map[string]bool{}
	for _, ra := range assignments {
		relevantPersonIDs[ra.PersonID] = true
	}
	for i, g := range groups {
		posID := positionID(g.TeamID, g.TeamPositionName)
		positionIDs[i] = posID
		if posID == "" {
			continue
		}
		people, err := pco.GetPersonTeamPositionAssignments(ctx, g.TeamID, posID, nil)
		if err != nil {
			continue // best-effort - one position's candidate pool failing doesn't sink the page
		}
		candidatesByGroup[i] = people.Data
		for _, pa := range people.Data {
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

	blockoutsByPerson := map[string][]pco.BlockoutData{}
	for id := range relevantPersonIDs {
		if bo, err := pco.GetBlockouts(ctx, id, &pco.BlockoutsParams{Filter: "future"}); err == nil {
			blockoutsByPerson[id] = bo.Data
		}
	}

	schedulePreferenceFor := func(candidates []pco.PersonTeamPositionAssignmentData, personID string) string {
		for _, pa := range candidates {
			if pa.Relationships.Person.Data != nil && pa.Relationships.Person.Data.ID == personID {
				return pa.Attributes.SchedulePreference
			}
		}
		return ""
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
			if pref != "" && !planDate.IsZero() {
				freqWarn = frequencyWarning(pref, planDate, otherAssignmentDates(ctx, ra.PersonID, g.TeamID, g.TeamPositionName, planID))
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
			if pa.Attributes.SchedulePreference != "" && !planDate.IsZero() {
				freqWarn = frequencyWarning(pa.Attributes.SchedulePreference, planDate, otherAssignmentDates(ctx, personID, g.TeamID, g.TeamPositionName, planID))
			}

			pg.Candidates = append(pg.Candidates, views.CandidatePersonView{
				PersonID:           personID,
				Name:               titleOrID(names, personID),
				SchedulePreference: pa.Attributes.SchedulePreference,
				FrequencyWarning:   freqWarn,
				BlockoutWarning:    blockoutWarningIfKnown(planDate, blockoutsByPerson[personID]),
			})
		}

		if len(pg.Assigned) < pg.Quantity {
			data.OpenPositionCount++
		}
		data.Positions = append(data.Positions, pg)
	}

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

	sort.SliceStable(data.Positions, func(i, j int) bool {
		if data.Positions[i].TeamName != data.Positions[j].TeamName {
			return data.Positions[i].TeamName < data.Positions[j].TeamName
		}
		return data.Positions[i].TeamPositionName < data.Positions[j].TeamPositionName
	})

	return data
}

// blockoutWarningIfKnown is blockoutWarning guarded for a not-yet-resolved
// plan date (e.g. the GetPlan call failed) - no date means nothing to check
// against, not a false "unavailable."
func blockoutWarningIfKnown(planDate time.Time, blockouts []pco.BlockoutData) string {
	if planDate.IsZero() {
		return ""
	}
	return blockoutWarning(planDate, blockouts)
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
