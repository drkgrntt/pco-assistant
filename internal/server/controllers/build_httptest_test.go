package controllers

// Call-volume/timing test for the plan-fetch path (fetchPlansByFilter),
// mirroring internal/scheduling's song-fetch concurrency test - same
// pco.SetBaseURLForTesting seam, same reasoning: a local httptest.Server
// stands in for Planning Center, no real network call or credentials
// involved. Unlike the song-fetch path, this one has no cache to test (see
// build.go: the plan picker deliberately re-fetches per search/page
// request), so only the concurrency bound gets its own test here.

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

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// fakePlansPage writes one page of a fake PCO plans list response - same
// shape/reasoning as internal/scheduling's fakeSongsPage, duplicated here
// rather than shared since the two packages return different PCO resource
// types and this repo's existing tests favor small per-package helpers
// over a shared test-only package for something this small.
func fakePlansPage(w http.ResponseWriter, offset, perPage, totalCount int) {
	end := offset + perPage
	if end > totalCount {
		end = totalCount
	}
	data := make([]map[string]any, 0, end-offset)
	for i := offset; i < end; i++ {
		data = append(data, map[string]any{
			"type": "Plan",
			"id":   fmt.Sprintf("plan-%d", i),
			"attributes": map[string]any{
				"title": fmt.Sprintf("Plan %d", i),
				"dates": "Some date",
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"meta": map[string]any{"total_count": totalCount},
	})
}

// TestFetchPlansByFilterConcurrencyStaysBounded fetches a plan list large
// enough (16 pages at PCO's fixed 100-per-page) that fanning every
// remaining page out unbounded would run 15 requests at once - well past
// scheduling.PCOFanOutLimit (10) - and asserts the fake server never sees
// more than PCOFanOutLimit requests in flight at the same instant. Each
// fake request sleeps briefly so genuinely-concurrent requests actually
// overlap in wall-clock time rather than finishing too fast to overlap.
func TestFetchPlansByFilterConcurrencyStaysBounded(t *testing.T) {
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
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		fakePlansPage(w, offset, buildPlansFetchPageSize, totalCount)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	plans, err := fetchPlansByFilter(context.Background(), "service-type-1", "future", "sort_date")
	if err != nil {
		t.Fatalf("fetchPlansByFilter failed: %v", err)
	}

	// Falls out for free from a correct fetch - not this test's focus, but
	// worth a sanity check that bounding concurrency didn't drop/duplicate
	// any page.
	if len(plans) != totalCount {
		t.Errorf("expected %d plans, got %d", totalCount, len(plans))
	}

	wantPages := 16
	if got := atomic.LoadInt32(&requestCount); got != int32(wantPages) {
		t.Errorf("expected %d page requests, got %d", wantPages, got)
	}

	if max := atomic.LoadInt32(&maxInFlightSeen); max > scheduling.PCOFanOutLimit {
		t.Errorf("max concurrent in-flight PCO requests = %d, want <= scheduling.PCOFanOutLimit (%d)", max, scheduling.PCOFanOutLimit)
	}
}
