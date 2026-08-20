package controllers

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Person relationships & position preferences ---------------------------

func (sc *SchedulingController) PersonRelationshipsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)

	relationships, err := sc.store.ListPersonRelationships(c.Context(), "")
	if err != nil {
		return render(c, views.PersonRelationships(personName, relationships, nil, err.Error()))
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))

	return render(c, views.PersonRelationships(personName, relationships, names, ""))
}

func (sc *SchedulingController) CreatePersonRelationship(c *fiber.Ctx) error {
	var body scheduling.CreatePersonRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreatePersonRelationship(c.Context(), body); err != nil {
		errMsg = err.Error()
	}

	relationships, err := sc.store.ListPersonRelationships(c.Context(), "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))

	return render(c, views.PersonRelationshipsTable(relationships, names, errMsg))
}

func (sc *SchedulingController) DeletePersonRelationship(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeletePersonRelationship(c.Context(), int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	relationships, err := sc.store.ListPersonRelationships(c.Context(), "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))

	return render(c, views.PersonRelationshipsTable(relationships, names, errMsg))
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

// personNameMapForIDs resolves a specific set of PCO person ids to display
// names - like personNameMap in scheduling.go, but for callers (person
// relationships, rosters) that already know exactly which ids they need
// rather than deriving them from a preferences list. An id that fails to
// resolve is simply left out; callers fall back to showing the raw id.
func personNameMapForIDs(ctx context.Context, ids []string) map[string]string {
	names := make(map[string]string, len(ids))
	for _, id := range ids {
		if person, err := pco.GetPerson(ctx, id); err == nil {
			names[id] = person.Data.Attributes.Name
		}
	}
	return names
}

// --- Position preferences ---------------------------------------------------

func (sc *SchedulingController) PositionPreferencesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)

	ctx := middleware.RequestContext(c)
	serviceTypes, err := pco.GetServiceTypes(ctx, nil)
	if err != nil {
		return render(c, views.PositionPreferences(personName, nil, nil, "", err.Error()))
	}

	serviceTypeID := c.Query("service_type_id")
	if serviceTypeID == "" && len(serviceTypes.Data) == 1 {
		serviceTypeID = serviceTypes.Data[0].ID
	}

	positionGroups, err := fetchTeamPositions(ctx, serviceTypeID)
	errMsg := ""
	if err != nil {
		errMsg = "loading teams from Planning Center: " + err.Error()
	}

	return render(c, views.PositionPreferences(personName, serviceTypes.Data, positionGroups, serviceTypeID, errMsg))
}

// fetchTeamPositions lists every position across every team on a service
// type, grouped by team - the position preferences page's dropdown source.
// serviceTypeID == "" (nothing picked yet) returns an empty list rather than
// an error. Like fetchAllSongs, it returns whatever it managed to fetch even
// after a mid-walk error on one team, since a partial list beats an empty
// one.
func fetchTeamPositions(ctx context.Context, serviceTypeID string) ([]views.PositionGroup, error) {
	if serviceTypeID == "" {
		return nil, nil
	}

	teams, err := pco.GetTeams(ctx, serviceTypeID, &pco.TeamsParams{OrderBy: "name"})
	if err != nil {
		return nil, err
	}

	var groups []views.PositionGroup
	var firstErr error
	for _, team := range teams.Data {
		positions, err := pco.GetTeamPositions(ctx, team.ID, &pco.TeamPositionsParams{OrderBy: "name"})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		group := views.PositionGroup{TeamID: team.ID, TeamName: team.Attributes.Name}
		for _, pos := range positions.Data {
			group.Positions = append(group.Positions, views.PositionOption{
				TeamID:           team.ID,
				TeamPositionID:   pos.ID,
				TeamPositionName: pos.Attributes.Name,
			})
		}
		groups = append(groups, group)
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
