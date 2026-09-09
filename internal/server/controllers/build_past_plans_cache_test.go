package controllers

// Call-volume/behavior tests for build_past_plans_cache.go - the Past
// tab's windowed fetch + cache-aside layer - using a local httptest.Server
// standing in for Planning Center (via pco.SetBaseURLForTesting), same
// seam as build_httptest_test.go/pco_songs_httptest_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// fakePastPlansPage writes one page of a fake PCO "past" plans list
// response, each plan's sort_date walking backward from now by interval
// per index (so page 0 is the most recent, matching "-sort_date"
// ordering) - unlike fakePlansPage (build_httptest_test.go), this needs a
// real sort_date on each plan for fetchPastPlansWindowed's own cutoff
// check to have something to compare against.
func fakePastPlansPage(w http.ResponseWriter, offset, perPage, totalCount int, interval time.Duration) {
	end := offset + perPage
	if end > totalCount {
		end = totalCount
	}
	data := make([]map[string]any, 0, end-offset)
	for i := offset; i < end; i++ {
		sortDate := time.Now().Add(-time.Duration(i) * interval)
		data = append(data, map[string]any{
			"type": "Plan",
			"id":   fmt.Sprintf("plan-%d", i),
			"attributes": map[string]any{
				"title":     fmt.Sprintf("Plan %d", i),
				"dates":     "Some date",
				"sort_date": sortDate.Format(time.RFC3339),
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"meta": map[string]any{"total_count": totalCount},
	})
}

func requestOffsetForTest(r *http.Request) int {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return offset
}

// TestFetchPastPlansWindowedStopsAtCutoff confirms the windowed fetch
// stops paging as soon as a page's oldest plan falls outside
// buildPastPlansWindowMonths, rather than walking a service type's entire
// history - the whole point of this function. Plans spaced 5 days apart
// put page 0's oldest plan (index 99) at 495 days back and page 1's
// oldest (index 199) at 995 days back - buildPastPlansWindowMonths (18
// months, ~548 days) falls squarely between the two with a wide safety
// margin either side (immune to AddDate's calendar-length variance), so a
// correct implementation fetches exactly 2 pages and never touches a 3rd,
// even though the fake server has far more history available than that.
func TestFetchPastPlansWindowedStopsAtCutoff(t *testing.T) {
	const totalCount = 100000 // effectively "the org's entire history"
	const interval = 5 * 24 * time.Hour
	const wantPages = 2

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		fakePastPlansPage(w, requestOffsetForTest(r), buildPlansFetchPageSize, totalCount, interval)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	plans, truncated, err := fetchPastPlansWindowed(context.Background(), "service-type-1")
	if err != nil {
		t.Fatalf("fetchPastPlansWindowed failed: %v", err)
	}

	if !truncated {
		t.Error("expected truncated=true - the fake server has far more history than the window should ever reach")
	}

	if got := atomic.LoadInt32(&requestCount); got != wantPages {
		t.Errorf("expected %d page requests before stopping at the cutoff, got %d", wantPages, got)
	}

	if len(plans) == 0 {
		t.Error("expected some plans within the window, got none")
	}
	if len(plans) >= totalCount {
		t.Errorf("expected the fetch to stop well short of the full %d-plan history, got %d", totalCount, len(plans))
	}
}

// TestFetchPastPlansWindowedNotTruncatedWhenHistoryExhausted confirms a
// service type whose entire history fits inside the window reports
// truncated=false - the "ran out of real pages" case, distinct from
// "stopped early because of the cutoff."
func TestFetchPastPlansWindowedNotTruncatedWhenHistoryExhausted(t *testing.T) {
	const totalCount = 5 // well within one page, well within the window

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fakePastPlansPage(w, requestOffsetForTest(r), buildPlansFetchPageSize, totalCount, 7*24*time.Hour)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	plans, truncated, err := fetchPastPlansWindowed(context.Background(), "service-type-1")
	if err != nil {
		t.Fatalf("fetchPastPlansWindowed failed: %v", err)
	}

	if truncated {
		t.Error("expected truncated=false - the fake server's entire history fits in one short page")
	}
	if len(plans) != totalCount {
		t.Errorf("expected all %d plans, got %d", totalCount, len(plans))
	}
}

// TestFetchPastPlansCachedHitAvoidsSecondCall proves buildPastPlansCacheTTL's
// whole point holds: a second fetchPastPlansCached call for the same
// (organization, service type) within the TTL window costs zero
// additional PCO calls.
func TestFetchPastPlansCachedHitAvoidsSecondCall(t *testing.T) {
	const organizationID = "org-past-plans-cache-hit"
	const serviceTypeID = "service-type-cache-hit"
	t.Cleanup(func() { InvalidatePastPlansCache(organizationID, serviceTypeID) })

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		fakePastPlansPage(w, requestOffsetForTest(r), buildPlansFetchPageSize, 3, 7*24*time.Hour)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	first, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID)
	if err != nil {
		t.Fatalf("first fetchPastPlansCached call failed: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(first))
	}

	second, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID)
	if err != nil {
		t.Fatalf("second fetchPastPlansCached call failed: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("expected 3 plans from cache, got %d", len(second))
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected exactly 1 live PCO request across two fetchPastPlansCached calls within the TTL window, got %d", got)
	}
}

// TestFetchPastPlansCachedScopedPerServiceType confirms the cache key
// includes serviceTypeID, not just organizationID - two service types in
// the same org must never share a cache entry (past plans are
// per-service-type data).
func TestFetchPastPlansCachedScopedPerServiceType(t *testing.T) {
	const organizationID = "org-past-plans-scoping"
	const serviceTypeA = "service-type-a"
	const serviceTypeB = "service-type-b"
	t.Cleanup(func() {
		InvalidatePastPlansCache(organizationID, serviceTypeA)
		InvalidatePastPlansCache(organizationID, serviceTypeB)
	})

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		fakePastPlansPage(w, requestOffsetForTest(r), buildPlansFetchPageSize, 1, 7*24*time.Hour)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	if _, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeA); err != nil {
		t.Fatalf("fetchPastPlansCached(serviceTypeA) failed: %v", err)
	}
	if _, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeB); err != nil {
		t.Fatalf("fetchPastPlansCached(serviceTypeB) failed: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Errorf("expected 2 live PCO requests (one per distinct service type), got %d", got)
	}
}

// TestInvalidatePastPlansCacheForcesFreshFetch confirms
// InvalidatePastPlansCache actually clears the entry rather than being a
// no-op.
func TestInvalidatePastPlansCacheForcesFreshFetch(t *testing.T) {
	const organizationID = "org-past-plans-invalidate"
	const serviceTypeID = "service-type-invalidate"
	t.Cleanup(func() { InvalidatePastPlansCache(organizationID, serviceTypeID) })

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		fakePastPlansPage(w, requestOffsetForTest(r), buildPlansFetchPageSize, 1, 7*24*time.Hour)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	if _, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID); err != nil {
		t.Fatalf("first fetchPastPlansCached call failed: %v", err)
	}
	InvalidatePastPlansCache(organizationID, serviceTypeID)
	if _, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID); err != nil {
		t.Fatalf("second fetchPastPlansCached call failed: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Errorf("expected 2 live PCO requests (cache invalidated between them), got %d", got)
	}
}

// TestFetchPastPlansForSearchFallsBackWhenTruncatedAndNoMatch confirms the
// deep-fallback path: a search term that doesn't match anything in the
// windowed/cached result, when that result was truncated, triggers one
// more (uncached) unbounded fetch via fetchPastPlans rather than silently
// reporting no matches.
func TestFetchPastPlansForSearchFallsBackWhenTruncatedAndNoMatch(t *testing.T) {
	const organizationID = "org-past-plans-deep-fallback"
	const serviceTypeID = "service-type-deep-fallback"
	t.Cleanup(func() { InvalidatePastPlansCache(organizationID, serviceTypeID) })

	// A large, ongoing weekly history: the windowed fetch will be
	// truncated (stopped at the cutoff, not run out of plans), and the
	// plan actually being searched for lives well outside the window (~9.6
	// years back), so it should be absent from the windowed result but
	// found once the deep fallback fires. Stays under
	// buildPlansFetchSanityMaxPages*buildPlansFetchPageSize (20,000) so the
	// deep, unbounded fetchPastPlans fallback this test exercises doesn't
	// itself trip fetchPlansByFilter's sanity-ceiling guard.
	const totalCount = 15000

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := requestOffsetForTest(r)
		end := offset + buildPlansFetchPageSize
		if end > totalCount {
			end = totalCount
		}
		data := make([]map[string]any, 0, end-offset)
		for i := offset; i < end; i++ {
			// One plan per week, walking back from today.
			sortDate := time.Now().Add(-time.Duration(i) * 7 * 24 * time.Hour)
			title := fmt.Sprintf("Plan %d", i)
			if i == 500 { // ~9.6 years back - well outside the window
				title = "The Old Dedication Service"
			}
			data = append(data, map[string]any{
				"type": "Plan",
				"id":   fmt.Sprintf("plan-%d", i),
				"attributes": map[string]any{
					"title":     title,
					"dates":     "Some date",
					"sort_date": sortDate.Format(time.RFC3339),
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": data,
			"meta": map[string]any{"total_count": totalCount},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	plans, err := fetchPastPlansForSearch(ctx, organizationID, serviceTypeID, "Dedication", nil)
	if err != nil {
		t.Fatalf("fetchPastPlansForSearch failed: %v", err)
	}

	found := false
	for _, p := range plans {
		if p.Attributes.Title == "The Old Dedication Service" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected the deep fallback to reach a plan well outside the default window, but it wasn't found")
	}
}

// TestFetchPastPlansForSearchSkipsDeepFetchWhenWindowSatisfiesSearch
// confirms the cheap pre-check actually saves the deep fetch when the
// windowed/cached result already contains a match - no need to pay for an
// unbounded fetch just because the window happened to be truncated too.
func TestFetchPastPlansForSearchSkipsDeepFetchWhenWindowSatisfiesSearch(t *testing.T) {
	const organizationID = "org-past-plans-no-deep-fetch-needed"
	const serviceTypeID = "service-type-no-deep-fetch-needed"
	t.Cleanup(func() { InvalidatePastPlansCache(organizationID, serviceTypeID) })

	var deepFetchCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := requestOffsetForTest(r)
		// A recent match, well inside the window, plus enough total plans
		// that the windowed fetch is truncated anyway.
		const totalCount = 100000
		end := offset + buildPlansFetchPageSize
		if end > totalCount {
			end = totalCount
		}
		data := make([]map[string]any, 0, end-offset)
		for i := offset; i < end; i++ {
			title := fmt.Sprintf("Plan %d", i)
			if i == 1 {
				title = "Easter Sunday"
			}
			sortDate := time.Now().Add(-time.Duration(i) * 7 * 24 * time.Hour)
			data = append(data, map[string]any{
				"type": "Plan",
				"id":   fmt.Sprintf("plan-%d", i),
				"attributes": map[string]any{
					"title":     title,
					"dates":     "Some date",
					"sort_date": sortDate.Format(time.RFC3339),
				},
			})
		}
		// Any request past what fetchPastPlansWindowed itself should need
		// counts as evidence of an unwanted deep fetch; track total
		// requests and sanity check below instead of trying to
		// distinguish "which function issued this" from the handler.
		atomic.AddInt32(&deepFetchCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": data,
			"meta": map[string]any{"total_count": totalCount},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	// Prime the cache first so the request count below reflects only the
	// search call.
	if _, _, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID); err != nil {
		t.Fatalf("priming fetchPastPlansCached failed: %v", err)
	}
	countAfterPriming := atomic.LoadInt32(&deepFetchCalls)

	plans, err := fetchPastPlansForSearch(ctx, organizationID, serviceTypeID, "Easter", nil)
	if err != nil {
		t.Fatalf("fetchPastPlansForSearch failed: %v", err)
	}

	if got := atomic.LoadInt32(&deepFetchCalls); got != countAfterPriming {
		t.Errorf("expected the cache-satisfied search to make zero additional PCO requests, but request count went from %d to %d", countAfterPriming, got)
	}

	found := false
	for _, p := range plans {
		if p.Attributes.Title == "Easter Sunday" {
			found = true
		}
	}
	if !found {
		t.Error("expected the cached windowed result to already contain the match")
	}
}
