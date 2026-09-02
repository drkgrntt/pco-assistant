package controllers

// DB-backed tests for the person-data cache's cache-first read path
// (otherAssignmentDates/crossTeamAssignmentsThisWeek/personBlockouts in
// rosters_warnings.go) - proving a miss never blocks on PCO and queues a
// sync, and a stale hit serves the stale row immediately while also
// queuing a refresh. See internal/scheduling/person_data_cache_db_test.go
// for the store-layer wholesale-replace-on-sync coverage this builds on.
//
// Gated behind RUN_DB_TESTS=1, same as that file - see its doc comment for
// why and how to run these.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/server/controllers/... -run PersonDataCacheReadPath -v

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"pco-assistant/internal/database"
	"pco-assistant/internal/scheduling"
)

// dbTestController returns a *SchedulingController (plus the *scheduling.Store
// backing it) using a real Postgres connection, or skips the test entirely
// when RUN_DB_TESTS isn't set - the controllers-package counterpart to
// internal/scheduling/person_data_cache_db_test.go's dbTestStore.
func dbTestController(t *testing.T) (*SchedulingController, *scheduling.Store) {
	t.Helper()
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("set RUN_DB_TESTS=1 to run this DB-backed test against a real Postgres (DB_* env vars)")
	}
	db := database.New()
	store := scheduling.NewStore(db.DB())
	return NewSchedulingController(store), store
}

// testScopeID is this package's own copy of
// internal/scheduling/person_data_cache_db_test.go's helper of the same
// name (unexported there, so not reachable from this package) - a random,
// test-only id that can't collide with a real Planning Center id.
func testScopeID(label string) string {
	return fmt.Sprintf("test-%s-%d-%d", label, time.Now().UnixNano(), rand.Intn(1_000_000))
}

// cleanupPersonDataCache is this package's own copy of
// internal/scheduling/person_data_cache_db_test.go's helper of the same
// name - deletes every row this test could have written for organizationID
// across all three sync-marker/detail table pairs, via t.Cleanup so it
// still runs if the test fails partway through.
func cleanupPersonDataCache(t *testing.T, store *scheduling.Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, table := range []string{
			"person_blockouts", "person_blockout_sync",
			"person_plan_history", "person_plan_history_sync",
			"person_team_plan_history", "person_team_plan_history_sync",
		} {
			if _, err := store.DB().ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE organization_id = $1`, table), organizationID); err != nil {
				t.Errorf("cleanup: deleting from %s for org %s: %v", table, organizationID, err)
			}
		}
	})
}

// clearPersonSyncPending removes req from the package-level pending queue -
// tests call this before checking whether a given call enqueued req, so a
// leftover entry from an earlier assertion in the same test can't produce
// a false positive.
func clearPersonSyncPending(req personSyncRequest) {
	personSyncMu.Lock()
	delete(personSyncPending, req)
	personSyncMu.Unlock()
}

func personSyncPendingContains(req personSyncRequest) bool {
	personSyncMu.Lock()
	defer personSyncMu.Unlock()
	return personSyncPending[req]
}

func TestPersonBlockoutsReadPathMissEnqueuesAndDoesNotBlock(t *testing.T) {
	sc, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	req := personSyncRequest{organizationID: orgID, personID: personID, kind: personSyncKindBlockouts}
	clearPersonSyncPending(req)
	t.Cleanup(func() { clearPersonSyncPending(req) })

	start := time.Now()
	got := sc.personBlockouts(ctx, orgID, personID)
	elapsed := time.Since(start)

	if got != nil {
		t.Errorf("personBlockouts (miss) = %v, want nil", got)
	}
	// Generous bound - a local DB round trip should be single-digit
	// milliseconds; this just proves nothing resembling a live PCO call
	// (network round trip to a third party, typically 100-400ms per
	// docs/pco-api-risks.md) happened inline.
	if elapsed > 50*time.Millisecond {
		t.Errorf("personBlockouts (miss) took %s, want well under 50ms (should never block on a live PCO call)", elapsed)
	}
	if !personSyncPendingContains(req) {
		t.Error("personBlockouts (miss) did not enqueue a background sync request")
	}
}

func TestPersonBlockoutsReadPathStaleHitServesStaleAndEnqueuesRefresh(t *testing.T) {
	sc, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	stale := []scheduling.PersonBlockout{
		{PCOBlockoutID: "b1", StartsAt: time.Now(), EndsAt: time.Now().Add(24 * time.Hour), Reason: "vacation"},
	}
	if err := store.ReplacePersonBlockouts(ctx, orgID, personID, stale); err != nil {
		t.Fatalf("ReplacePersonBlockouts: %v", err)
	}
	// Backdate synced_at past personCacheTTL - ReplacePersonBlockouts always
	// stamps now(), so this simulates a row that's gone stale since its
	// last sync.
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE person_blockout_sync SET synced_at = $1 WHERE organization_id = $2 AND person_id = $3
	`, time.Now().Add(-personCacheTTL-time.Hour), orgID, personID); err != nil {
		t.Fatalf("backdating synced_at: %v", err)
	}

	req := personSyncRequest{organizationID: orgID, personID: personID, kind: personSyncKindBlockouts}
	clearPersonSyncPending(req)
	t.Cleanup(func() { clearPersonSyncPending(req) })

	got := sc.personBlockouts(ctx, orgID, personID)

	if len(got) != 1 || got[0].ID != "b1" {
		t.Fatalf("personBlockouts (stale hit) = %+v, want the stale cached row served immediately", got)
	}
	if !personSyncPendingContains(req) {
		t.Error("personBlockouts (stale hit) did not enqueue a refresh")
	}
}

func TestCrossTeamAssignmentsThisWeekReadPathMissEnqueuesAndDoesNotBlock(t *testing.T) {
	sc, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	req := personSyncRequest{organizationID: orgID, personID: personID, kind: personSyncKindCrossTeamHistory}
	clearPersonSyncPending(req)
	t.Cleanup(func() { clearPersonSyncPending(req) })

	got := sc.crossTeamAssignmentsThisWeek(ctx, orgID, personID, time.Now(), "plan-current")

	if got != nil {
		t.Errorf("crossTeamAssignmentsThisWeek (miss) = %v, want nil", got)
	}
	if !personSyncPendingContains(req) {
		t.Error("crossTeamAssignmentsThisWeek (miss) did not enqueue a background sync request")
	}
}

func TestOtherAssignmentDatesReadPathStaleHitServesStaleAndEnqueuesRefresh(t *testing.T) {
	sc, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	// .Truncate(time.Microsecond): Postgres's timestamptz column only stores
	// microsecond precision, so a raw time.Now() (nanosecond precision)
	// would round-trip through the real store with a few hundred
	// nanoseconds trimmed off - a real, if inconsequential, difference that
	// makes the exact .Equal() below correctly report a mismatch that has
	// nothing to do with this test's actual subject (stale-hit serving).
	// Truncating up front matches what the store will actually persist.
	oldDate := time.Now().AddDate(0, 0, -30).Truncate(time.Microsecond)
	entries := []scheduling.PersonTeamPlanHistoryEntry{
		{PCOPlanPersonID: "pp1", TeamPositionName: "Vocals", PlanID: "plan-old", SortDate: oldDate},
	}
	if err := store.ReplacePersonTeamPlanHistory(ctx, orgID, personID, teamID, entries); err != nil {
		t.Fatalf("ReplacePersonTeamPlanHistory: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE person_team_plan_history_sync SET synced_at = $1 WHERE organization_id = $2 AND person_id = $3 AND team_id = $4
	`, time.Now().Add(-personCacheTTL-time.Hour), orgID, personID, teamID); err != nil {
		t.Fatalf("backdating synced_at: %v", err)
	}

	req := personSyncRequest{organizationID: orgID, personID: personID, teamID: teamID, kind: personSyncKindTeamHistory}
	clearPersonSyncPending(req)
	t.Cleanup(func() { clearPersonSyncPending(req) })

	got := sc.otherAssignmentDates(ctx, orgID, personID, teamID, "Vocals", "plan-current")

	if len(got) != 1 || !got[0].Equal(oldDate) {
		t.Fatalf("otherAssignmentDates (stale hit) = %v, want [%v] served immediately from the stale cache", got, oldDate)
	}
	if !personSyncPendingContains(req) {
		t.Error("otherAssignmentDates (stale hit) did not enqueue a refresh")
	}
}
