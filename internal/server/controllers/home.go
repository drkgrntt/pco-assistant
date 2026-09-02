package controllers

import (
	"context"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// homeServiceTypesCap bounds how many service types the "needs
// attention" widget looks at - a defensive limit, not a real one for the
// single/multi-campus churches this app targets (see the README's
// pricing table), so it's generous rather than tight.
const homeServiceTypesCap = 5

type HomeController struct {
	// schedulingController is shared (constructed once in routes.go, not
	// duplicated) so Home can reuse its private page-builder methods
	// (planStatus, rosterStatus, buildSongTagSuggestionsData,
	// buildSongInfoData) instead of re-implementing any of them - see
	// buildHomeData.
	schedulingController *SchedulingController
	authStore            *auth.Store
}

func NewHomeController(schedulingController *SchedulingController, authStore *auth.Store) *HomeController {
	return &HomeController{schedulingController: schedulingController, authStore: authStore}
}

func (hc *HomeController) RegisterRoutes(router fiber.Router) {
	router.Get("/", hc.Index)
}

// Index serves "/" - the one route RequireAuth treats as soft-auth (see
// its own doc comment) rather than hard-gated, so it's reachable signed
// out. personID is only ever populated by a real session, never faked, so
// checking it is the correct signal for "should this be the public landing
// page or the signed-in dashboard" - same signal CurrentPerson's own doc
// comment already describes.
func (hc *HomeController) Index(c *fiber.Ctx) error {
	personID, personName := middleware.CurrentPerson(c)
	if personID == "" {
		return render(c, views.Landing())
	}

	orgID := middleware.CurrentOrganizationID(c)
	data := hc.buildHomeData(middleware.RequestContext(c), orgID)

	return render(c, views.Home(personName, middleware.CurrentOrganization(c), data))
}

// buildHomeData gathers everything the reworked home page shows - all of
// it reusing logic other pages already have, not new queries: the next
// plan per service type (capped, see homeServiceTypesCap) with its real
// song/team status exactly like the Build page's own plan list computes
// per-row (combinedPlansForServiceType, build.go); the Song Tag
// Suggestions count and Song Info cache coverage, straight from those
// pages' own data-builders; and trial/plan status from organizations,
// same calculation buildBillingData already does. Every piece is
// best-effort - a failed lookup just leaves that section empty, never
// breaks the page.
func (hc *HomeController) buildHomeData(ctx context.Context, organizationID string) views.HomeData {
	var data views.HomeData

	if sts, err := scheduling.FetchServiceTypes(ctx, organizationID); err == nil {
		if len(sts) > homeServiceTypesCap {
			sts = sts[:homeServiceTypesCap]
		}

		// Each service type's own GetPlans + planStatus/rosterStatus is
		// independent of every other's, and both status checks fall back to
		// a live GetItems/GetTeamMembers call whenever there's no local
		// draft yet (see planStatus/rosterStatus's own doc comments) - so a
		// church with homeServiceTypesCap-many service types could pay up
		// to 3 sequential PCO round trips each, one after another, on
		// every single home page load. Fanned out the same way
		// build.go's buildPlanRows already does for the Build page's own
		// plan picker (see docs/pco-api-risks.md's concurrency mitigation) -
		// rows is pre-sized and index-owned per goroutine, with a nil entry
		// meaning "no upcoming plan/error, leave it out", filtered below to
		// preserve service-type order regardless of completion order.
		rows := make([]*views.HomePlanRow, len(sts))
		indexes := make([]int, len(sts))
		for i := range sts {
			indexes[i] = i
		}

		scheduling.RunConcurrently(indexes, func(i int) {
			st := sts[i]
			plans, err := pco.GetPlans(ctx, st.ID, &pco.PlansParams{
				Filter:  "future",
				OrderBy: "sort_date",
				PerPage: 1,
			})
			if err != nil || len(plans.Data) == 0 {
				return
			}

			plan := plans.Data[0]
			rows[i] = &views.HomePlanRow{
				ServiceTypeID:   st.ID,
				PlanID:          plan.ID,
				ServiceTypeName: st.Attributes.Name,
				Title:           plan.Attributes.Title,
				Dates:           plan.Attributes.Dates,
				SetStatus:       hc.schedulingController.planStatus(ctx, organizationID, st.ID, plan.ID),
				RosterStatus:    hc.schedulingController.rosterStatus(ctx, organizationID, st.ID, plan.ID),
			}
		})

		for _, row := range rows {
			if row != nil {
				data.UpcomingPlans = append(data.UpcomingPlans, *row)
			}
		}
	}

	suggestions := hc.schedulingController.buildSongTagSuggestionsData(ctx, organizationID)
	data.SuggestionCount = len(suggestions.Suggestions)

	songInfo := hc.schedulingController.buildSongInfoData(ctx, organizationID)
	data.SongInfoCached = songInfo.CachedCount
	data.SongInfoTotal = songInfo.TotalCount

	if org, err := hc.authStore.GetOrganization(ctx, organizationID); err == nil {
		data.OrgStatus = org.Status
		data.OrgPlan = org.Plan
		if org.Status == auth.OrgStatusTrialing {
			data.TrialDaysLeft = int(time.Until(org.TrialEndsAt).Hours() / 24)
			if data.TrialDaysLeft < 0 {
				data.TrialDaysLeft = 0
			}
		}
	}

	return data
}
