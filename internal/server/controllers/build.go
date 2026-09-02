package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Build (the combined song/team workflow) ---------------------------------

// buildPlansFetchPageSize is PCO's max per_page.
// buildPlansFetchSanityMaxPages is a runaway/bug guard, not a real cap -
// same reasoning and value as scheduling.pcoSongSanityMaxPages:
// fetchPlansByFilter always fans out exactly as many pages as
// pco.Meta.TotalCount says it needs (see below), so a real service type
// only ever exceeds this if TotalCount itself is implausible - covers
// ~20,000 plans, far beyond any real church's history. buildPlansPageSize
// is this app's own display page size for the plan picker.
const (
	buildPlansFetchPageSize       = 100
	buildPlansFetchSanityMaxPages = 200
	buildPlansPageSize            = 10
)

// planTab reads the plan picker's "tab" query param - "past" or anything
// else (missing, "upcoming", a typo) defaults to "upcoming", the same
// behavior every existing link/bookmark that predates the past-plans
// archive already gets since none of them carry this param at all.
func planTab(c *fiber.Ctx) string {
	if c.Query("tab") == "past" {
		return "past"
	}
	return "upcoming"
}

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
		return render(c, views.Build(personName, orgName, nil, "", nil, 1, 1, "upcoming", err.Error()))
	}

	if len(serviceTypes.Data) == 1 {
		serviceTypeID := serviceTypes.Data[0].ID
		rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(ctx, orgID, serviceTypeID, "", 1, false)
		return render(c, views.Build(personName, orgName, serviceTypes.Data, serviceTypeID, rows, page, totalPages, "upcoming", errMsg))
	}

	return render(c, views.Build(personName, orgName, serviceTypes.Data, "", nil, 1, 1, "upcoming", ""))
}

// BuildPlans lists a freshly-chosen service type's plans (search blank,
// page 1, whichever tab is requested - see planTab) - the htmx target for
// both the service-type picker and the past/upcoming tab switcher on the
// Build page. Unlike BuildPlansSearch below, this re-renders the whole
// picker block (search box and tab bar included), since switching service
// types or tabs is a clean slate.
func (sc *SchedulingController) BuildPlans(c *fiber.Ctx) error {
	serviceTypeID := c.Query("service_type_id")
	tab := planTab(c)
	if serviceTypeID == "" {
		return render(c, views.BuildPlansList(nil, "", "", 1, 1, tab, ""))
	}

	rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(middleware.RequestContext(c), middleware.CurrentOrganizationID(c), serviceTypeID, "", 1, tab == "past")
	return render(c, views.BuildPlansList(rows, serviceTypeID, "", page, totalPages, tab, errMsg))
}

// BuildPlansSearch backs the plan picker's search box and pagination links -
// unlike BuildPlans above, it renders only the inner results fragment
// (views.BuildPlansResults), never the search box or tab bar themselves, so
// typing a search term never replaces the input the user is typing into
// (which would drop focus/cursor position mid-keystroke - see the
// person-search picker's own form-reset bug for what this class of mistake
// looks like). page defaults to 1 when absent (e.g. every request the
// search box itself fires, since it doesn't carry a page field) - a new
// search term should always restart from the first page of matches. tab
// comes from BuildPlansList's own hidden field (see that templ), carried
// forward on every Search/Prev/Next submit from within the same form so
// searching or paginating never silently drops back to the upcoming tab.
func (sc *SchedulingController) BuildPlansSearch(c *fiber.Ctx) error {
	serviceTypeID := c.Query("service_type_id")
	search := c.Query("search")
	tab := planTab(c)
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}

	if serviceTypeID == "" {
		return render(c, views.BuildPlansResults(nil, "", "", 1, 1, tab, ""))
	}

	rows, page, totalPages, errMsg := sc.combinedPlansForServiceType(middleware.RequestContext(c), middleware.CurrentOrganizationID(c), serviceTypeID, search, page, tab == "past")
	return render(c, views.BuildPlansResults(rows, serviceTypeID, search, page, totalPages, tab, errMsg))
}

// UpdatePlanNote saves one plan's purely-local at-a-glance note (see
// plan_notes/GetPlanNote) from the plan builder page's own planNoteSection
// (build.templ) - re-rendering just that one form on success/failure
// (hx-target="this" on the form itself), never the whole page. Deliberately
// its own route/handler rather than folding into BuildPlanPage: it only
// ever touches this one field, not the whole builder.
func (sc *SchedulingController) UpdatePlanNote(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	note := c.FormValue("note")

	if err := sc.store.UpsertPlanNote(c.Context(), orgID, serviceTypeID, planID, note); err != nil {
		// Best-effort feedback: re-render the form with whatever was
		// actually saved (i.e. still whatever the field held before this
		// failed save), not the attempted value, since UpsertPlanNote
		// failing means the attempted value never actually landed.
		saved, _ := sc.store.GetPlanNote(c.Context(), orgID, serviceTypeID, planID)
		return render(c, views.PlanNoteFormFragment(serviceTypeID, planID, saved))
	}

	return render(c, views.PlanNoteFormFragment(serviceTypeID, planID, note))
}

// fetchPlansByFilter pages through a service type's plans matching PCO's
// own named filter ("future" or "past" - confirmed live against the
// signed-in dev org's PCO API; go-pco's PlansParams.Filter doc comment
// names both), ordered by orderBy (id/title/dates only - status is
// deliberately computed separately, only for whichever page is actually
// being displayed, since that's the expensive part - see
// combinedPlansForServiceType), for the Build page's plan picker to
// search/paginate over locally.
//
// Same shape as scheduling.fetchSongsFromPCO (see that function's doc
// comment for the full reasoning): fetches page 0 first (synchronously) to
// learn Meta.TotalCount, then fans the remaining pages out concurrently
// via scheduling.RunConcurrently, bounded by buildPlansFetchSanityMaxPages
// as a runaway/bug guard rather than a real cap - offset-based pagination
// (not Links.Next-driven) is what makes this safe to fan out, and
// PlansParams is offset-based the same way SongsParams is. Fixes the bug
// this replaces: a single unpaginated GetPlans call would otherwise
// silently rely on PCO's default per_page, so a service type with many
// plans (recurring templates scheduled well ahead, or a long-running
// church's full past history) could be quietly truncated. Unlike the old
// version, a failed page now returns a real error (no partial slice) -
// same reasoning as fetchSongsFromPCO's own error-handling note. Shared by
// fetchUpcomingPlans/fetchPastPlans below - the only thing that differs
// between the two tabs is which filter and sort order makes sense to
// browse by.
func fetchPlansByFilter(ctx context.Context, serviceTypeID, filter, orderBy string) ([]pco.PlanData, error) {
	first, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
		Filter:  filter,
		OrderBy: orderBy,
		PerPage: buildPlansFetchPageSize,
		Offset:  0,
	})
	if err != nil {
		return nil, err
	}

	totalCount := first.Meta.TotalCount
	totalPages := (totalCount + buildPlansFetchPageSize - 1) / buildPlansFetchPageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if totalPages > buildPlansFetchSanityMaxPages {
		logger.Error("PCO plan list page count exceeds sanity ceiling - aborting fetch",
			"service_type_id", serviceTypeID, "filter", filter, "total_count", totalCount,
			"total_pages", totalPages, "sanity_max_pages", buildPlansFetchSanityMaxPages)
		return nil, fmt.Errorf("PCO reports %d plans (%d pages) for service type %s filter %q - exceeds sanity ceiling of %d pages, aborting", totalCount, totalPages, serviceTypeID, filter, buildPlansFetchSanityMaxPages)
	}

	pages := make([][]pco.PlanData, totalPages)
	pages[0] = first.Data

	if totalPages > 1 {
		pageIndexes := make([]int, totalPages-1)
		for i := range pageIndexes {
			pageIndexes[i] = i + 1
		}

		var fetchErr error
		var errMu sync.Mutex

		scheduling.RunConcurrently(pageIndexes, func(page int) {
			resp, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
				Filter:  filter,
				OrderBy: orderBy,
				PerPage: buildPlansFetchPageSize,
				Offset:  page * buildPlansFetchPageSize,
			})
			if err != nil {
				errMu.Lock()
				if fetchErr == nil {
					fetchErr = err
				}
				errMu.Unlock()
				return
			}
			// pages[page] is this goroutine's own, exclusive index into a
			// pre-sized slice - no lock needed (see RunConcurrently's doc
			// comment).
			pages[page] = resp.Data
		})

		if fetchErr != nil {
			return nil, fetchErr
		}
	}

	plans := make([]pco.PlanData, 0, totalCount)
	for _, page := range pages {
		plans = append(plans, page...)
	}

	return plans, nil
}

// fetchUpcomingPlans is fetchPlansByFilter for the "upcoming" tab - soonest
// first, so the very next service is always at the top of page 1.
func fetchUpcomingPlans(ctx context.Context, serviceTypeID string) ([]pco.PlanData, error) {
	return fetchPlansByFilter(ctx, serviceTypeID, "future", "sort_date")
}

// fetchPastPlans is fetchPlansByFilter for the "past" tab - most recent
// first ("-sort_date"), since browsing backward from "what just happened"
// is the far more common way to look for a specific past service than
// starting from the oldest plan this org has ever had.
func fetchPastPlans(ctx context.Context, serviceTypeID string) ([]pco.PlanData, error) {
	return fetchPlansByFilter(ctx, serviceTypeID, "past", "-sort_date")
}

// filterUpcomingPlans keeps plans whose title, dates, or local at-a-glance
// note (see plan_notes/GetPlanNote) contain search (case-insensitive); ""
// matches everything. Dates is included in the haystack because a plan
// without a custom title - common, PCO defaults to just showing the date -
// would otherwise be unsearchable by the only text actually visible for it
// in the list. notes maps plan id -> note text (see
// scheduling.Store.ListPlanNotesForServiceType); a nil/empty map (no notes
// saved for this service type, or the lookup failed) just means nothing
// extra matches beyond title/dates, same as before notes existed - this is
// shared by both the upcoming and past tabs (see combinedPlansForServiceType),
// since a note is exactly the kind of thing useful for finding a specific
// service either way, past or future.
func filterUpcomingPlans(plans []pco.PlanData, search string, notes map[string]string) []pco.PlanData {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return plans
	}

	filtered := make([]pco.PlanData, 0, len(plans))
	for _, plan := range plans {
		haystack := strings.ToLower(plan.Attributes.Title + " " + plan.Attributes.Dates + " " + notes[plan.ID])
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
// once (past's own filter/order when past is true - see fetchPastPlans -
// otherwise the existing upcoming behavior, unchanged), filter by search,
// paginate, and only then compute each shown row's song-lineup status
// (planStatus, in sets.go), team status (rosterStatus, in rosters.go), and
// local note (see plan_notes/GetPlanNote) - each of those is its own
// local/PCO lookup, so doing it per-row only for the up-to-
// buildPlansPageSize rows actually on screen (instead of for every plan,
// as the previous unpaginated version did) is the whole point of
// paginating server-side rather than just hiding rows client-side. Notes
// are the one exception - fetched in bulk for the whole service type
// (ListPlanNotesForServiceType) up front, before paginating, since search
// needs to match against every plan's note, not just the page actually
// shown. The per-row status lookups themselves are fanned out concurrently
// by buildPlanRows below rather than run one row at a time - each row's
// planStatus/rosterStatus can fall back to a live PCO call
// (GetItems/GetTeamMembers) when there's no local draft yet, so a full page
// of buildPlansPageSize rows could otherwise mean up to 2*buildPlansPageSize
// sequential round trips just to render the picker. Returns the clamped
// page actually shown alongside the total page count.
func (sc *SchedulingController) combinedPlansForServiceType(ctx context.Context, organizationID, serviceTypeID, search string, page int, past bool) (rows []views.PlanRow, currentPage int, totalPages int, errMsg string) {
	fetch := fetchUpcomingPlans
	if past {
		fetch = fetchPastPlans
	}
	plans, err := fetch(ctx, serviceTypeID)
	if err != nil {
		return nil, 1, 1, err.Error()
	}

	// Best-effort: a failed notes lookup just means search/display fall
	// back to no notes for this render, not a broken plan picker.
	notes, _ := sc.store.ListPlanNotesForServiceType(ctx, organizationID, serviceTypeID)

	filtered := filterUpcomingPlans(plans, search, notes)
	pageOfPlans, page, totalPages := paginateUpcomingPlans(filtered, page)

	rows = buildPlanRows(pageOfPlans, notes, func(plan pco.PlanData) (setStatus, rosterStatus string) {
		return sc.planStatus(ctx, organizationID, serviceTypeID, plan.ID), sc.rosterStatus(ctx, organizationID, serviceTypeID, plan.ID)
	})
	return rows, page, totalPages, ""
}

// buildPlanRows turns one page of plans into display rows, computing each
// row's SetStatus/RosterStatus via statusFn - split out from
// combinedPlansForServiceType so the concurrent fan-out itself is testable
// against a fake statusFn, without needing a real *scheduling.Store or a
// live PCO round trip (see build_httptest_test.go).
//
// Each row's status is independent of every other row's, so the up-to-
// buildPlansPageSize rows are fanned out via scheduling.RunConcurrently
// (bounded to scheduling.PCOFanOutLimit) instead of calling statusFn one
// row at a time - same pre-sized, index-owned-per-goroutine pattern as
// renderServiceOrder (sets_builder.go): out is allocated up front and each
// goroutine only ever writes its own index, so no lock is needed and rows
// can never land on the wrong plan despite running concurrently.
func buildPlanRows(pageOfPlans []pco.PlanData, notes map[string]string, statusFn func(plan pco.PlanData) (setStatus, rosterStatus string)) []views.PlanRow {
	rows := make([]views.PlanRow, len(pageOfPlans))

	indexes := make([]int, len(pageOfPlans))
	for i := range pageOfPlans {
		indexes[i] = i
	}

	scheduling.RunConcurrently(indexes, func(i int) {
		plan := pageOfPlans[i]
		setStatus, rosterStatus := statusFn(plan)
		rows[i] = views.PlanRow{
			ID:           plan.ID,
			Title:        plan.Attributes.Title,
			Dates:        plan.Attributes.Dates,
			Note:         notes[plan.ID],
			SetStatus:    setStatus,
			RosterStatus: rosterStatus,
		}
	})

	return rows
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
// half this page's total load cost, per that doc) is fetched separately
// (BuildSongsTabLazy/BuildTeamTabLazy below), starting as soon as the page
// itself has rendered rather than blocking this response on it - see
// lazyTabTrigger's doc comment (build_tabs.templ) for why "as soon as
// possible" beats waiting for the user to actually hover/focus/click that
// tab. Both structs still need ServiceTypeID/PlanID/
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
		setData.ReadOnly = rosterData.ReadOnly
	} else {
		setData = sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
		rosterData.ServiceTypeID, rosterData.PlanID = serviceTypeID, planID
		rosterData.PlanTitle, rosterData.PlanDates = setData.PlanTitle, setData.PlanDates
		rosterData.ReadOnly = setData.ReadOnly
	}

	// Best-effort: a failed note lookup just means the page renders with
	// no note pre-filled, same as if nothing had ever been saved.
	note, _ := sc.store.GetPlanNote(ctx, orgID, serviceTypeID, planID)

	return render(c, views.BuildPlan(personName, orgName, setData, rosterData, activeTab, note))
}

// BuildSongsTabLazy renders the Songs tab's real content - both its main
// panel and its "Songs, at a glance" sidebar card: BuildPlanPage's initial
// response only ever renders a loading placeholder for it (see that
// handler's own doc comment), and build_tabs.templ's tabButtonAttrs points
// its tab button's hx-get here, firing on page load (plus hover/focus/click
// as fallback retriggers - see lazyTabTrigger). views.SetBuilderBodyLazy is
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
