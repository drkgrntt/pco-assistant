package controllers

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Person relationships & position preferences ---------------------------

// personRelationshipTypeLabelMap builds a relationship_type code -> label
// lookup from ListPersonRelationshipTypes, for resolving a relationship's
// stored type code back to its display label (see PersonRelationshipsTable).
func personRelationshipTypeLabelMap(types []scheduling.PersonRelationshipType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

func (sc *SchedulingController) PersonRelationshipsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	relationshipTypes, err := sc.store.ListPersonRelationshipTypes(c.Context())
	if err != nil {
		return render(c, views.PersonRelationships(personName, orgName, nil, nil, nil, nil, err.Error()))
	}

	relationships, err := sc.store.ListPersonRelationships(c.Context(), orgID, "")
	if err != nil {
		return render(c, views.PersonRelationships(personName, orgName, nil, relationshipTypes, nil, personRelationshipTypeLabelMap(relationshipTypes), err.Error()))
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))

	return render(c, views.PersonRelationships(personName, orgName, relationships, relationshipTypes, names, personRelationshipTypeLabelMap(relationshipTypes), ""))
}

func (sc *SchedulingController) CreatePersonRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.CreatePersonRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdatePersonRelationship(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreatePersonRelationship(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	relationships, err := sc.store.ListPersonRelationships(c.Context(), orgID, "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))
	relationshipTypes, _ := sc.store.ListPersonRelationshipTypes(c.Context())

	return render(c, views.PersonRelationshipsTable(relationships, names, personRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

func (sc *SchedulingController) DeletePersonRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeletePersonRelationship(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	relationships, err := sc.store.ListPersonRelationships(c.Context(), orgID, "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))
	relationshipTypes, _ := sc.store.ListPersonRelationshipTypes(c.Context())

	return render(c, views.PersonRelationshipsTable(relationships, names, personRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

// relationshipPersonIDs collects every distinct person id referenced by a
// relationship list, for a single batch of name lookups rather than one per
// row.
func relationshipPersonIDs(relationships []scheduling.PersonRelationship) []string {
	seen := map[string]bool{}
	var ids []string
	for _, rel := range relationships {
		for _, id := range [2]string{rel.PersonIDA, rel.PersonIDB} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// personIDBatchSize caps how many ids personNameMapForIDs puts in one
// `where[id]=...` call - PCO's People#index accepts a comma-joined id list
// (confirmed live; `can_query_by` includes "id") but still paginates like
// any other request, so PerPage is set to match each chunk and results
// beyond PCO's own per-page ceiling would otherwise silently truncate.
// 100 matches PCO's documented per_page maximum across its APIs.
const personIDBatchSize = 100

// personLookup is one person's display name plus avatar URL, as resolved by
// personLookupMapForIDs - the roster builder's person rows (assigned/
// candidate/suggested) need both; everything else only ever wanted the name,
// which is why personNameMapForIDs below stays around as a thin wrapper
// rather than every existing caller having to switch to this richer type.
type personLookup struct {
	Name      string
	AvatarURL string
}

// personLookupMapForIDs resolves a specific set of PCO person ids to display
// names and avatar URLs - like personNameMap in scheduling.go, but for
// callers (person relationships, rosters) that already know exactly which
// ids they need rather than deriving them from a preferences list. Batches
// ids into one `where[id]=...` call per personIDBatchSize ids rather than one
// GetPerson round trip per id (see docs/pco-api-risks.md's mitigation #2 -
// this was the single biggest source of PCO calls on the Team Builder page).
// An id that fails to resolve is simply left out; callers fall back to
// showing the raw id (and, for avatars, no image at all - see
// components.Avatar).
//
// PersonAttributes.Avatar rides along on this same batched fetch for free -
// PCO's People#index already returns it on every person, uploaded photo or
// PCO's own generated placeholder, with no extra Include/round trip needed
// (see docs/pco-api-risks.md).
func personLookupMapForIDs(ctx context.Context, ids []string) map[string]personLookup {
	lookups := make(map[string]personLookup, len(ids))
	for start := 0; start < len(ids); start += personIDBatchSize {
		end := min(start+personIDBatchSize, len(ids))
		batch := ids[start:end]

		people, err := pco.GetPeople(ctx, &pco.PeopleParams{IDs: batch, PerPage: len(batch)})
		if err != nil {
			continue // best-effort, same as every other lookup here - callers fall back to raw ids
		}
		for _, person := range people.Data {
			lookups[person.ID] = personLookup{Name: person.Attributes.Name, AvatarURL: person.Attributes.Avatar}
		}
	}
	return lookups
}

// personNameMapForIDs is personLookupMapForIDs, names only - every call site
// that doesn't need avatars (person relationships, position preferences)
// stays on this narrower, unchanged signature.
func personNameMapForIDs(ctx context.Context, ids []string) map[string]string {
	lookups := personLookupMapForIDs(ctx, ids)
	names := make(map[string]string, len(lookups))
	for id, l := range lookups {
		names[id] = l.Name
	}
	return names
}

// --- Position preferences ---------------------------------------------------

func (sc *SchedulingController) PositionPreferencesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)

	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypes, err := scheduling.FetchServiceTypes(ctx, orgID)
	if err != nil {
		return render(c, views.PositionPreferences(personName, orgName, nil, nil, "", err.Error()))
	}

	serviceTypeID := c.Query("service_type_id")
	if serviceTypeID == "" && len(serviceTypes) == 1 {
		serviceTypeID = serviceTypes[0].ID
	}

	positionGroups, err := fetchTeamPositions(ctx, serviceTypeID)
	errMsg := ""
	if err != nil {
		errMsg = "loading teams from Planning Center: " + err.Error()
	}

	return render(c, views.PositionPreferences(personName, orgName, serviceTypes, positionGroups, serviceTypeID, errMsg))
}

// fetchTeamPositions lists every position across every team on a service
// type, grouped by team - the position preferences page's dropdown source.
// serviceTypeID == "" (nothing picked yet) returns an empty list rather than
// an error. Like fetchAllSongs, it returns whatever it managed to fetch even
// after a mid-walk error on one team, since a partial list beats an empty
// one.
//
// Each team's GetTeamPositions call is independent of every other team's, so
// they're fanned out via scheduling.RunConcurrently instead of paying every
// team's round trip one at a time - same reasoning as build.go's
// combinedPlansForServiceType. results is pre-sized and index-matched to
// teams.Data (one slot per team, written only by that team's own goroutine,
// so no lock is needed for the writes themselves), which is what keeps the
// final group order matching teams.Data's own OrderBy: "name" order despite
// running concurrently; a nil slot means that team's lookup failed and is
// dropped when results is flattened into groups below, same as the old
// sequential loop's `continue`. "first" error is no longer well-defined once
// teams are queried concurrently, so firstErr here is really just "an" error
// - whichever team's failure is first to reach errOnce.Do - not necessarily
// the one that would have failed first in teams.Data's own order; that's a
// fine trade for a caller that only ever surfaces it as a single generic
// "loading teams from Planning Center" message anyway (see
// PositionPreferencesPage).
func fetchTeamPositions(ctx context.Context, serviceTypeID string) ([]views.PositionGroup, error) {
	if serviceTypeID == "" {
		return nil, nil
	}

	teams, err := pco.GetTeams(ctx, serviceTypeID, &pco.TeamsParams{OrderBy: "name"})
	if err != nil {
		return nil, err
	}

	results := make([]*views.PositionGroup, len(teams.Data))
	var firstErr error
	var errOnce sync.Once

	indexes := make([]int, len(teams.Data))
	for i := range teams.Data {
		indexes[i] = i
	}

	scheduling.RunConcurrently(indexes, func(i int) {
		team := teams.Data[i]
		positions, err := pco.GetTeamPositions(ctx, team.ID, &pco.TeamPositionsParams{OrderBy: "name"})
		if err != nil {
			errOnce.Do(func() { firstErr = err })
			return
		}

		group := views.PositionGroup{TeamID: team.ID, TeamName: team.Attributes.Name}
		for _, pos := range positions.Data {
			group.Positions = append(group.Positions, views.PositionOption{
				TeamID:           team.ID,
				TeamPositionID:   pos.ID,
				TeamPositionName: pos.Attributes.Name,
			})
		}
		// results[i] is this goroutine's own, exclusive index into a
		// pre-sized slice - no lock needed (see RunConcurrently's doc
		// comment).
		results[i] = &group
	})

	var groups []views.PositionGroup
	for _, g := range results {
		if g != nil {
			groups = append(groups, *g)
		}
	}

	return groups, firstErr
}

// schedulePreferenceOptions lists PCO's schedule_preference values for the
// add-preference dropdown. "Choose Weeks" is deliberately left out - this
// app doesn't support entering the custom week list it needs (see
// PersonTeamPositionAssignmentAttributes.PreferredWeeks in go-pco), so
// offering it here would silently save a preference with no weeks chosen.
func schedulePreferenceOptions() []views.SchedulePreferenceOption {
	values := []string{
		pco.SchedulePreferenceUnavailable,
		pco.SchedulePreferenceEveryWeek,
		pco.SchedulePreferenceEveryOtherWeek,
		pco.SchedulePreferenceEvery3rdWeek,
		pco.SchedulePreferenceEvery4thWeek,
		pco.SchedulePreferenceEvery5thWeek,
		pco.SchedulePreferenceEvery6thWeek,
		pco.SchedulePreferenceOnceAMonth,
		pco.SchedulePreferenceTwiceAMonth,
		pco.SchedulePreferenceThreeTimesAMonth,
		pco.SchedulePreferenceOnceAQuarter,
	}
	options := make([]views.SchedulePreferenceOption, len(values))
	for i, v := range values {
		options[i] = views.SchedulePreferenceOption{Value: v, Label: v}
	}
	return options
}

// PositionAssignments is the htmx target for the position dropdown (and the
// add/remove actions within it) - ?position= is "team_id:team_position_id"
// (see position_preferences.templ's option values).
func (sc *SchedulingController) PositionAssignments(c *fiber.Ctx) error {
	teamID, teamPositionID, _ := strings.Cut(c.Query("position"), ":")
	return sc.renderPositionAssignments(c, teamID, teamPositionID, "")
}

func (sc *SchedulingController) SetPositionPreference(c *fiber.Ctx) error {
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

	return sc.renderPositionAssignments(c, body.TeamID, body.TeamPositionID, errMsg)
}

// setPersonTeamPositionPreference creates the person's eligibility+preference
// for this position, or updates it if they're already eligible - PCO has no
// upsert endpoint, so this looks the existing list up first (small and
// already fetched fresh for the same position elsewhere on this page, so the
// extra round trip is cheap).
func setPersonTeamPositionPreference(ctx context.Context, teamID, teamPositionID, personID, schedulePreference string) error {
	existing, err := pco.GetPersonTeamPositionAssignments(ctx, teamID, teamPositionID, nil)
	if err != nil {
		return err
	}

	for _, a := range existing.Data {
		if a.Relationships.Person.Data != nil && a.Relationships.Person.Data.ID == personID {
			_, err := pco.UpdatePersonTeamPositionAssignment(ctx, teamID, teamPositionID, a.ID, &pco.UpdatePersonTeamPositionAssignmentParams{
				SchedulePreference: schedulePreference,
			})
			return err
		}
	}

	_, err = pco.CreatePersonTeamPositionAssignment(ctx, teamID, teamPositionID, &pco.CreatePersonTeamPositionAssignmentParams{
		PersonID:           personID,
		SchedulePreference: schedulePreference,
	})
	return err
}

func (sc *SchedulingController) DeletePositionPreference(c *fiber.Ctx) error {
	teamID := c.Params("team_id")
	teamPositionID := c.Params("team_position_id")
	assignmentID := c.Params("assignment_id")

	errMsg := ""
	if err := pco.DeletePersonTeamPositionAssignment(middleware.RequestContext(c), teamID, teamPositionID, assignmentID); err != nil {
		errMsg = err.Error()
	}

	return sc.renderPositionAssignments(c, teamID, teamPositionID, errMsg)
}

func (sc *SchedulingController) renderPositionAssignments(c *fiber.Ctx, teamID, teamPositionID, errMsg string) error {
	if teamID == "" || teamPositionID == "" {
		return render(c, views.PositionAssignmentsFragment(nil, teamID, teamPositionID, nil, errMsg))
	}

	ctx := middleware.RequestContext(c)
	assignments, err := pco.GetPersonTeamPositionAssignments(ctx, teamID, teamPositionID, nil)
	if err != nil {
		return render(c, views.PositionAssignmentsFragment(nil, teamID, teamPositionID, schedulePreferenceOptions(), appendErr(errMsg, err.Error())))
	}

	ids := make([]string, len(assignments.Data))
	for i, a := range assignments.Data {
		if a.Relationships.Person.Data != nil {
			ids[i] = a.Relationships.Person.Data.ID
		}
	}
	names := personNameMapForIDs(ctx, ids)

	rows := make([]views.PreferenceRow, len(assignments.Data))
	for i, a := range assignments.Data {
		rows[i] = views.PreferenceRow{
			AssignmentID:       a.ID,
			PersonID:           ids[i],
			PersonName:         titleOrID(names, ids[i]),
			SchedulePreference: a.Attributes.SchedulePreference,
		}
	}

	return render(c, views.PositionAssignmentsFragment(rows, teamID, teamPositionID, schedulePreferenceOptions(), errMsg))
}
