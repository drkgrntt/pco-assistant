package controllers

import (
	"context"
	"strconv"
	"strings"

	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Build (the combined song/team workflow) ---------------------------------

// buildPlansFetchPageSize/MaxPages bound fetchUpcomingPlans' PCO paging,
// same reasoning as pcoSongPickerPageSize/MaxPages. buildPlansPageSize is
// this app's own display page size for the plan picker.
const (
	buildPlansFetchPageSize = 100
	buildPlansFetchMaxPages = 20
	buildPlansPageSize      = 10
)

// BuildPage renders the service-type picker. With only one service type,
// there's nothing to actually pick, so its plans are loaded immediately
// instead of waiting for a submit that would only ever have one option.
func (sc *SchedulingController) BuildPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	serviceTypes, err := pco.GetServiceTypes(ctx, nil)
	if err != nil {
		return render(c, views.Build(personName, orgName, nil, "", nil, 1, 1, err.Error()))
	}

	if len(serviceTypes.Data) == 1 {
		serviceTypeID := serviceTypes.Data[0].ID
		rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(ctx, orgID, serviceTypeID, "", 1)
		return render(c, views.Build(personName, orgName, serviceTypes.Data, serviceTypeID, rows, page, totalPages, errMsg))
	}

	return render(c, views.Build(personName, orgName, serviceTypes.Data, "", nil, 1, 1, ""))
}

// BuildPlans lists a freshly-chosen service type's upcoming plans (search
// blank, page 1) - the htmx target for the service-type picker on the
// Build page. Unlike BuildPlansSearch below, this re-renders the whole
// picker block (search box included), since switching service types is a
// clean slate.
func (sc *SchedulingController) BuildPlans(c *fiber.Ctx) error {
	serviceTypeID := c.Query("service_type_id")
	if serviceTypeID == "" {
		return render(c, views.BuildPlansList(nil, "", "", 1, 1, ""))
	}

	rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(middleware.RequestContext(c), middleware.CurrentOrganizationID(c), serviceTypeID, "", 1)
	return render(c, views.BuildPlansList(rows, serviceTypeID, "", page, totalPages, errMsg))
}

// BuildPlansSearch backs the plan picker's search box and pagination links -
// unlike BuildPlans above, it renders only the inner results fragment
// (views.BuildPlansResults), never the search box itself, so typing a
// search term never replaces the input the user is typing into (which
// would drop focus/cursor position mid-keystroke - see the person-search
// picker's own form-reset bug for what this class of mistake looks like).
// page defaults to 1 when absent (e.g. every request the search box itself
// fires, since it doesn't carry a page field) - a new search term should
// always restart from the first page of matches.
func (sc *SchedulingController) BuildPlansSearch(c *fiber.Ctx) error {
	serviceTypeID := c.Query("service_type_id")
	search := c.Query("search")
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}

	if serviceTypeID == "" {
		return render(c, views.BuildPlansResults(nil, "", "", 1, 1, ""))
	}

	rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(middleware.RequestContext(c), middleware.CurrentOrganizationID(c), serviceTypeID, search, page)
	return render(c, views.BuildPlansResults(rows, serviceTypeID, search, page, totalPages, errMsg))
}

// fetchUpcomingPlans pages through a service type's future plans (id,
// title, dates only - status is deliberately computed separately, only for
// whichever page is actually being displayed, since that's the expensive
// part - see combinedPlansForServiceType), for the Build page's plan picker
// to search/paginate over locally. Same "return whatever was fetched so
// far" tolerance as fetchAllSongs, and fixes a latent bug this replaces:
// the previous single unpaginated GetPlans call silently relied on PCO's
// default per_page, so a service type with many upcoming plans (recurring
// templates scheduled well ahead) could have been quietly truncated.
func fetchUpcomingPlans(ctx context.Context, serviceTypeID string) ([]pco.PlanData, error) {
	var plans []pco.PlanData

	for page := 0; page < buildPlansFetchMaxPages; page++ {
		resp, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
			Filter:  "future",
			OrderBy: "sort_date",
			PerPage: buildPlansFetchPageSize,
			Offset:  page * buildPlansFetchPageSize,
		})
		if err != nil {
			return plans, err
		}

		plans = append(plans, resp.Data...)

		if len(resp.Data) < buildPlansFetchPageSize || len(plans) >= resp.Meta.TotalCount {
			break
		}
	}

	return plans, nil
}

// filterUpcomingPlans keeps plans whose title or dates contain search
// (case-insensitive); "" matches everything. Dates is included in the
// haystack because a plan without a custom title - common, PCO defaults to
// just showing the date - would otherwise be unsearchable by the only text
// actually visible for it in the list.
func filterUpcomingPlans(plans []pco.PlanData, search string) []pco.PlanData {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return plans
	}

	filtered := make([]pco.PlanData, 0, len(plans))
	for _, plan := range plans {
		haystack := strings.ToLower(plan.Attributes.Title + " " + plan.Attributes.Dates)
		if strings.Contains(haystack, search) {
			filtered = append(filtered, plan)
		}
	}
	return filtered
}

// paginateUpcomingPlans slices filtered down to the requested page
// (buildPlansPageSize rows), clamping page into [1, totalPages] first - so
// e.g. requesting a page past the end (a new search narrowed what "page 3"
// used to mean) falls back to the last real page instead of an empty
// out-of-range slice. Pulled out of combinedPlansForServiceType so this
// arithmetic is testable without a PCO round trip.
func paginateUpcomingPlans(filtered []pco.PlanData, page int) (pageOfPlans []pco.PlanData, currentPage, totalPages int) {
	totalPages = (len(filtered) + buildPlansPageSize - 1) / buildPlansPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * buildPlansPageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + buildPlansPageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], page, totalPages
}

// combinedPlansForServiceType is the Build page's plan-picker source: fetch
// once, filter by search, paginate, and only then compute each shown row's
// song-lineup status (planStatus, in sets.go) and team status (rosterStatus,
// in rosters.go) - each of those is its own local/PCO lookup, so doing it
// per-row only for the up-to-buildPlansPageSize rows actually on screen
// (instead of for every upcoming plan, as the previous unpaginated version
// did) is the whole point of paginating server-side rather than just
// hiding rows client-side. Returns the clamped page actually shown
// alongside the total page count.
func (sc *SchedulingController) combinedPlansForServiceType(ctx context.Context, organizationID, serviceTypeID, search string, page int) (rows []views.PlanRow, currentPage int, totalPages int, errMsg string) {
	plans, err := fetchUpcomingPlans(ctx, serviceTypeID)
	if err != nil {
		return nil, 1, 1, err.Error()
	}

	filtered := filterUpcomingPlans(plans, search)
	pageOfPlans, page, totalPages := paginateUpcomingPlans(filtered, page)

	rows = make([]views.PlanRow, len(pageOfPlans))
	for i, plan := range pageOfPlans {
		rows[i] = views.PlanRow{
			ID:           plan.ID,
			Title:        plan.Attributes.Title,
			Dates:        plan.Attributes.Dates,
			SetStatus:    sc.planStatus(ctx, organizationID, serviceTypeID, plan.ID),
			RosterStatus: sc.rosterStatus(ctx, organizationID, serviceTypeID, plan.ID),
		}
	}
	return rows, page, totalPages, ""
}

// BuildPlanPage renders one plan's combined builder - the song order of
// service and the team, stacked on one page (see BuildPlan in
// build.templ). Each section is independently interactive (its own htmx
// swap target, its own add/remove/publish/discard actions, still posting
// to the existing /scheduling/sets/... and /scheduling/rosters/...
// endpoints) - this page just displays both at once instead of requiring a
// separate visit to build each.
//
// Only activeTab's own data is actually built here - see pco-api-risks.md's
// mitigation #3. The other tab's data (a real PCO-heavy build - roughly
// half this page's total load cost, per that doc) is deferred until the
// user shows real intent to look at it (BuildSongsTabLazy/BuildTeamTabLazy
// below, hover/focus/click-triggered - see BuildTab.LazyURL,
// build_tabs.templ). Both structs still need ServiceTypeID/PlanID/
// PlanTitle/PlanDates set regardless of which one was skipped: the page
// header and nav links read setData's copies unconditionally (BuildPlan,
// build.templ), and the deferred tab's own lazy-fetch URL needs its
// ServiceTypeID/PlanID too - cheaper to copy the three strings across than
// to make BuildPlan branch on which struct actually has them.
func (sc *SchedulingController) BuildPlanPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	// ?tab=team makes the Team tab directly linkable/bookmarkable (see
	// BuildPlan's activeTab doc comment) - anything other than exactly
	// "team" (missing, "songs", a typo) falls back to Songs, the same
	// default the page always had before this existed.
	activeTab := "songs"
	if c.Query("tab") == "team" {
		activeTab = "team"
	}

	var setData views.SetBuilderData
	var rosterData views.RosterBuilderData
	if activeTab == "team" {
		rosterData = sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
		setData.ServiceTypeID, setData.PlanID = serviceTypeID, planID
		setData.PlanTitle, setData.PlanDates = rosterData.PlanTitle, rosterData.PlanDates
	} else {
		setData = sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
		rosterData.ServiceTypeID, rosterData.PlanID = serviceTypeID, planID
		rosterData.PlanTitle, rosterData.PlanDates = setData.PlanTitle, setData.PlanDates
	}

	return render(c, views.BuildPlan(personName, orgName, setData, rosterData, activeTab))
}

// BuildSongsTabLazy renders the Songs tab's real content - both its main
// panel and its "Songs, at a glance" sidebar card - the first time it's
// actually requested: BuildPlanPage's initial response only ever renders a
// loading placeholder for it (see that handler's own doc comment), and
// build_tabs.templ's tabButtonAttrs points its tab button's hx-get here,
// firing on hover/focus/click. views.SetBuilderBodyLazy is
// SetBuilderBody's counterpart for this - same content, but its OOB
// fragments render hidden (the Team tab is the one actually open right
// now) instead of visible.
func (sc *SchedulingController) BuildSongsTabLazy(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	return render(c, views.SetBuilderBodyLazy(data))
}

// BuildTeamTabLazy is BuildSongsTabLazy's Team-tab counterpart - see its
// doc comment, which this mirrors in full.
func (sc *SchedulingController) BuildTeamTabLazy(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	return render(c, views.RosterBuilderBodyLazy(data))
}
