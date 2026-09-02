package controllers

// CandidateWarnings is the on-demand counterpart to the eager candidate
// warning computation buildRosterBuilderData/buildRosterPositionGroupData
// used to do for every candidate in a position's PCO eligibility pool - see
// docs/pco-api-risks.md's cold-start-eligibility-pool addendum for why that was
// scoped down to assigned-only. A director who wants to know whether one
// specific candidate has a conflict clicks "Check availability" on that
// person's own row (roster_builder.templ's positionGroupCard), which hits
// this endpoint and swaps just that row's badge span - never a passive
// trigger (mouseenter/visible/load) that would burst-fetch for every row a
// card happens to render at once.

import (
	"context"
	"time"

	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// CandidateWarnings computes one candidate's FrequencyWarning/
// BlockoutWarning/CrossTeamWarning using the exact same pure functions
// (frequencyWarning/blockoutWarning/crossTeamWarning, rosters_warnings.go)
// the old eager path used - just for one person instead of a whole
// eligibility pool. team_id/team_position_name/schedule_preference travel as
// query params rather than being re-derived: they're already known to the
// page (this candidate's own CandidatePersonView.SchedulePreference, and the
// position group it's rendered under), so there's no need to re-fetch PCO's
// eligibility list just to look them back up.
func (sc *SchedulingController) CandidateWarnings(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	personID := c.Params("person_id")
	teamID := c.Query("team_id")
	teamPositionName := c.Query("team_position_name")
	schedulePreference := c.Query("schedule_preference")
	ctx := middleware.RequestContext(c)

	view := sc.computeCandidateWarnings(ctx, organizationID, serviceTypeID, planID, personID, teamID, teamPositionName, schedulePreference)

	return render(c, views.CandidateWarningsFragment(view))
}

// computeCandidateWarnings is CandidateWarnings' actual computation, factored
// out from the fiber handler above so it can be exercised directly without a
// real *fiber.Ctx - see rosters_candidate_warnings_db_test.go, which proves
// this produces the same values the old eager buildRosterBuilderData/
// buildRosterPositionGroupData path used to compute for the same fixture
// data.
//
// A director clicking "Check availability" wants this candidate's real,
// right-now answer, not whatever's already sitting in the person-data cache
// (which may not exist yet for someone nobody's looked at before) - so
// rather than reading otherAssignmentDates/crossTeamAssignmentsThisWeek/
// personBlockouts (rosters_warnings.go) cache-first the way the assigned-
// roster's own eager warnings now do, this forces a live sync for just this
// one person first (syncPersonBlockouts/syncPersonPlanHistory/
// syncPersonTeamPlanHistory, person_data_cache.go - the exact same
// live-fetch-and-replace functions RefreshPersonData/the background sync
// batch use), then reads the now-freshly-populated cache. Same "bounded to
// one item, triggered by an explicit click, live PCO calls are fine"
// reasoning as RefreshPersonData/RefreshSetSongArrangementInfo - this is
// just that same reasoning applied to a candidate instead of an assignment.
// Best-effort throughout: a sync failure just leaves that one warning off,
// same tolerance every other lookup on this page already has.
func (sc *SchedulingController) computeCandidateWarnings(ctx context.Context, organizationID, serviceTypeID, planID, personID, teamID, teamPositionName, schedulePreference string) views.CandidatePersonView {
	var planDate time.Time
	if plan, err := pco.GetPlan(ctx, serviceTypeID, planID); err == nil {
		planDate = plan.Data.Attributes.SortDate
	}

	view := views.CandidatePersonView{
		PersonID:           personID,
		SchedulePreference: schedulePreference,
	}

	if planDate.IsZero() {
		return view
	}

	_ = syncPersonBlockouts(ctx, sc.store, organizationID, personID)
	view.BlockoutWarning = blockoutWarningIfKnown(planDate, sc.personBlockouts(ctx, organizationID, personID))

	if schedulePreference != "" {
		_ = syncPersonTeamPlanHistory(ctx, sc.store, organizationID, personID, teamID)
		otherDates := sc.otherAssignmentDates(ctx, organizationID, personID, teamID, teamPositionName, planID)
		view.FrequencyWarning = frequencyWarning(schedulePreference, planDate, otherDates)
	}

	_ = syncPersonPlanHistory(ctx, sc.store, organizationID, personID)
	crossTeamLabels := sc.crossTeamAssignmentsThisWeek(ctx, organizationID, personID, planDate, planID)
	view.CrossTeamWarning = crossTeamWarning(crossTeamLabels)

	return view
}
