package controllers

import (
	"context"

	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Build (the combined song/team workflow) ---------------------------------

// BuildPage renders the service-type picker. With only one service type,
// there's nothing to actually pick, so its plans are loaded immediately
// instead of waiting for a submit that would only ever have one option.
func (sc *SchedulingController) BuildPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)

	ctx := middleware.RequestContext(c)
	serviceTypes, err := pco.GetServiceTypes(ctx, nil)
	if err != nil {
		return render(c, views.Build(personName, nil, "", nil, err.Error()))
	}

	if len(serviceTypes.Data) == 1 {
		serviceTypeID := serviceTypes.Data[0].ID
		rows, errMsg := sc.combinedPlansForServiceType(ctx, serviceTypeID)
		return render(c, views.Build(personName, serviceTypes.Data, serviceTypeID, rows, errMsg))
	}

	return render(c, views.Build(personName, serviceTypes.Data, "", nil, ""))
}

// BuildPlans lists a service type's upcoming plans, each linking to its
// combined builder - the htmx target for the service-type picker on the
// Build page.
func (sc *SchedulingController) BuildPlans(c *fiber.Ctx) error {
	serviceTypeID := c.Query("service_type_id")
	if serviceTypeID == "" {
		return render(c, views.BuildPlansList(nil, "", ""))
	}

	rows, errMsg := sc.combinedPlansForServiceType(middleware.RequestContext(c), serviceTypeID)
	return render(c, views.BuildPlansList(rows, serviceTypeID, errMsg))
}

// combinedPlansForServiceType is the Build page's plan-picker source - one
// PCO plans fetch, with both a song-lineup status (planStatus, in sets.go)
// and a team status (rosterStatus, in rosters.go) computed per row, since a
// plan can be e.g. fully planned on songs but still todo on people.
func (sc *SchedulingController) combinedPlansForServiceType(ctx context.Context, serviceTypeID string) ([]views.PlanRow, string) {
	plans, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{Filter: "future", OrderBy: "sort_date"})
	if err != nil {
		return nil, err.Error()
	}

	rows := make([]views.PlanRow, len(plans.Data))
	for i, plan := range plans.Data {
		rows[i] = views.PlanRow{
			ID:           plan.ID,
			Title:        plan.Attributes.Title,
			Dates:        plan.Attributes.Dates,
			SetStatus:    sc.planStatus(ctx, serviceTypeID, plan.ID),
			RosterStatus: sc.rosterStatus(ctx, serviceTypeID, plan.ID),
		}
	}
	return rows, ""
}

// BuildPlanPage renders one plan's combined builder - the song order of
// service and the team, stacked on one page (see BuildPlan in
// build.templ). Each section is independently interactive (its own htmx
// swap target, its own add/remove/publish/discard actions, still posting
// to the existing /scheduling/sets/... and /scheduling/rosters/...
// endpoints) - this page just displays both at once instead of requiring a
// separate visit to build each.
func (sc *SchedulingController) BuildPlanPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)

	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	setData := sc.buildSetBuilderData(ctx, serviceTypeID, planID)
	rosterData := sc.buildRosterBuilderData(ctx, serviceTypeID, planID)

	return render(c, views.BuildPlan(personName, setData, rosterData))
}
