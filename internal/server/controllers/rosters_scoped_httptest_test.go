package controllers

// Call-count regression test for fetchRosterSharedPCOData (rosters_scoped.go)
// - the fix for the Team Builder's worst-per-action PCO cost: before this,
// buildRosterPositionGroupData and buildRosterLightData each independently
// called GetTeamMembers/GetNeededPositions/GetTeams for the same plan on
// every add/remove/quick-add mutation (see rosters_detail.go's
// AddRosterAssignment/RemoveRosterAssignment/QuickAddPositionPreference,
// which call both functions back-to-back), so those three requests were
// sent twice each - 6 calls where 3 would do. Both functions now read
// these resources off a *rosterSharedPCOData the caller fetches once (see
// rosters_scoped.go's own doc comments) instead of calling PCO themselves
// whenever one is handed in, so fetchRosterSharedPCOData is now the single
// place these requests originate from for that shared path.
//
// The standalone GetTeams call itself is gone entirely as of the
// include=team fix (see docs/pco-api-risks.md) - fetchRosterSharedPCOData
// now costs exactly two requests (GetTeamMembers, GetNeededPositions with
// team sideloaded), not three, so this test's own counters and assertions
// were updated alongside that fix rather than left asserting the old
// three-call shape.
//
// fetchRosterSharedPCOData is now also cache-aside (rosters_shared_cache.go,
// 20s TTL) on top of the per-request sharing this file's own tests target -
// see rosters_shared_cache_httptest_test.go for that layer's own hit/miss/
// TTL-expiry/error-not-cached coverage. TestFetchRosterSharedPCODataCalled-
// TwiceDoublesRequests below calls fetchRosterSharedPCODataLive (the raw,
// never-cached fetch) rather than fetchRosterSharedPCOData specifically so
// it keeps demonstrating what it always has - two independent fetches cost
// two requests each - without the new cache making a second call within
// the TTL window a free hit instead.
//
// Same pco.SetBaseURLForTesting seam as internal/scheduling's
// pco_songs_httptest_test.go and this package's own
// build_httptest_test.go - a local httptest.Server stands in for Planning
// Center, no real network call or credentials. Unlike those two, this
// package's build functions need a working *scheduling.Store (real
// Postgres) to test end-to-end, which this repo's test suite has no
// harness for anywhere (no test DB setup exists in internal/scheduling or
// internal/server/controllers - every existing controller test is either a
// pure-function test or, like this one, an httptest-only call-count test)
// - so this test targets the shared-fetch helper directly, the one piece
// of the fix that's both PCO-call-count-bearing and DB-free. A regression
// that made a handler call fetchRosterSharedPCOData twice instead of once
// (defeating the fix without touching rosters_scoped.go at all) would
// still need a source read of rosters_detail.go's three handlers to catch
// - this test proves the helper itself is duplication-free, and the two
// tests below contrast what one call versus two actually cost in requests.

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

// rosterSharedFetchCounts tallies how many requests a fake PCO server saw
// for each of the two endpoints fetchRosterSharedPCOData calls,
// distinguished by URL path suffix (team_members/needed_positions - see
// go-pco's teamMembersPath/neededPositionsPath, neither of which collides
// with each other or with any other endpoint's suffix).
type rosterSharedFetchCounts struct {
	teamMembers     int32
	neededPositions int32
}

// newRosterSharedFetchFakeServer stands in for Planning Center's
// team_members/needed_positions endpoints, incrementing counts's matching
// counter per request and replying with a minimal-but-valid single-page
// response for whichever one was hit. The needed_positions response
// sideloads a Team via Included, mirroring the real include=team response
// fetchRosterSharedPCOData now requests instead of a separate GetTeams
// call - and fails the test outright if that request didn't actually ask
// for it, so a regression back to a plain (un-sideloaded) call would be
// caught here even though the fake server would still happily answer it.
func newRosterSharedFetchFakeServer(t *testing.T, counts *rosterSharedFetchCounts) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/team_members"):
			atomic.AddInt32(&counts.teamMembers, 1)
			writeJSON(w, map[string]any{
				"data": []map[string]any{
					{"type": "PlanPerson", "id": "pp-1", "attributes": map[string]any{"notes": "bring a capo"}},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/needed_positions"):
			atomic.AddInt32(&counts.neededPositions, 1)
			if got := r.URL.Query().Get("include"); got != "team" {
				t.Errorf("expected needed_positions request to carry include=team, got include=%q", got)
			}
			writeJSON(w, map[string]any{
				"data": []map[string]any{
					{
						"type":       "NeededPosition",
						"id":         "np-1",
						"attributes": map[string]any{"team_position_name": "Drums", "quantity": 1},
						"relationships": map[string]any{
							"team": map[string]any{"data": map[string]any{"type": "Team", "id": "t-1"}},
						},
					},
				},
				"included": []map[string]any{
					{"type": "Team", "id": "t-1", "attributes": map[string]any{"name": "Worship", "sequence": 1}},
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestFetchRosterSharedPCODataMakesOneCallEach proves a single
// fetchRosterSharedPCOData call costs exactly one GetTeamMembers and one
// GetNeededPositions request - the shared fetch every rosters_detail.go
// handler that needs both build functions now performs once per request,
// in place of each function fetching its own copy - and that TeamInfo
// comes back resolved from that response's sideloaded Included array
// rather than a separate GetTeams call.
func TestFetchRosterSharedPCODataMakesOneCallEach(t *testing.T) {
	const serviceTypeID, planID = "service-type-1", "plan-1"
	t.Cleanup(func() { invalidateRosterSharedPCOCache(serviceTypeID, planID) })

	var counts rosterSharedFetchCounts
	server := newRosterSharedFetchFakeServer(t, &counts)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	shared := fetchRosterSharedPCOData(context.Background(), serviceTypeID, planID)

	if shared.MembersErr != nil {
		t.Errorf("unexpected Members fetch error: %v", shared.MembersErr)
	}
	if shared.NeededErr != nil {
		t.Errorf("unexpected Needed fetch error: %v", shared.NeededErr)
	}
	if len(shared.Members.Data) != 1 || shared.Members.Data[0].ID != "pp-1" {
		t.Errorf("expected 1 team member (pp-1), got %+v", shared.Members.Data)
	}
	if len(shared.Needed.Data) != 1 || shared.Needed.Data[0].ID != "np-1" {
		t.Errorf("expected 1 needed position (np-1), got %+v", shared.Needed.Data)
	}
	if info, ok := shared.TeamInfo["t-1"]; !ok || info.Name != "Worship" || info.Sequence != 1 {
		t.Errorf("expected TeamInfo[t-1] to be {Worship, 1} from the sideloaded Included array, got %+v (ok=%v)", info, ok)
	}

	// The regression this test exists to catch: before rosterSharedPCOData,
	// a request that needed both buildRosterPositionGroupData and
	// buildRosterLightData sent each of these requests twice - and before
	// the include=team fix, a third GetTeams request on top of that.
	if got := atomic.LoadInt32(&counts.teamMembers); got != 1 {
		t.Errorf("expected exactly 1 GetTeamMembers request, got %d", got)
	}
	if got := atomic.LoadInt32(&counts.neededPositions); got != 1 {
		t.Errorf("expected exactly 1 GetNeededPositions request, got %d", got)
	}
}

// TestFetchRosterSharedPCODataLiveCalledTwiceDoublesRequests models the
// pre-fix shape - buildRosterPositionGroupData and buildRosterLightData
// each independently calling what's now fetchRosterSharedPCOData's calls -
// for direct contrast with TestFetchRosterSharedPCODataMakesOneCallEach's
// single-call assertion of 1 request each: two independent fetches for the
// same plan cost 2 requests per resource (4 total), exactly the
// duplication this fix eliminates by fetching once and sharing the
// result. Deliberately calls fetchRosterSharedPCODataLive (the raw,
// never-cached fetch), not fetchRosterSharedPCOData - the latter's own 20s
// cache (rosters_shared_cache.go) would turn a second same-key call within
// the TTL window into a free hit, which would defeat what this test is
// demonstrating (see rosters_shared_cache_httptest_test.go for that cache
// behavior's own coverage).
func TestFetchRosterSharedPCODataLiveCalledTwiceDoublesRequests(t *testing.T) {
	var counts rosterSharedFetchCounts
	server := newRosterSharedFetchFakeServer(t, &counts)

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := context.Background()
	_ = fetchRosterSharedPCODataLive(ctx, "service-type-1", "plan-1")
	_ = fetchRosterSharedPCODataLive(ctx, "service-type-1", "plan-1")

	if got := atomic.LoadInt32(&counts.teamMembers); got != 2 {
		t.Errorf("expected 2 GetTeamMembers requests from 2 independent fetches, got %d", got)
	}
	if got := atomic.LoadInt32(&counts.neededPositions); got != 2 {
		t.Errorf("expected 2 GetNeededPositions requests from 2 independent fetches, got %d", got)
	}
}
