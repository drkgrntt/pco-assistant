package controllers

// Per-assignment roster builder mutations: add/remove a person, plus the
// quick-add panels for relationships and position preferences. Each of
// these renders a narrow htmx fragment scoped to whatever it actually
// touched (the one position group affected, or - for a relationship,
// which touches no position at all - just the shared "roster-top" banner
// block) rather than the whole #roster-builder-body, using
// buildRosterPositionGroupData/buildRosterLightData (rosters_scoped.go)
// instead of a full buildRosterBuilderData rebuild. See roster_builder.
// templ's PositionGroupElementID/positionGroupCard/rosterTop for the
// fragments these produce, and rosters_publish.go's PublishRoster/
// RevertRoster for the two roster mutations that still legitimately
// re-render the whole tab (both can change every position at once by
// design, so there's no single group to scope down to).

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
	ctx := middleware.RequestContext(c)

	var body struct {
		PersonID         string `form:"person_id"`
		TeamID           string `form:"team_id"`
		TeamPositionName string `form:"team_position_name"`
	}
	errMsg := ""
	previousPersonID := ""
	var newAssignmentID int64
	added := false
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else {
		// Captured before the add, so it reflects who was already on the
		// roster, not (once AddPersonToPosition below succeeds) the person
		// just added themselves.
		previousPersonID = sc.previouslyAddedPersonID(ctx, roster.ID)
		if ra, err := sc.store.AddPersonToPosition(ctx, roster.ID, body.PersonID, body.TeamID, body.TeamPositionName); err != nil {
			errMsg = err.Error()
			previousPersonID = "" // the add itself failed - nothing to prompt about
		} else {
			newAssignmentID = ra.ID
			added = true
		}
	}

	// Fetched once and handed to both builders below - see
	// rosterSharedPCOData's doc comment (rosters_scoped.go): without this,
	// buildRosterPositionGroupData and buildRosterLightData would each make
	// their own GetTeamMembers/GetNeededPositions calls for the same plan a
	// moment apart, doubling this handler's PCO cost for no reason.
	shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	group, groupErrMsg := sc.buildRosterPositionGroupData(ctx, orgID, serviceTypeID, planID, body.TeamID, body.TeamPositionName, shared)
	top := sc.buildRosterLightData(ctx, orgID, serviceTypeID, planID, shared)
	sc.triggerPersonDataSync(c)
	top.ErrMsg = appendErr(appendErr(errMsg, groupErrMsg), top.ErrMsg)

	var addedView *views.AssignmentView
	if added {
		addedView = assignmentInGroup(group, newAssignmentID)
	}

	// PersonPairPrompt: only ever set here, right after a successful add
	// that actually had someone else already on the roster to pair with -
	// see RosterBuilderData.PersonPairPrompt's doc comment and
	// SetBuilderData.SongPairPrompt, which this mirrors. Every other
	// mutation (remove/publish/revert/quick-add, or acting on the prompt
	// itself) builds its own response fresh without it, so it disappears
	// on its own the moment you do anything else. previousPersonID !=
	// body.PersonID guards the edge case where the "most recently added"
	// person is the same one just added to a second position - relating
	// someone to themselves makes no sense, so no prompt in that case.
	// Names come from top.Positions (buildRosterLightData resolves every
	// assigned person's name across the whole roster, not just this one
	// group) since previousPersonID's own assignment could be on a
	// different position entirely.
	if added && previousPersonID != "" && previousPersonID != body.PersonID {
		prompt := &views.PersonPairPromptView{PreviousPersonID: previousPersonID, NewPersonID: body.PersonID}
		for _, pos := range top.Positions {
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
			top.PersonPairPrompt = prompt
		}
	}

	leaderData, leaderSongs, _ := sc.buildLeaderFormRefreshViews(ctx, orgID, serviceTypeID, planID, top)
	return render(c, views.RosterAssignmentAddedResponse(serviceTypeID, planID, group, top, addedView, leaderData, leaderSongs))
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
	ctx := middleware.RequestContext(c)

	errMsg := ""
	var removed scheduling.RosterAssignment
	found := false

	rowID, err := c.ParamsInt("assignment_id")
	if err != nil {
		errMsg = "assignment_id must be an integer"
	} else if roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else {
		// Looked up before the delete (RemovePersonFromPosition either
		// hard-deletes the row or tombstones it) - the touched group and
		// the sidebar detail card to remove both need this row's team/
		// position, which won't be readable afterwards.
		if existing, err := sc.store.ListRosterAssignments(ctx, roster.ID); err == nil {
			for _, ra := range existing {
				if ra.ID == int64(rowID) {
					removed = ra
					found = true
					break
				}
			}
		}
		if err := sc.store.RemovePersonFromPosition(ctx, roster.ID, int64(rowID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
			errMsg = err.Error()
		}
	}

	if !found {
		// Row was already gone (double-click, stale page, or the
		// assignment_id/lookup above failed outright) - nothing to scope a
		// group refresh to. Falling back to a full body render still
		// leaves the page correct, just not narrowly, for what should be a
		// rare edge case rather than the common path.
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, errMsg)))
	}

	// See AddRosterAssignment's own comment on this same pattern.
	shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	group, groupErrMsg := sc.buildRosterPositionGroupData(ctx, orgID, serviceTypeID, planID, removed.TeamID, removed.TeamPositionName, shared)
	top := sc.buildRosterLightData(ctx, orgID, serviceTypeID, planID, shared)
	sc.triggerPersonDataSync(c)
	top.ErrMsg = appendErr(appendErr(errMsg, groupErrMsg), top.ErrMsg)

	leaderData, leaderSongs, _ := sc.buildLeaderFormRefreshViews(ctx, orgID, serviceTypeID, planID, top)
	return render(c, views.RosterAssignmentRemovedResponse(serviceTypeID, planID, group, top, int64(rowID), leaderData, leaderSongs))
}

// QuickAddPersonRelationship/QuickAddPositionPreference back the roster
// builder's quick-add panels (roster_builder.templ) - the same
// setup-level entities as PersonRelationshipsPage/PositionPreferencesPage's
// own create forms (same underlying operations - CreatePersonRelationship,
// setPersonTeamPositionPreference from people.go), just posted from the
// build page. Deliberately create-only, same as the set builder's three
// quick-add handlers in sets.go.

// QuickAddPersonRelationship creates a works-well/mentorship/avoid-pairing
// relationship between two people - unlike the assignment/preference
// mutations below, this never touches any specific position's assignments
// or candidates (relationships aren't stored per-position), so it only
// ever needs to refresh the shared roster-top banner block (existing-
// conflicts can gain or lose a pair; the import banner and pair-prompt can
// disappear) rather than any position group's own card.
func (sc *SchedulingController) QuickAddPersonRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	ctx := middleware.RequestContext(c)

	var body scheduling.CreatePersonRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreatePersonRelationship(ctx, orgID, body); err != nil {
		errMsg = err.Error()
	}

	// nil: this handler never calls buildRosterPositionGroupData, so
	// there's no second consumer to share a prefetch with - buildRosterLightData
	// just fetches its own copy, same as before this shared-data parameter
	// existed.
	top := sc.buildRosterLightData(ctx, orgID, serviceTypeID, planID, nil)
	top.ErrMsg = appendErr(errMsg, top.ErrMsg)

	return render(c, views.RosterTopMutationResponse(top))
}

// QuickAddPositionPreference sets one person's eligibility/schedule
// preference for one team position, straight against Planning Center (see
// setPersonTeamPositionPreference - this writes immediately, unlike
// assignments/relationships, since PCO has no "draft" concept for a
// PersonTeamPositionAssignment). team_position_name travels as its own
// hidden field alongside team_position_id (see roster_builder.templ's
// positionGroupCard) so the touched group can be re-resolved by
// buildRosterPositionGroupData - which is keyed on team/position *name*,
// matching groupNeededPositions - without an extra PCO round trip to
// translate the id back to a name.
func (sc *SchedulingController) QuickAddPositionPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	ctx := middleware.RequestContext(c)

	var body struct {
		TeamID             string `form:"team_id"`
		TeamPositionID     string `form:"team_position_id"`
		TeamPositionName   string `form:"team_position_name"`
		PersonID           string `form:"person_id"`
		SchedulePreference string `form:"schedule_preference"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.TeamID == "" || body.TeamPositionID == "" || body.PersonID == "" {
		errMsg = "team, position, and person are required"
	} else if err := setPersonTeamPositionPreference(ctx, body.TeamID, body.TeamPositionID, body.PersonID, body.SchedulePreference); err != nil {
		errMsg = err.Error()
	}

	// See AddRosterAssignment's own comment on this same pattern.
	shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	group, groupErrMsg := sc.buildRosterPositionGroupData(ctx, orgID, serviceTypeID, planID, body.TeamID, body.TeamPositionName, shared)
	top := sc.buildRosterLightData(ctx, orgID, serviceTypeID, planID, shared)
	sc.triggerPersonDataSync(c)
	top.ErrMsg = appendErr(appendErr(errMsg, groupErrMsg), top.ErrMsg)

	return render(c, views.RosterPositionGroupMutationResponse(serviceTypeID, planID, group, top))
}
