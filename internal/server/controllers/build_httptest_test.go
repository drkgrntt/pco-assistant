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

// TestBuildPlanRowsConcurrencyStaysBoundedAndRowsStayCorrect covers
// buildPlanRows - the per-row status fan-out combinedPlansForServiceType
// delegates to (see its doc comment) - against a fake statusFn standing in
// for planStatus/rosterStatus's PCO round trips, rather than a real
// *scheduling.Store + PCO fake server: statusFn is the seam
// combinedPlansForServiceType itself passes in, so exercising it directly
// here proves the same concurrency/correctness properties without needing
// a live database. More rows (25) than scheduling.PCOFanOutLimit (10) means
// unbounded fan-out would run 25 statusFn calls at once; each fake call
// sleeps briefly so genuinely-concurrent calls actually overlap in
// wall-clock time rather than finishing too fast to overlap. Also asserts
// each row lands with its own plan's status (no cross-row mixup) and that
// row order still matches pageOfPlans' order despite running concurrently.
func TestBuildPlanRowsConcurrencyStaysBoundedAndRowsStayCorrect(t *testing.T) {
	const numPlans = 25
	const perCallDelay = 20 * time.Millisecond

	pageOfPlans := make([]pco.PlanData, numPlans)
	for i := range pageOfPlans {
		id := fmt.Sprintf("plan-%d", i)
		pageOfPlans[i] = pco.PlanData{
			ID: id,
			Attributes: pco.PlanAttributes{
				Title: fmt.Sprintf("Plan %d", i),
				Dates: fmt.Sprintf("Date %d", i),
			},
		}
	}
	notes := map[string]string{"plan-3": "note for plan 3"}

	var (
		callCount       int32
		inFlight        int32
		maxInFlightSeen int32
	)

	statusFn := func(p pco.PlanData) (setStatus, rosterStatus string) {
		atomic.AddInt32(&callCount, 1)

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

		time.Sleep(perCallDelay)
		// Derived from this row's own plan id, so a cross-row mixup (row i
		// ending up with row j's status) would fail the per-row assertion
		// below.
		return "set-" + p.ID, "roster-" + p.ID
	}

	rows := buildPlanRows(pageOfPlans, notes, statusFn)

	if len(rows) != numPlans {
		t.Fatalf("expected %d rows, got %d", numPlans, len(rows))
	}

	for i, row := range rows {
		wantID := pageOfPlans[i].ID
		if row.ID != wantID {
			t.Errorf("rows[%d].ID = %q, want %q (row order should match pageOfPlans' order)", i, row.ID, wantID)
		}
		if want := "set-" + wantID; row.SetStatus != want {
			t.Errorf("rows[%d].SetStatus = %q, want %q", i, row.SetStatus, want)
		}
		if want := "roster-" + wantID; row.RosterStatus != want {
			t.Errorf("rows[%d].RosterStatus = %q, want %q", i, row.RosterStatus, want)
		}
		if row.Note != notes[wantID] {
			t.Errorf("rows[%d].Note = %q, want %q", i, row.Note, notes[wantID])
		}
	}

	if got := atomic.LoadInt32(&callCount); got != int32(numPlans) {
		t.Errorf("expected statusFn called exactly once per row (%d), got %d calls", numPlans, got)
	}

	if max := atomic.LoadInt32(&maxInFlightSeen); max > scheduling.PCOFanOutLimit {
		t.Errorf("max concurrent in-flight statusFn calls = %d, want <= scheduling.PCOFanOutLimit (%d)", max, scheduling.PCOFanOutLimit)
	}
}
