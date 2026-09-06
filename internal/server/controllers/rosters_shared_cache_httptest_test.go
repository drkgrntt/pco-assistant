package controllers

// Cache-aside behavior tests for fetchRosterSharedPCOData's 20s TTL cache
// (rosters_shared_cache.go) - hit, per-plan miss, TTL expiry, and
// error-not-cached, the four cases that file's own doc comment promises.
// Same pco.SetBaseURLForTesting/httptest.Server seam as
// rosters_scoped_httptest_test.go, reusing that file's
// rosterSharedFetchCounts/newRosterSharedFetchFakeServer helpers (same
// package) rather than duplicating them.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// TestFetchRosterSharedPCODataCacheHitAvoidsSecondFetch proves
// rosterSharedCacheTTL's whole point actually holds: a second
// fetchRosterSharedPCOData call for the same (serviceTypeID, planID)
// within the TTL window costs zero additional PCO calls.
func TestFetchRosterSharedPCODataCacheHitAvoidsSecondFetch(t *testing.T) {
	const serviceTypeID, planID = "cache-hit-service-type", "cache-hit-plan"
	t.Cleanup(func() { invalidateRosterSharedPCOCache(serviceTypeID, planID) })

	var counts rosterSharedFetchCounts
	server := newRosterSharedFetchFakeServer(t, &counts)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()
	first := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	if first.MembersErr != nil || first.NeededErr != nil {
		t.Fatalf("unexpected fetch error(s): MembersErr=%v NeededErr=%v", first.MembersErr, first.NeededErr)
	}

	second := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	if second.MembersErr != nil || second.NeededErr != nil {
		t.Fatalf("unexpected fetch error(s) on cached call: MembersErr=%v NeededErr=%v", second.MembersErr, second.NeededErr)
	}
	if len(second.Members.Data) != 1 || len(second.Needed.Data) != 1 {
		t.Errorf("expected the cached result back unchanged, got %+v", second)
	}

	if got := atomic.LoadInt32(&counts.teamMembers); got != 1 {
		t.Errorf("expected exactly 1 GetTeamMembers request across two calls within the TTL window (the second should be a pure cache hit), got %d", got)
	}
	if got := atomic.LoadInt32(&counts.neededPositions); got != 1 {
		t.Errorf("expected exactly 1 GetNeededPositions request across two calls within the TTL window, got %d", got)
	}
}

// TestFetchRosterSharedPCODataCacheIsPerPlan proves the cache is keyed by
// (serviceTypeID, planID), not global - two different plans each get their
// own live fetch, mirroring scheduling.FetchServiceTypes' per-organization
// scoping test.
func TestFetchRosterSharedPCODataCacheIsPerPlan(t *testing.T) {
	const serviceTypeID = "cache-per-plan-service-type"
	const planA, planB = "cache-per-plan-a", "cache-per-plan-b"
	t.Cleanup(func() {
		invalidateRosterSharedPCOCache(serviceTypeID, planA)
		invalidateRosterSharedPCOCache(serviceTypeID, planB)
	})

	var counts rosterSharedFetchCounts
	server := newRosterSharedFetchFakeServer(t, &counts)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()
	if shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planA); shared.MembersErr != nil || shared.NeededErr != nil {
		t.Fatalf("fetch for planA failed: MembersErr=%v NeededErr=%v", shared.MembersErr, shared.NeededErr)
	}
	if shared := fetchRosterSharedPCOData(ctx, serviceTypeID, planB); shared.MembersErr != nil || shared.NeededErr != nil {
		t.Fatalf("fetch for planB failed: MembersErr=%v NeededErr=%v", shared.MembersErr, shared.NeededErr)
	}

	if got := atomic.LoadInt32(&counts.teamMembers); got != 2 {
		t.Errorf("expected 2 GetTeamMembers requests, one per distinct plan, got %d", got)
	}
	if got := atomic.LoadInt32(&counts.neededPositions); got != 2 {
		t.Errorf("expected 2 GetNeededPositions requests, one per distinct plan, got %d", got)
	}
}

// TestFetchRosterSharedPCODataCacheExpiresAfterTTL proves an entry past
// rosterSharedCacheTTL is treated as a miss, not served stale - seeding
// the package-level cache map directly (white-box, same package) with an
// already-expired cachedAt rather than actually sleeping
// rosterSharedCacheTTL in a test.
func TestFetchRosterSharedPCODataCacheExpiresAfterTTL(t *testing.T) {
	const serviceTypeID, planID = "cache-expiry-service-type", "cache-expiry-plan"
	t.Cleanup(func() { invalidateRosterSharedPCOCache(serviceTypeID, planID) })

	key := rosterSharedCacheKey{serviceTypeID: serviceTypeID, planID: planID}
	staleData := &rosterSharedPCOData{
		Members: pco.PlanPersonListResponse{Data: []pco.PlanPersonData{{ID: "stale-pp"}}},
	}
	rosterSharedCacheMu.Lock()
	rosterSharedCache[key] = cachedRosterShared{data: staleData, cachedAt: time.Now().Add(-rosterSharedCacheTTL - time.Second)}
	rosterSharedCacheMu.Unlock()

	var counts rosterSharedFetchCounts
	server := newRosterSharedFetchFakeServer(t, &counts)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	shared := fetchRosterSharedPCOData(context.Background(), serviceTypeID, planID)
	if shared.MembersErr != nil || shared.NeededErr != nil {
		t.Fatalf("unexpected fetch error(s): MembersErr=%v NeededErr=%v", shared.MembersErr, shared.NeededErr)
	}
	if len(shared.Members.Data) != 1 || shared.Members.Data[0].ID != "pp-1" {
		t.Errorf("expected the fresh live result (pp-1), not the seeded stale entry, got %+v", shared.Members.Data)
	}

	if got := atomic.LoadInt32(&counts.teamMembers); got != 1 {
		t.Errorf("expected a live GetTeamMembers request once the cached entry was past its TTL, got %d requests", got)
	}
	if got := atomic.LoadInt32(&counts.neededPositions); got != 1 {
		t.Errorf("expected a live GetNeededPositions request once the cached entry was past its TTL, got %d requests", got)
	}
}

// TestFetchRosterSharedPCODataErrorNotCached proves a fetch that fails
// (either sub-call) is never cached - the fail-safe convention
// fetchRosterSharedPCOData's own doc comment promises, matching
// scheduling.FetchActiveSongs/FetchServiceTypes. A second call after a
// failed first one must retry live, not serve (or permanently withhold
// behind) a cached error/partial result.
func TestFetchRosterSharedPCODataErrorNotCached(t *testing.T) {
	const serviceTypeID, planID = "cache-error-service-type", "cache-error-plan"
	t.Cleanup(func() { invalidateRosterSharedPCOCache(serviceTypeID, planID) })

	var teamMembersRequests, neededPositionsRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/team_members"):
			atomic.AddInt32(&teamMembersRequests, 1)
			// Always fails - this is the sub-call whose error should keep
			// the whole rosterSharedPCOData out of the cache every time.
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/needed_positions"):
			atomic.AddInt32(&neededPositionsRequests, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"type": "NeededPosition", "id": "np-1", "attributes": map[string]any{"team_position_name": "Drums", "quantity": 1}},
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()

	first := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	if first.MembersErr == nil {
		t.Fatal("expected MembersErr from the always-failing team_members endpoint, got nil")
	}

	second := fetchRosterSharedPCOData(ctx, serviceTypeID, planID)
	if second.MembersErr == nil {
		t.Fatal("expected MembersErr again on the second call - a failed fetch must never be cached")
	}

	// Both GetNeededPositions calls succeeded, but a partial success (one
	// call ok, the other failed) still must not be cached as a whole - see
	// rosterSharedPCOData's own doc comment on why errors are kept
	// separate but caching stays all-or-nothing.
	if got := atomic.LoadInt32(&teamMembersRequests); got != 2 {
		t.Errorf("expected 2 GetTeamMembers requests (no caching of the failed fetch), got %d", got)
	}
	if got := atomic.LoadInt32(&neededPositionsRequests); got != 2 {
		t.Errorf("expected 2 GetNeededPositions requests (the whole result stays uncached when MembersErr is set, even though this sub-call succeeded both times), got %d", got)
	}
}
