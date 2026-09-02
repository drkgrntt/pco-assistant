package controllers

// CandidateWarnings is the on-demand counterpart to the eager candidate
// warning computation buildRosterBuilderData/buildRosterPositionGroupData
// used to do for every candidate in a position's PCO eligibility pool - see
// pco-api-risks.md's cold-start-eligibility-pool addendum for why that was
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
// and thin PCO-calling wrappers (otherAssignmentDates/
// crossTeamAssignmentsThisWeek/pco.GetBlockouts) the old eager path used -
// just for one person instead of a whole eligibility pool. team_id/
// team_position_name/schedule_preference travel as query params rather than
// being re-derived: they're already known to the page (this candidate's own
// CandidatePersonView.SchedulePreference, and the position group it's
// rendered under), so there's no need to re-fetch PCO's eligibility list
// just to look them back up.
func (sc *SchedulingController) CandidateWarnings(c *fiber.Ctx) error {
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	personID := c.Params("person_id")
	teamID := c.Query("team_id")
	teamPositionName := c.Query("team_position_name")
	schedulePreference := c.Query("schedule_preference")
	ctx := middleware.RequestContext(c)

	view := computeCandidateWarnings(ctx, serviceTypeID, planID, personID, teamID, teamPositionName, schedulePreference)

	return render(c, views.CandidateWarningsFragment(view))
}

// computeCandidateWarnings is CandidateWarnings' actual computation, factored
// out from the fiber handler above so it can be exercised directly against a
// fake PCO server (pco.SetBaseURLForTesting) without needing a real
// *fiber.Ctx - see rosters_candidate_warnings_httptest_test.go, which proves
// this produces the same values the old eager buildRosterBuilderData/
// buildRosterPositionGroupData path used to compute for the same fixture
// data.
func computeCandidateWarnings(ctx context.Context, serviceTypeID, planID, personID, teamID, teamPositionName, schedulePreference string) views.CandidatePersonView {
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

	if schedulePreference != "" {
		otherDates := otherAssignmentDates(ctx, personID, teamID, teamPositionName, planID)
		view.FrequencyWarning = frequencyWarning(schedulePreference, planDate, otherDates)
	}

	crossTeamLabels := crossTeamAssignmentsThisWeek(ctx, personID, planDate, planID)
	view.CrossTeamWarning = crossTeamWarning(crossTeamLabels)

	if bo, err := pco.GetBlockouts(ctx, personID, &pco.BlockoutsParams{Filter: "future"}); err == nil {
		view.BlockoutWarning = blockoutWarningIfKnown(planDate, bo.Data)
	}

	return view
}
