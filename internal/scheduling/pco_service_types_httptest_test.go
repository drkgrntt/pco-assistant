package scheduling

// Call-volume regression test for FetchServiceTypes's cache, using a local
// httptest.Server standing in for Planning Center (via
// pco.SetBaseURLForTesting) - no real network call, no credentials. Same
// pattern as pco_songs_httptest_test.go's TestFetchActiveSongsCacheHit-
// AvoidsSecondCall.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

// TestFetchServiceTypesCacheHitAvoidsSecondCall proves
// serviceTypeCacheTTL's whole point actually holds: a second
// FetchServiceTypes call for the same org within the TTL window costs
// zero additional PCO calls - this is what stops build.go/home.go/
// people.go's three independent page loads from each paying their own
// live GetServiceTypes round trip.
func TestFetchServiceTypesCacheHitAvoidsSecondCall(t *testing.T) {
	const organizationID = "org-service-types-cache-hit-test"
	t.Cleanup(func() { InvalidateServiceTypesCache(organizationID) })

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "ServiceType", "id": "st-1", "attributes": map[string]any{"name": "Sunday Service"}},
			},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	first, err := FetchServiceTypes(ctx, organizationID)
	if err != nil {
		t.Fatalf("first FetchServiceTypes call failed: %v", err)
	}
	if len(first) != 1 || first[0].ID != "st-1" {
		t.Fatalf("expected 1 service type (st-1), got %+v", first)
	}

	second, err := FetchServiceTypes(ctx, organizationID)
	if err != nil {
		t.Fatalf("second FetchServiceTypes call failed: %v", err)
	}
	if len(second) != 1 || second[0].ID != "st-1" {
		t.Fatalf("expected 1 service type from cache, got %+v", second)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("expected exactly 1 live PCO request across two FetchServiceTypes calls within the TTL window (the second should be a pure cache hit), got %d", got)
	}
}

// TestFetchServiceTypesCacheIsPerOrganization proves the cache is keyed by
// organization id, not global - two different orgs each get their own
// live fetch, mirroring FetchActiveSongs' own org-scoping.
func TestFetchServiceTypesCacheIsPerOrganization(t *testing.T) {
	const orgA = "org-service-types-a"
	const orgB = "org-service-types-b"
	t.Cleanup(func() {
		InvalidateServiceTypesCache(orgA)
		InvalidateServiceTypesCache(orgB)
	})

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "ServiceType", "id": "st-1", "attributes": map[string]any{"name": "Sunday Service"}},
			},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	if _, err := FetchServiceTypes(ctx, orgA); err != nil {
		t.Fatalf("FetchServiceTypes(orgA) failed: %v", err)
	}
	if _, err := FetchServiceTypes(ctx, orgB); err != nil {
		t.Fatalf("FetchServiceTypes(orgB) failed: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Errorf("expected 2 live PCO requests, one per distinct org, got %d", got)
	}
}
