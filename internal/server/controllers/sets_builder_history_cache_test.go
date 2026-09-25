package controllers

// fetchRecentPlanHistory's cache - added alongside the "Browse all songs"
// search endpoint (BuildCandidatesSearch), which turned this from a
// once-per-full-render call into a once-per-debounced-keystroke one - see
// its doc comment. Same fake-httptest-server seam as
// build_httptest_test.go's fetchPlansByFilter tests.
//
// The fake server below models PCO's own previous_plan chain
// (current-plan -> prev-plan -> prev-prev-plan) rather than a plans list
// endpoint - see fetchRecentPlanHistoryFromPCO's doc comment for why the
// two plans compared are found by walking a specific plan's own
// previous_plan relationship, not by asking for "the two most recent past
// plans."

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

// recentPlanHistoryTestServer stands in for Planning Center's plan/items
// endpoints for a three-plan chain: "plan-current" (previous_plan ->
// "plan-1"), "plan-1" (previous_plan -> "plan-2", one item, song-a),
// "plan-2" (no previous_plan, one item, also song-a - so RepeatedSongs
// comes back non-empty, an easy signal that a cache hit returned real data
// rather than a zero-value miss). requestCount tracks every request
// actually reaching the fake server, which is what proves a cache hit
// skipped the round trip entirely.
func recentPlanHistoryTestServer(t *testing.T, requestCount *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/items") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"type": "Item",
						"id":   "item-1",
						"attributes": map[string]any{
							"item_type": pco.ItemTypeSong,
							"title":     "Some Song",
							"length":    180,
						},
						"relationships": map[string]any{
							"song": map[string]any{"data": map[string]any{"type": "Song", "id": "song-a"}},
						},
					},
				},
			})
			return
		}

		planID := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]

		var previousPlanRel any
		switch planID {
		case "plan-2":
			previousPlanRel = nil
		default:
			previousPlanRel = map[string]any{"type": "Plan", "id": "plan-2"}
		}
		if planID != "plan-1" && planID != "plan-2" {
			previousPlanRel = map[string]any{"type": "Plan", "id": "plan-1"}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"type":       "Plan",
				"id":         planID,
				"attributes": map[string]any{"title": planID},
				"relationships": map[string]any{
					"previous_plan": map[string]any{"data": previousPlanRel},
				},
			},
		})
	}))
}

func TestFetchRecentPlanHistoryCachesWithinTTL(t *testing.T) {
	var requestCount int32
	server := recentPlanHistoryTestServer(t, &requestCount)
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	serviceTypeID := "service-type-cache-hit"

	first := fetchRecentPlanHistory(context.Background(), serviceTypeID, "plan-current")
	if !first.RepeatedSongs["song-a"] {
		t.Fatalf("expected song-a to be flagged as repeated on the first (real) fetch, got %+v", first)
	}
	firstCount := atomic.LoadInt32(&requestCount)
	if firstCount == 0 {
		t.Fatal("expected the first call to actually hit the fake server")
	}

	second := fetchRecentPlanHistory(context.Background(), serviceTypeID, "plan-current")
	if !second.RepeatedSongs["song-a"] {
		t.Errorf("expected the cached result to match the first fetch, got %+v", second)
	}
	if got := atomic.LoadInt32(&requestCount); got != firstCount {
		t.Errorf("expected a second call within the TTL to hit the cache (no new requests), request count went from %d to %d", firstCount, got)
	}
}

// TestFetchRecentPlanHistoryDoesNotCacheFailure confirms a failed PCO call
// never gets pinned into the cache - the next call retries live instead of
// silently returning an empty result for the rest of the TTL window (same
// fail-safe convention as scheduling.FetchActiveSongs' own cache).
func TestFetchRecentPlanHistoryDoesNotCacheFailure(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	serviceTypeID := "service-type-cache-failure"

	fetchRecentPlanHistory(context.Background(), serviceTypeID, "plan-current")
	firstCount := atomic.LoadInt32(&requestCount)
	if firstCount == 0 {
		t.Fatal("expected the first call to actually hit the fake server")
	}

	fetchRecentPlanHistory(context.Background(), serviceTypeID, "plan-current")
	if got := atomic.LoadInt32(&requestCount); got <= firstCount {
		t.Errorf("expected a second call after a failed fetch to retry live (not serve a cached failure), request count stayed at %d", got)
	}
}

// TestFetchRecentPlanHistoryScopedPerPlan confirms the cache key includes
// the plan id, not just the service type id - two different plans (even in
// the same service type) never share an entry, since the two plans being
// compared depend on which plan's own previous_plan chain is being walked.
func TestFetchRecentPlanHistoryScopedPerPlan(t *testing.T) {
	var requestCount int32
	server := recentPlanHistoryTestServer(t, &requestCount)
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	fetchRecentPlanHistory(context.Background(), "service-type-a-scoped", "plan-current")
	afterFirst := atomic.LoadInt32(&requestCount)

	fetchRecentPlanHistory(context.Background(), "service-type-a-scoped", "plan-other-current")
	if got := atomic.LoadInt32(&requestCount); got <= afterFirst {
		t.Errorf("expected a different plan id to be a cache miss (new requests), request count stayed at %d", got)
	}
}

// TestFetchRecentPlanHistoryWalksPreviousPlanNotTodaysPastPlans is the
// direct regression test for the bug this behavior fixed: a plan scheduled
// well into the future must compare against the two plans immediately
// preceding *it* (via PCO's own previous_plan chain), not "the two most
// recent plans that have already happened relative to today" - those are
// only the same thing when nothing else is on the calendar in between.
func TestFetchRecentPlanHistoryWalksPreviousPlanNotTodaysPastPlans(t *testing.T) {
	var requestCount int32
	server := recentPlanHistoryTestServer(t, &requestCount)
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	history := fetchRecentPlanHistory(context.Background(), "service-type-future-plan", "plan-a-month-out")
	if !history.RepeatedSongs["song-a"] {
		t.Fatalf("expected the walk-back to plan-1/plan-2 to find song-a repeated, got %+v", history)
	}
}
