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

// homeLookAheadPlansCap bounds how many plans past the chronologically-next
// one get checked for the "needs attention" widget's second row (see
// buildHomeData) when that next plan is already fully planned. Without a
// bound, a service type whose next several months are all fully planned
// ahead of time would mean paging forward indefinitely just to render the
// home page - unbounded PCO-call cost on a hot render path, exactly what
// docs/pco-api-risks.md warns against. Capped at 3: small enough that the
// worst case (every one of those 3 candidates needs its own
// planStatus/rosterStatus check, each of which can itself fall back to a
// live PCO call - see those methods' doc comments) stays bounded and
// cheap, one service type's look-ahead work fanned out concurrently
// alongside every other service type's (see buildHomeData's
// scheduling.RunConcurrently call). If none of those 3 turn up unplanned,
// the widget simply shows nothing extra for that service type rather than
// looking further - a deliberately conservative trade (a plan that's
// unplanned 4+ services out won't surface here) in exchange for a bounded
// cost per home page load.
const homeLookAheadPlansCap = 3

type HomeController struct {
	// schedulingController is shared (constructed once in routes.go, not
	// duplicated) so Home can reuse its private page-builder methods
	// (planStatus, rosterStatus, songTagSuggestionPendingCount) instead of
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
// per-row (combinedPlansForServiceType, build.go); the Song Tags page's
// own Suggestions panel pending count (songTagSuggestionPendingCount,
// song_tags.go); and trial/plan status from organizations, same calculation buildBillingData
// already does. Every piece is best-effort - a failed lookup just leaves
// that section empty, never breaks the page. No Song Info cache coverage
// here any more - that nudge is gone now that a miss syncs itself in the
// background instead of needing anyone to notice and click something (see
// docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2).
//
// Each service type's row can now be followed by a second, bounded
// look-ahead row (see homeLookAheadPlansCap) when the chronologically-next
// plan is already fully planned on both Set and Roster status - otherwise
// the widget would only ever point at a plan with nothing left to do,
// while the actual next *unplanned* plan sits further out, unseen. The
// extra candidates for that look-ahead ride the exact same GetPlans call
// as the primary "next plan" fetch (PerPage bumped from 1 to
// 1+homeLookAheadPlansCap) - zero additional GetPlans calls, since PCO
// charges the same for a small page regardless of PerPage. The only added
// cost is the planStatus/rosterStatus checks themselves, and only when the
// primary plan is already "planned": those run in plan order, stopping at
// the first candidate that isn't fully planned, so a service type whose
// very next unplanned plan is the second one checked pays for exactly one
// extra status check, not homeLookAheadPlansCap-many.
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

		// lookAheadRows mirrors rows - one slot per service type, filled in
		// only when that service type's next plan is already fully planned
		// and a later, still-unplanned candidate was found within
		// homeLookAheadPlansCap plans (see that const's doc comment).
		lookAheadRows := make([]*views.HomePlanRow, len(sts))

		scheduling.RunConcurrently(indexes, func(i int) {
			st := sts[i]
			plans, err := pco.GetPlans(ctx, st.ID, &pco.PlansParams{
				Filter:  "future",
				OrderBy: "sort_date",
				PerPage: 1 + homeLookAheadPlansCap,
			})
			if err != nil || len(plans.Data) == 0 {
				return
			}

			plan := plans.Data[0]
			row := &views.HomePlanRow{
				ServiceTypeID:   st.ID,
				PlanID:          plan.ID,
				ServiceTypeName: st.Attributes.Name,
				Title:           plan.Attributes.Title,
				Dates:           plan.Attributes.Dates,
				SetStatus:       hc.schedulingController.planStatus(ctx, organizationID, st.ID, plan.ID),
				RosterStatus:    hc.schedulingController.rosterStatus(ctx, organizationID, st.ID, plan.ID),
			}
			rows[i] = row

			// Only look further if the very next plan is already fully
			// planned - otherwise this row already needs attention and
			// there's nothing more useful to add underneath it.
			if row.SetStatus != "planned" || row.RosterStatus != "planned" {
				return
			}

			for _, candidate := range plans.Data[1:] {
				setStatus := hc.schedulingController.planStatus(ctx, organizationID, st.ID, candidate.ID)
				rosterStatus := hc.schedulingController.rosterStatus(ctx, organizationID, st.ID, candidate.ID)
				if setStatus == "planned" && rosterStatus == "planned" {
					continue
				}
				lookAheadRows[i] = &views.HomePlanRow{
					ServiceTypeID:   st.ID,
					PlanID:          candidate.ID,
					ServiceTypeName: st.Attributes.Name,
					Title:           candidate.Attributes.Title,
					Dates:           candidate.Attributes.Dates,
					SetStatus:       setStatus,
					RosterStatus:    rosterStatus,
					IsNextUnplanned: true,
				}
				break
			}
		})

		for i, row := range rows {
			if row == nil {
				continue
			}
			data.UpcomingPlans = append(data.UpcomingPlans, *row)
			if lookAheadRows[i] != nil {
				data.UpcomingPlans = append(data.UpcomingPlans, *lookAheadRows[i])
			}
		}
	}

	data.SuggestionCount = hc.schedulingController.songTagSuggestionPendingCount(ctx, organizationID)

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
// HomeData.Onboarding's doc comment) - nine cheap local-DB existence
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
	hasSongContexts, _ := store.HasAnySongContexts(ctx, organizationID)
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
					{Label: "Song contexts", Description: "Big church, youth, another campus - wherever a song actually belongs.", Href: "/scheduling/song-contexts", Done: hasSongContexts, Trackable: true},
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
