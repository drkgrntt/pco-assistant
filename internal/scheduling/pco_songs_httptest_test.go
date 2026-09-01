package scheduling

// Call-volume/timing tests for the song-fetch path, using a local
// httptest.Server standing in for Planning Center (via
// pco.SetBaseURLForTesting) - no real network call, no credentials. These
// exist to give the recent performance/rate-limit fixes (60s active-song
// cache, bounded concurrent page fan-out) automated regression protection,
// which they didn't have before: the existing tests in
// pco_songs_test.go only cover pure helpers (songCacheEntryFresh,
// filterActiveSongs), never the actual fetch/cache/concurrency behavior.

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

// fakeSongsPage writes one page of a fake PCO songs list response: a
// stub song per index in [offset, offset+perPage) capped at totalCount,
// plus a Meta.TotalCount PCO would send on every page (fetchSongsFromPCO
// only actually reads it off page 0, but sending it everywhere matches
// PCO's real behavior).
func fakeSongsPage(w http.ResponseWriter, offset, perPage, totalCount int) {
	end := offset + perPage
	if end > totalCount {
		end = totalCount
	}
	data := make([]map[string]any, 0, end-offset)
	for i := offset; i < end; i++ {
		data = append(data, map[string]any{
			"type": "Song",
			"id":   fmt.Sprintf("song-%d", i),
			"attributes": map[string]any{
				"title":  fmt.Sprintf("Song %d", i),
				"hidden": false,
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"meta": map[string]any{"total_count": totalCount},
	})
}

// requestOffset reads the "offset" query param a page request carries (0
// when absent, matching page 0's request which omits it - see
// QueryParams.Offset, which only sets the param for n > 0).
func requestOffset(r *http.Request) int {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return offset
}

// --- Cache hit avoids a second live call -----------------------------------

// TestFetchActiveSongsCacheHitAvoidsSecondCall proves songCacheTTL's whole
// point actually holds: a second FetchActiveSongs call for the same org
// within the TTL window costs zero additional PCO calls, not a second
// full-library fetch. This was previously unverified by any test.
func TestFetchActiveSongsCacheHitAvoidsSecondCall(t *testing.T) {
	const organizationID = "org-cache-hit-test"
	t.Cleanup(func() { InvalidateActiveSongsCache(organizationID) })

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		fakeSongsPage(w, requestOffset(r), pcoSongPageSize, 3)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	first, err := FetchActiveSongs(ctx, organizationID)
	if err != nil {
		t.Fatalf("first FetchActiveSongs call failed: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("expected 3 active songs, got %d", len(first))
	}

	second, err := FetchActiveSongs(ctx, organizationID)
	if err != nil {
		t.Fatalf("second FetchActiveSongs call failed: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("expected 3 active songs from cache, got %d", len(second))
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected exactly 1 live PCO request across two FetchActiveSongs calls within the TTL window (the second should be a pure cache hit), got %d", got)
	}
}

// --- Concurrent page fan-out stays bounded ----------------------------------

// TestFetchActiveSongsConcurrencyStaysBounded fetches a song library large
// enough (16 pages at PCO's fixed 100-per-page) that fanning every
// remaining page out unbounded would run 15 requests at once - well past
// scheduling.PCOFanOutLimit (10) - and asserts the fake server never sees
// more than PCOFanOutLimit requests in flight at the same instant. Each
// fake request sleeps briefly so genuinely-concurrent requests actually
// overlap in wall-clock time rather than finishing too fast to overlap.
func TestFetchActiveSongsConcurrencyStaysBounded(t *testing.T) {
	const organizationID = "org-concurrency-test"
	t.Cleanup(func() { InvalidateActiveSongsCache(organizationID) })

	const totalCount = 1600 // ceil(1600/100) = 16 pages
	const perRequestDelay = 20 * time.Millisecond

	var (
		requestCount    int32
		inFlight        int32
		maxInFlightSeen int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		current := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		for {
			seen := atomic.LoadInt32(&maxInFlightSeen)
			if current <= seen {
				break
			}
			if atomic.CompareAndSwapInt32(&maxInFlightSeen, seen, current) {
				break
			}
		}

		time.Sleep(perRequestDelay)
		fakeSongsPage(w, requestOffset(r), pcoSongPageSize, totalCount)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	songs, err := FetchActiveSongs(context.Background(), organizationID)
	if err != nil {
		t.Fatalf("FetchActiveSongs failed: %v", err)
	}

	// Falls out for free from a correct fetch - not this test's focus (see
	// pco_songs_test.go for the dedicated filterActiveSongs coverage), but
	// worth a sanity check that bounding concurrency didn't drop/duplicate
	// any page.
	if len(songs) != totalCount {
		t.Errorf("expected %d songs, got %d", totalCount, len(songs))
	}

	wantPages := 16
	if got := atomic.LoadInt32(&requestCount); got != int32(wantPages) {
		t.Errorf("expected %d page requests, got %d", wantPages, got)
	}

	if max := atomic.LoadInt32(&maxInFlightSeen); max > PCOFanOutLimit {
		t.Errorf("max concurrent in-flight PCO requests = %d, want <= PCOFanOutLimit (%d)", max, PCOFanOutLimit)
	}
}
