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
	// (planStatus, rosterStatus, buildSongTagSuggestionsData) instead of
	// re-implementing any of them - see buildHomeData.
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
// Suggestions count, straight from that page's own data-builder; and
// trial/plan status from organizations, same calculation buildBillingData
// already does. Every piece is best-effort - a failed lookup just leaves
// that section empty, never breaks the page. No Song Info cache coverage
// here any more - that nudge is gone now that a miss syncs itself in the
// background instead of needing anyone to notice and click something (see
// docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2).
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

	data.Onboarding = hc.buildOnboardingChecklist(ctx, organizationID)

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

// buildOnboardingChecklist builds the home page's setup checklist (see
// HomeData.Onboarding's doc comment) - eight cheap local-DB existence
// checks (not PCO calls, so no rate-limit concern per
// docs/pco-api-risks.md), one per setup category, run sequentially since
// each is a single indexed EXISTS query, not worth fanning out via
// scheduling.RunConcurrently the way the PCO calls above are. A failing
// check just leaves that one item "not done" (best-effort, same tolerance
// as the rest of this file) rather than erroring the whole checklist.
//
// Position Preferences is the one item with no store check at all - see
// OnboardingItem.Trackable's doc comment for why (that data lives in PCO,
// not this app's own database).
func (hc *HomeController) buildOnboardingChecklist(ctx context.Context, organizationID string) views.OnboardingChecklist {
	store := hc.schedulingController.store
	hasSongRels, _ := store.HasAnySongRelationships(ctx, organizationID)
	hasSongRoles, _ := store.HasAnySongRoles(ctx, organizationID)
	hasSongTags, _ := store.HasAnySongTags(ctx, organizationID)
	hasSongVocalRanges, _ := store.HasAnySongVocalRanges(ctx, organizationID)
	hasSongPersonPrefs, _ := store.HasAnySongPersonPreferences(ctx, organizationID)
	hasPersonRels, _ := store.HasAnyPersonRelationships(ctx, organizationID)
	hasPersonVocalRanges, _ := store.HasAnyPersonVocalRanges(ctx, organizationID)

	checklist := views.OnboardingChecklist{
		Groups: []views.OnboardingGroup{
			{
				Title: "Protect the service",
				Intro: "Feeds the warnings that catch repetition, a bad pairing, or a set that doesn't flow, before the plan goes out.",
				Items: []views.OnboardingItem{
					{Label: "Song relationships", Description: "Flows well, same theme, or avoid pairing.", Href: "/scheduling/song-relationships", Done: hasSongRels, Trackable: true},
					{Label: "Song roles", Description: "Opener, response, or any role you add.", Href: "/scheduling/song-roles", Done: hasSongRoles, Trackable: true},
					{Label: "Song tags", Description: "Theme, scripture, and idea tags for search and filtering.", Href: "/scheduling/song-tags", Done: hasSongTags, Trackable: true},
					{Label: "Song vocal range", Description: "Each song's sung range, so a mismatch gets caught before rehearsal.", Href: "/scheduling/song-vocal-range", Done: hasSongVocalRanges, Trackable: true},
				},
			},
			{
				Title: "Protect the team",
				Intro: "Feeds the warnings and suggestions that respect stated preferences and catch relational friction before the roster goes out.",
				Items: []views.OnboardingItem{
					{Label: "Song / person preferences", Description: "Note who's preferred or discouraged for a song.", Href: "/scheduling/song-preferences", Done: hasSongPersonPrefs, Trackable: true},
					{Label: "Person relationships", Description: "Works well, mentorship, or avoid pairing between people.", Href: "/scheduling/person-relationships", Done: hasPersonRels, Trackable: true},
					{Label: "Position preferences", Description: "How often someone wants to serve a position - synced live with Planning Center, so it isn't tracked here.", Href: "/scheduling/position-preferences", Trackable: false},
					{Label: "Vocal range", Description: "Each vocalist's comfortable range - a faster, less awkward \"can they hit this note.\"", Href: "/scheduling/vocal-range", Done: hasPersonVocalRanges, Trackable: true},
				},
			},
		},
	}

	for _, group := range checklist.Groups {
		for _, item := range group.Items {
			if !item.Trackable {
				continue
			}
			checklist.Trackable++
			if item.Done {
				checklist.Done++
			}
		}
	}

	return checklist
}
