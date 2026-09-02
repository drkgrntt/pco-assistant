package controllers

// DB-backed test for RefreshPersonData's actual write path -
// syncPersonBlockouts/syncPersonPlanHistory/syncPersonTeamPlanHistory
// (person_data_cache.go), called directly the same way the handler does,
// against a fake PCO server (pco.SetBaseURLForTesting) and a real Postgres
// store (dbTestController, defined in person_data_cache_db_test.go in this
// same package). RefreshPersonData itself is a thin *fiber.Ctx wrapper
// around a roster-assignment lookup plus these three calls - this repo has
// no harness anywhere for driving a full fiber handler end-to-end (see
// rosters_scoped_httptest_test.go's own doc comment), so this proves the
// piece that's both DB-writing and worth a regression test: that calling
// through actually overwrites an already-cached (and, per the scenario
// below, stale) row with whatever Planning Center returns right now -
// exactly what a manual "Refresh availability data" click needs to
// accomplish immediately, rather than waiting for the paced background
// sync's own turn.
//
// Gated behind RUN_DB_TESTS=1, same as person_data_cache_db_test.go.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/server/controllers/... -run RefreshPersonData -v

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// newBlockoutsFakeServer stands in for Planning Center's per-person
// blockouts endpoint, replying with one blockout carrying the given reason.
func newBlockoutsFakeServer(t *testing.T, reason string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/blockouts") {
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"type": "Blockout",
					"id":   "bo-1",
					"attributes": map[string]any{
						"starts_at": time.Now().Format(time.RFC3339),
						"ends_at":   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
						"reason":    reason,
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestSyncPersonBlockoutsOverwritesStaleCacheRow proves RefreshPersonData's
// blockout half of its write path (syncPersonBlockouts) force-overwrites an
// already-cached row with fresh data from Planning Center - the whole point
// of a manual refresh, as opposed to the cache-first read path
// (personBlockouts, rosters_warnings.go) which never calls Planning Center
// again on a fresh hit.
func TestSyncPersonBlockoutsOverwritesStaleCacheRow(t *testing.T) {
	_, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	// Seed a stale cached blockout directly (bypassing PCO), the same shape
	// a previous background sync would have left behind.
	stale := []scheduling.PersonBlockout{
		{PCOBlockoutID: "old-bo", StartsAt: time.Now().AddDate(0, -1, 0), EndsAt: time.Now().AddDate(0, -1, 1), Reason: "old reason"},
	}
	if err := store.ReplacePersonBlockouts(ctx, orgID, personID, stale); err != nil {
		t.Fatalf("ReplacePersonBlockouts (seed stale row): %v", err)
	}

	server := newBlockoutsFakeServer(t, "fresh reason")
	restore := pco.SetBaseURLForTesting(server.URL)
	err := syncPersonBlockouts(ctx, store, orgID, personID)
	restore()
	if err != nil {
		t.Fatalf("syncPersonBlockouts: %v", err)
	}

	blockouts, _, ok, err := store.PersonBlockoutCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonBlockoutCache (after refresh): %v", err)
	}
	if !ok {
		t.Fatal("PersonBlockoutCache (after refresh) ok = false, want true")
	}
	if len(blockouts) != 1 || blockouts[0].PCOBlockoutID != "bo-1" || blockouts[0].Reason != "fresh reason" {
		t.Fatalf("PersonBlockoutCache (after refresh) = %+v, want exactly one fresh blockout (bo-1, \"fresh reason\") - stale row was not overwritten", blockouts)
	}
}

// TestSyncPersonTeamPlanHistoryOverwritesStaleCacheRow is
// TestSyncPersonBlockoutsOverwritesStaleCacheRow's counterpart for the
// per-team plan-people history half of RefreshPersonData's write path
// (syncPersonTeamPlanHistory) - the cache otherAssignmentDates reads to
// compute a person's FrequencyWarning.
func TestSyncPersonTeamPlanHistoryOverwritesStaleCacheRow(t *testing.T) {
	_, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	stale := []scheduling.PersonTeamPlanHistoryEntry{
		{PCOPlanPersonID: "old-pp", TeamPositionName: "Vocals", PlanID: "plan-old", SortDate: time.Now().AddDate(0, -1, 0)},
	}
	if err := store.ReplacePersonTeamPlanHistory(ctx, orgID, personID, teamID, stale); err != nil {
		t.Fatalf("ReplacePersonTeamPlanHistory (seed stale row): %v", err)
	}

	planDate := time.Now().Truncate(24 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/plan_people"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"type": "PlanPerson",
						"id":   "fresh-pp",
						"attributes": map[string]any{
							"team_position_name": "Guitar",
						},
						"relationships": map[string]any{
							"plan": map[string]any{"data": map[string]any{"type": "Plan", "id": "plan-fresh"}},
						},
					},
				},
				"included": []map[string]any{
					{
						"type":       "Plan",
						"id":         "plan-fresh",
						"attributes": map[string]any{"sort_date": planDate.Format(time.RFC3339)},
					},
				},
			})
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	restore := pco.SetBaseURLForTesting(server.URL)
	err := syncPersonTeamPlanHistory(ctx, store, orgID, personID, teamID)
	restore()
	if err != nil {
		t.Fatalf("syncPersonTeamPlanHistory: %v", err)
	}

	entries, _, ok, err := store.PersonTeamPlanHistoryCache(ctx, orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonTeamPlanHistoryCache (after refresh): %v", err)
	}
	if !ok {
		t.Fatal("PersonTeamPlanHistoryCache (after refresh) ok = false, want true")
	}
	if len(entries) != 1 || entries[0].PCOPlanPersonID != "fresh-pp" || entries[0].TeamPositionName != "Guitar" {
		t.Fatalf("PersonTeamPlanHistoryCache (after refresh) = %+v, want exactly one fresh entry (fresh-pp, Guitar) - stale row was not overwritten", entries)
	}
}
