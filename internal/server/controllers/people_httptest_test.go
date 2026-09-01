package controllers

// Call-volume/correctness test for fetchTeamPositions (people.go), proving
// its per-team GetTeamPositions calls now fan out concurrently (bounded by
// scheduling.PCOFanOutLimit) instead of running one team at a time, while
// still preserving team order and the "return whatever succeeded, even
// after a mid-walk error on one team" behavior - same local
// httptest.Server-stands-in-for-Planning-Center pattern as
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
	"time"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// TestFetchTeamPositionsConcurrencyBoundedOrderedAndErrorTolerant fetches
// more teams (15) than scheduling.PCOFanOutLimit (10), so unbounded fan-out
// would send 15 GetTeamPositions requests at once, and asserts the fake
// server never sees more than PCOFanOutLimit in flight at the same instant.
// One team (team-05) deliberately 500s to prove a mid-walk error on one
// team doesn't drop the rest and is still surfaced as a non-nil error, and
// the resulting groups are asserted to come back in the same order
// GetTeams returned them (minus the failed team), proving the concurrent
// fan-out doesn't scramble team order.
func TestFetchTeamPositionsConcurrencyBoundedOrderedAndErrorTolerant(t *testing.T) {
	const serviceTypeID = "st1"
	const numTeams = 15
	const failingTeam = "team-05"
	const perCallDelay = 20 * time.Millisecond

	teamIDs := make([]string, numTeams)
	for i := range teamIDs {
		teamIDs[i] = fmt.Sprintf("team-%02d", i)
	}

	var (
		positionRequestCount int32
		inFlight             int32
		maxInFlightSeen      int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/team_positions"):
			atomic.AddInt32(&positionRequestCount, 1)

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

			// team_positions path is .../teams/<teamID>/team_positions.
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			teamID := parts[len(parts)-2]

			if teamID == failingTeam {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"title":"boom"}]}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"type": "TeamPosition",
						"id":   teamID + "-pos-0",
						"attributes": map[string]any{
							"name": teamID + " Position 0",
						},
					},
				},
				"meta": map[string]any{"total_count": 1},
			})

		case strings.HasSuffix(r.URL.Path, "/teams"):
			data := make([]map[string]any, len(teamIDs))
			for i, id := range teamIDs {
				data[i] = map[string]any{
					"type": "Team",
					"id":   id,
					"attributes": map[string]any{
						"name": id,
					},
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": data,
				"meta": map[string]any{"total_count": len(data)},
			})

		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	groups, err := fetchTeamPositions(context.Background(), serviceTypeID)

	if err == nil {
		t.Error("expected a non-nil error surfaced from the failing team, got nil")
	}

	wantGroups := numTeams - 1 // every team except the one that 500s
	if len(groups) != wantGroups {
		t.Fatalf("expected %d groups (all teams but the failing one), got %d: %+v", wantGroups, len(groups), groups)
	}

	// Order must still match teamIDs' own order (minus the skipped failing
	// team) despite the fan-out running concurrently.
	wantOrder := make([]string, 0, wantGroups)
	for _, id := range teamIDs {
		if id != failingTeam {
			wantOrder = append(wantOrder, id)
		}
	}
	for i, g := range groups {
		if g.TeamID != wantOrder[i] {
			t.Errorf("groups[%d].TeamID = %q, want %q (team order should match GetTeams' order)", i, g.TeamID, wantOrder[i])
		}
		if len(g.Positions) != 1 || g.Positions[0].TeamPositionID != g.TeamID+"-pos-0" {
			t.Errorf("groups[%d] (%s) has unexpected positions: %+v", i, g.TeamID, g.Positions)
		}
	}

	if got := atomic.LoadInt32(&positionRequestCount); got != numTeams {
		t.Errorf("expected %d GetTeamPositions requests (one per team, including the failing one), got %d", numTeams, got)
	}

	if max := atomic.LoadInt32(&maxInFlightSeen); max > scheduling.PCOFanOutLimit {
		t.Errorf("max concurrent in-flight GetTeamPositions requests = %d, want <= scheduling.PCOFanOutLimit (%d)", max, scheduling.PCOFanOutLimit)
	}
}
