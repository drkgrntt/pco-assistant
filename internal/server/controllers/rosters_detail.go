package controllers

// Per-assignment roster builder mutations: add/remove a person, plus the
// quick-add panels for relationships and position preferences.

import (
	"context"
	"database/sql"
	"errors"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

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
