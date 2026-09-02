package controllers

// fetchRecentPlanHistory's cache - added alongside the "Browse all songs"
// search endpoint (BuildCandidatesSearch), which turned this from a
// once-per-full-render call into a once-per-debounced-keystroke one - see
// its doc comment. Same fake-httptest-server seam as
// build_httptest_test.go's fetchPlansByFilter tests.

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

// recentPlanHistoryTestServer stands in for Planning Center's plans/items
// endpoints: two past plans, each with one song, the same song id in both
// (so RepeatedSongs comes back non-empty - an easy signal that a cache hit
// returned real data rather than a zero-value miss). requestCount tracks
// every request actually reaching the fake server, which is what proves a
// cache hit skipped the round trip entirely.
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

		// The plans list itself.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "Plan", "id": "plan-1", "attributes": map[string]any{"title": "Last week"}},
				{"type": "Plan", "id": "plan-2", "attributes": map[string]any{"title": "Two weeks ago"}},
			},
			"meta": map[string]any{"total_count": 2},
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

	first := fetchRecentPlanHistory(context.Background(), serviceTypeID)
	if !first.RepeatedSongs["song-a"] {
		t.Fatalf("expected song-a to be flagged as repeated on the first (real) fetch, got %+v", first)
	}
	firstCount := atomic.LoadInt32(&requestCount)
	if firstCount == 0 {
		t.Fatal("expected the first call to actually hit the fake server")
	}

	second := fetchRecentPlanHistory(context.Background(), serviceTypeID)
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

	fetchRecentPlanHistory(context.Background(), serviceTypeID)
	firstCount := atomic.LoadInt32(&requestCount)
	if firstCount == 0 {
		t.Fatal("expected the first call to actually hit the fake server")
	}

	fetchRecentPlanHistory(context.Background(), serviceTypeID)
	if got := atomic.LoadInt32(&requestCount); got <= firstCount {
		t.Errorf("expected a second call after a failed fetch to retry live (not serve a cached failure), request count stayed at %d", got)
	}
}

// TestFetchRecentPlanHistoryScopedPerServiceType confirms the cache key is
// the service type id - two different service types never share an entry,
// same as scheduling.FetchActiveSongs' own per-organization scoping.
func TestFetchRecentPlanHistoryScopedPerServiceType(t *testing.T) {
	var requestCount int32
	server := recentPlanHistoryTestServer(t, &requestCount)
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	fetchRecentPlanHistory(context.Background(), "service-type-a-scoped")
	afterFirst := atomic.LoadInt32(&requestCount)

	fetchRecentPlanHistory(context.Background(), "service-type-b-scoped")
	if got := atomic.LoadInt32(&requestCount); got <= afterFirst {
		t.Errorf("expected a different service type id to be a cache miss (new requests), request count stayed at %d", got)
	}
}
