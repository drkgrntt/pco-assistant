package controllers

// Call-volume test for personNameMap (scheduling.go), proving it now
// delegates to personNameMapForIDs's batched `where[id]=...` lookups
// instead of the one-GetPerson-call-per-person sequential loop it used to
// run - same local-httptest.Server-stands-in-for-Planning-Center pattern as
// build_httptest_test.go/pco_songs_httptest_test.go: no real network call,
// no credentials involved.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// fakePeoplePage writes a fake PCO People#index response for a
// `where[id]=...` batch request: one PersonData per requested id, named
// "Person <id>" - except any id present in missing, which is silently left
// out of the response, simulating a since-deleted person PCO no longer has
// a record for, so the test can assert personNameMap's existing
// leave-it-out-of-the-map fallback still holds after batching.
func fakePeoplePage(w http.ResponseWriter, ids []string, missing map[string]bool) {
	data := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if missing[id] {
			continue
		}
		data = append(data, map[string]any{
			"type": "Person",
			"id":   id,
			"attributes": map[string]any{
				"name": fmt.Sprintf("Person %s", id),
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"meta": map[string]any{"total_count": len(data)},
	})
}

// TestPersonNameMapBatchesRequests proves personNameMap now makes batched
// (not per-person) PCO calls: enough distinct person ids
// (2*personIDBatchSize + 1 = 201) that the old one-GetPerson-per-person
// loop would have made 201 individual requests, and asserts the fake
// server instead sees only ceil(201/personIDBatchSize) = 3 requests, one
// per personNameMapForIDs batch. Each id also appears twice in the input
// preferences list, proving personNameMap still dedupes before delegating.
// A deliberately "missing" id (absent from PCO's response, as if the
// person had since been deleted) is asserted left out of the resulting
// map rather than causing an error.
func TestPersonNameMapBatchesRequests(t *testing.T) {
	const distinctPeople = 2*personIDBatchSize + 1 // 201 -> ceil(201/100) = 3 batches

	var preferences []scheduling.SongPersonPreference
	var wantIDs []string
	for i := range distinctPeople {
		id := fmt.Sprintf("person-%d", i)
		wantIDs = append(wantIDs, id)
		// Appears twice - personNameMap's own dedup, not just
		// personNameMapForIDs's batching, is what's under test here too.
		preferences = append(preferences,
			scheduling.SongPersonPreference{PersonID: id},
			scheduling.SongPersonPreference{PersonID: id},
		)
	}

	missing := map[string]bool{"person-5": true}

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		ids := strings.Split(r.URL.Query().Get("where[id]"), ",")
		fakePeoplePage(w, ids, missing)
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	names := personNameMap(context.Background(), preferences)

	wantBatches := int32(3)
	if got := atomic.LoadInt32(&requestCount); got != wantBatches {
		t.Errorf("expected %d batched requests for %d distinct ids, got %d", wantBatches, distinctPeople, got)
	}

	wantResolved := distinctPeople - len(missing)
	if len(names) != wantResolved {
		t.Errorf("expected %d resolved names, got %d (%v)", wantResolved, len(names), names)
	}

	for _, id := range wantIDs {
		if missing[id] {
			if got, ok := names[id]; ok {
				t.Errorf("expected missing id %q to be left out of the map, found %q", id, got)
			}
			continue
		}
		want := fmt.Sprintf("Person %s", id)
		if got := names[id]; got != want {
			t.Errorf("names[%q] = %q, want %q", id, got, want)
		}
	}
}
