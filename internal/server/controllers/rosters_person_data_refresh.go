package controllers

// RefreshPersonData is the manual, single-person counterpart to the
// person-data cache's paced background sync (person_data_cache.go) - a
// director clicking "Refresh availability data" on one person's own
// sidebar detail card (person_detail_card.templ) instead of waiting for
// that person's blockouts/serving-history cache to come up in the
// background queue's own turn. Runs the exact same per-kind sync functions
// (syncPersonBlockouts/syncPersonPlanHistory/syncPersonTeamPlanHistory) the
// background pass calls per person, just synchronously and immediately for
// this one person - see pco-api-risks.md's manual-refresh addendum for why
// a live PCO call here (three of them, one per cache) is fine: bounded to
// one person, triggered by an explicit click, not a hot render path, same
// reasoning as RefreshSetSongArrangementInfo (song_arrangement_cache.go).

import (
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

func (sc *SchedulingController) RefreshPersonData(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	ctx := middleware.RequestContext(c)

	assignmentID, err := c.ParamsInt("assignment_id")
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, "assignment_id must be an integer")))
	}

	errMsg := ""
	var target scheduling.RosterAssignment
	found := false

	// Looked up first, the same way RemoveRosterAssignment does, since the
	// sync calls below need this assignment's person/team ids up front -
	// there's nothing else in this request that already carries them (the
	// button posts no body; assignment_id alone is all the URL needs to
	// stay stable across a page reload).
	if roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID); err != nil {
		errMsg = err.Error()
	} else {
		for _, ra := range assignments {
			if ra.ID == int64(assignmentID) {
				target = ra
				found = true
				break
			}
		}
	}

	if !found {
		// Row's already gone (double-click, stale page, or the lookup
		// above failed outright) - nothing to scope a refresh to. Same
		// "fall back to a full body render" reasoning as
		// RemoveRosterAssignment's own not-found path.
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, appendErr(errMsg, "assignment is no longer on this roster"))))
	}

	if err := syncPersonBlockouts(ctx, sc.store, orgID, target.PersonID); err != nil {
		errMsg = appendErr(errMsg, "blockouts: "+err.Error())
	}
	if err := syncPersonPlanHistory(ctx, sc.store, orgID, target.PersonID); err != nil {
		errMsg = appendErr(errMsg, "cross-team history: "+err.Error())
	}
	if err := syncPersonTeamPlanHistory(ctx, sc.store, orgID, target.PersonID, target.TeamID); err != nil {
		errMsg = appendErr(errMsg, "team history: "+err.Error())
	}

	// See AddRosterAssignment/RemoveRosterAssignment's own comment on
	// fetching this once and sharing it between both builders below.
	shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	group, groupErrMsg := sc.buildRosterPositionGroupData(ctx, orgID, serviceTypeID, planID, target.TeamID, target.TeamPositionName, shared)
	top := sc.buildRosterLightData(ctx, orgID, serviceTypeID, planID, shared)
	top.ErrMsg = appendErr(appendErr(errMsg, groupErrMsg), top.ErrMsg)

	refreshed := assignmentInGroup(group, int64(assignmentID))
	if refreshed == nil {
		// Shouldn't happen - target was found on the roster a moment ago,
		// and neither sync above adds/removes an assignment - but mirrors
		// buildRosterPositionGroupData's own "never error out over a merge
		// miss" stance rather than panicking on a nil dereference.
		refreshed = &views.AssignmentView{ID: int64(assignmentID)}
	}

	return render(c, views.RosterPersonDataRefreshResponse(serviceTypeID, planID, group, top, *refreshed))
}
