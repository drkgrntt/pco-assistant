package scheduling

// DB-backed tests for the person-data cache store methods (migration
// 00033). This codebase has no DB-backed test harness anywhere else yet
// (confirmed - see pco-api-risks.md's publish-path-concurrency addendum,
// which hit the same gap and fell back to manual review instead) - these
// are gated behind RUN_DB_TESTS=1 so a plain `go test ./...`/`make test`
// never silently runs them (they need a real reachable Postgres via the
// existing DB_* env vars, and they write/delete real rows, even though
// every row is scoped to a random test-only organization_id/person_id that
// can't collide with real data).
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run PersonDataCache -v
// (DB_HOST/DB_PORT/DB_DATABASE/DB_USERNAME/DB_PASSWORD/DB_SCHEMA must
// already be set in the environment, same as any other run of this app.)

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"pco-assistant/internal/database"
)

// dbTestStore returns a *Store backed by a real Postgres connection (via
// internal/database.New(), which also runs migrations - see that
// package's doc comment), or skips the test entirely when RUN_DB_TESTS
// isn't set.
func dbTestStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("set RUN_DB_TESTS=1 to run this DB-backed test against a real Postgres (DB_* env vars)")
	}
	db := database.New()
	return NewStore(db.DB())
}

// testScopeID returns a random, test-only id, prefixed and suffixed so it
// can never collide with a real Planning Center id and is easy to spot in
// the database if a cleanup ever fails to run.
func testScopeID(label string) string {
	return fmt.Sprintf("test-%s-%d-%d", label, time.Now().UnixNano(), rand.Intn(1_000_000))
}

// cleanupPersonDataCache deletes every row this package's person-data cache
// tables could hold for organizationID, across all three sync-marker and
// three detail tables - registered via t.Cleanup so it runs even if the
// test fails partway through, rather than relying on a transaction
// spanning the whole test (this store's methods each run their own
// transaction, same as the rest of this package).
func cleanupPersonDataCache(t *testing.T, store *Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, table := range []string{
			"person_blockouts", "person_blockout_sync",
			"person_plan_history", "person_plan_history_sync",
			"person_team_plan_history", "person_team_plan_history_sync",
		} {
			if _, err := store.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE organization_id = $1`, table), organizationID); err != nil {
				t.Errorf("cleanup: deleting from %s for org %s: %v", table, organizationID, err)
			}
		}
	})
}

func TestPersonBlockoutCacheMissThenReplace(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	// A genuine miss: no sync has ever run for this person.
	blockouts, _, ok, err := store.PersonBlockoutCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonBlockoutCache (miss): unexpected error: %v", err)
	}
	if ok {
		t.Fatal("PersonBlockoutCache (miss) ok = true, want false (no sync marker exists yet)")
	}
	if len(blockouts) != 0 {
		t.Fatalf("PersonBlockoutCache (miss) returned %d blockouts, want 0", len(blockouts))
	}

	// First sync: two blockouts.
	first := []PersonBlockout{
		{PCOBlockoutID: "b1", StartsAt: time.Now(), EndsAt: time.Now().Add(24 * time.Hour), Reason: "vacation"},
		{PCOBlockoutID: "b2", StartsAt: time.Now(), EndsAt: time.Now().Add(48 * time.Hour), Reason: ""},
	}
	if err := store.ReplacePersonBlockouts(ctx, orgID, personID, first); err != nil {
		t.Fatalf("ReplacePersonBlockouts (first sync): %v", err)
	}

	blockouts, syncedAt, ok, err := store.PersonBlockoutCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonBlockoutCache (after first sync): unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("PersonBlockoutCache (after first sync) ok = false, want true")
	}
	if syncedAt.IsZero() {
		t.Error("PersonBlockoutCache (after first sync) syncedAt is zero, want a real timestamp")
	}
	if len(blockouts) != 2 {
		t.Fatalf("PersonBlockoutCache (after first sync) returned %d blockouts, want 2", len(blockouts))
	}

	// Second sync: wholesale replace with a single, different blockout -
	// proves the delete+insert leaves no orphan from the first sync.
	second := []PersonBlockout{
		{PCOBlockoutID: "b3", StartsAt: time.Now(), EndsAt: time.Now().Add(72 * time.Hour), Reason: "sick"},
	}
	if err := store.ReplacePersonBlockouts(ctx, orgID, personID, second); err != nil {
		t.Fatalf("ReplacePersonBlockouts (second sync): %v", err)
	}

	blockouts, _, ok, err = store.PersonBlockoutCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonBlockoutCache (after second sync): unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("PersonBlockoutCache (after second sync) ok = false, want true")
	}
	if len(blockouts) != 1 || blockouts[0].PCOBlockoutID != "b3" {
		t.Fatalf("PersonBlockoutCache (after second sync) = %+v, want exactly [b3] (no leftover/duplicate rows from the first sync)", blockouts)
	}

	var syncRowCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM person_blockout_sync WHERE organization_id = $1 AND person_id = $2`, orgID, personID).Scan(&syncRowCount); err != nil {
		t.Fatalf("counting person_blockout_sync rows: %v", err)
	}
	if syncRowCount != 1 {
		t.Errorf("person_blockout_sync has %d rows for this person after two syncs, want exactly 1", syncRowCount)
	}
}

func TestPersonPlanHistoryCacheMissThenReplace(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	_, _, ok, err := store.PersonPlanHistoryCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonPlanHistoryCache (miss): unexpected error: %v", err)
	}
	if ok {
		t.Fatal("PersonPlanHistoryCache (miss) ok = true, want false")
	}

	first := []PersonPlanHistoryEntry{
		{PCOPlanPersonID: "pp1", TeamID: "team-a", TeamName: "Worship", TeamPositionName: "Vocals", PlanID: "plan-1", SortDate: time.Now()},
		{PCOPlanPersonID: "pp2", TeamID: "team-b", TeamName: "Media", TeamPositionName: "Camera", PlanID: "plan-2", SortDate: time.Now().AddDate(0, 0, -7)},
	}
	if err := store.ReplacePersonPlanHistory(ctx, orgID, personID, first); err != nil {
		t.Fatalf("ReplacePersonPlanHistory (first sync): %v", err)
	}

	entries, _, ok, err := store.PersonPlanHistoryCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonPlanHistoryCache (after first sync): %v", err)
	}
	if !ok || len(entries) != 2 {
		t.Fatalf("PersonPlanHistoryCache (after first sync) ok=%v entries=%d, want ok=true entries=2", ok, len(entries))
	}

	second := []PersonPlanHistoryEntry{
		{PCOPlanPersonID: "pp3", TeamID: "team-a", TeamName: "Worship", TeamPositionName: "Vocals", PlanID: "plan-3", SortDate: time.Now()},
	}
	if err := store.ReplacePersonPlanHistory(ctx, orgID, personID, second); err != nil {
		t.Fatalf("ReplacePersonPlanHistory (second sync): %v", err)
	}

	entries, _, ok, err = store.PersonPlanHistoryCache(ctx, orgID, personID)
	if err != nil {
		t.Fatalf("PersonPlanHistoryCache (after second sync): %v", err)
	}
	if !ok || len(entries) != 1 || entries[0].PCOPlanPersonID != "pp3" {
		t.Fatalf("PersonPlanHistoryCache (after second sync) = ok=%v entries=%+v, want ok=true exactly [pp3]", ok, entries)
	}

	var syncRowCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM person_plan_history_sync WHERE organization_id = $1 AND person_id = $2`, orgID, personID).Scan(&syncRowCount); err != nil {
		t.Fatalf("counting person_plan_history_sync rows: %v", err)
	}
	if syncRowCount != 1 {
		t.Errorf("person_plan_history_sync has %d rows for this person after two syncs, want exactly 1", syncRowCount)
	}
}

func TestPersonTeamPlanHistoryCacheMissThenReplace(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	_, _, ok, err := store.PersonTeamPlanHistoryCache(ctx, orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonTeamPlanHistoryCache (miss): unexpected error: %v", err)
	}
	if ok {
		t.Fatal("PersonTeamPlanHistoryCache (miss) ok = true, want false")
	}

	first := []PersonTeamPlanHistoryEntry{
		{PCOPlanPersonID: "pp1", TeamPositionName: "Vocals", PlanID: "plan-1", SortDate: time.Now()},
		{PCOPlanPersonID: "pp2", TeamPositionName: "Guitar", PlanID: "plan-2", SortDate: time.Now().AddDate(0, 0, -14)},
	}
	if err := store.ReplacePersonTeamPlanHistory(ctx, orgID, personID, teamID, first); err != nil {
		t.Fatalf("ReplacePersonTeamPlanHistory (first sync): %v", err)
	}

	entries, _, ok, err := store.PersonTeamPlanHistoryCache(ctx, orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonTeamPlanHistoryCache (after first sync): %v", err)
	}
	if !ok || len(entries) != 2 {
		t.Fatalf("PersonTeamPlanHistoryCache (after first sync) ok=%v entries=%d, want ok=true entries=2", ok, len(entries))
	}

	// A different team for the same person must stay independent - its own
	// miss, unaffected by teamID's sync above.
	otherTeamID := testScopeID("team")
	_, _, ok, err = store.PersonTeamPlanHistoryCache(ctx, orgID, personID, otherTeamID)
	if err != nil {
		t.Fatalf("PersonTeamPlanHistoryCache (other team, miss): %v", err)
	}
	if ok {
		t.Error("PersonTeamPlanHistoryCache (other team, miss) ok = true, want false (teams are independently keyed)")
	}

	second := []PersonTeamPlanHistoryEntry{
		{PCOPlanPersonID: "pp3", TeamPositionName: "Vocals", PlanID: "plan-3", SortDate: time.Now()},
	}
	if err := store.ReplacePersonTeamPlanHistory(ctx, orgID, personID, teamID, second); err != nil {
		t.Fatalf("ReplacePersonTeamPlanHistory (second sync): %v", err)
	}

	entries, _, ok, err = store.PersonTeamPlanHistoryCache(ctx, orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonTeamPlanHistoryCache (after second sync): %v", err)
	}
	if !ok || len(entries) != 1 || entries[0].PCOPlanPersonID != "pp3" {
		t.Fatalf("PersonTeamPlanHistoryCache (after second sync) = ok=%v entries=%+v, want ok=true exactly [pp3]", ok, entries)
	}

	var syncRowCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM person_team_plan_history_sync WHERE organization_id = $1 AND person_id = $2 AND team_id = $3`, orgID, personID, teamID).Scan(&syncRowCount); err != nil {
		t.Fatalf("counting person_team_plan_history_sync rows: %v", err)
	}
	if syncRowCount != 1 {
		t.Errorf("person_team_plan_history_sync has %d rows for this (person, team) after two syncs, want exactly 1", syncRowCount)
	}

	// Cleanup for otherTeamID too, since it never got a row but belongs to
	// the same organization scope cleanupPersonDataCache already covers -
	// no separate cleanup needed since that helper deletes by organization
	// id, not by team.
	_ = otherTeamID
}

func TestAnyPersonDataSynced(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personA := testScopeID("person")
	personB := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	any, err := store.AnyPersonDataSynced(ctx, orgID, []string{personA, personB})
	if err != nil {
		t.Fatalf("AnyPersonDataSynced (before any sync): %v", err)
	}
	if any {
		t.Error("AnyPersonDataSynced (before any sync) = true, want false")
	}

	if err := store.ReplacePersonBlockouts(ctx, orgID, personA, nil); err != nil {
		t.Fatalf("ReplacePersonBlockouts (personA, zero blockouts found): %v", err)
	}

	any, err = store.AnyPersonDataSynced(ctx, orgID, []string{personA, personB})
	if err != nil {
		t.Fatalf("AnyPersonDataSynced (after personA synced): %v", err)
	}
	if !any {
		t.Error("AnyPersonDataSynced (after personA synced, even with zero blockouts found) = false, want true - a completed sync marker counts even with no rows found")
	}

	any, err = store.AnyPersonDataSynced(ctx, orgID, []string{personB})
	if err != nil {
		t.Fatalf("AnyPersonDataSynced (personB only, still unsynced): %v", err)
	}
	if any {
		t.Error("AnyPersonDataSynced (personB only, still unsynced) = true, want false")
	}

	any, err = store.AnyPersonDataSynced(ctx, orgID, nil)
	if err != nil {
		t.Fatalf("AnyPersonDataSynced (empty personIDs): %v", err)
	}
	if any {
		t.Error("AnyPersonDataSynced (empty personIDs) = true, want false")
	}
}
