package scheduling

// DB-backed tests for StalePersonSyncTargets/PersonSyncMarkerStatus - the
// background-sync cron's refresh and discovery-dedup queries (see
// docs/plans/shipped/pco-background-sync-plan.md's "Scope"/"Per-org pass" sections and
// controllers.RunOrgBackgroundSync). Follows
// person_data_cache_db_test.go's established pattern exactly (see that
// file's doc comment for the full rationale): gated behind RUN_DB_TESTS=1,
// needs a real reachable Postgres via the existing DB_* env vars.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run 'StalePersonSyncTargets|PersonSyncMarkerStatus' -v

import (
	"context"
	"testing"
	"time"
)

func backdateSyncMarker(t *testing.T, store *Store, table, organizationID, personID, teamID string, syncedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	var err error
	if table == "person_team_plan_history_sync" {
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO person_team_plan_history_sync (organization_id, person_id, team_id, synced_at)
			VALUES ($1, $2, $3, $4)
		`, organizationID, personID, teamID, syncedAt)
	} else {
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO `+table+` (organization_id, person_id, synced_at)
			VALUES ($1, $2, $3)
		`, organizationID, personID, syncedAt)
	}
	if err != nil {
		t.Fatalf("backdateSyncMarker(%s): %v", table, err)
	}
}

func TestStalePersonSyncTargetsTTLBoundary(t *testing.T) {
	store := dbTestStore(t)
	orgID := testScopeID("org")
	freshPerson := testScopeID("person")
	stalePerson := testScopeID("person")
	staleTeamPerson := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	ttl := 4 * time.Hour

	// Fresh (within TTL) - should not be reported as stale.
	backdateSyncMarker(t, store, "person_blockout_sync", orgID, freshPerson, "", time.Now().Add(-1*time.Hour))

	// Stale (older than TTL) - should be reported.
	backdateSyncMarker(t, store, "person_plan_history_sync", orgID, stalePerson, "", time.Now().Add(-5*time.Hour))

	// Stale team-history row - should be reported with its team id.
	backdateSyncMarker(t, store, "person_team_plan_history_sync", orgID, staleTeamPerson, teamID, time.Now().Add(-6*time.Hour))

	targets, err := store.StalePersonSyncTargets(context.Background(), orgID, ttl)
	if err != nil {
		t.Fatalf("StalePersonSyncTargets: %v", err)
	}

	foundFresh := false
	foundStale := false
	foundStaleTeam := false
	for _, target := range targets {
		switch {
		case target.PersonID == freshPerson:
			foundFresh = true
		case target.PersonID == stalePerson && target.Kind == StalePersonSyncKindCrossTeamHistory:
			foundStale = true
		case target.PersonID == staleTeamPerson && target.Kind == StalePersonSyncKindTeamHistory && target.TeamID == teamID:
			foundStaleTeam = true
		}
	}

	if foundFresh {
		t.Error("StalePersonSyncTargets included a person synced within the TTL window, want excluded")
	}
	if !foundStale {
		t.Errorf("StalePersonSyncTargets missing the stale cross-team-history person, got %+v", targets)
	}
	if !foundStaleTeam {
		t.Errorf("StalePersonSyncTargets missing the stale team-history (person, team), got %+v", targets)
	}
}

func TestStalePersonSyncTargetsEmptyWhenNothingStale(t *testing.T) {
	store := dbTestStore(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	backdateSyncMarker(t, store, "person_blockout_sync", orgID, personID, "", time.Now())

	targets, err := store.StalePersonSyncTargets(context.Background(), orgID, 4*time.Hour)
	if err != nil {
		t.Fatalf("StalePersonSyncTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("StalePersonSyncTargets = %+v, want none (nothing is stale yet)", targets)
	}
}

func TestPersonSyncMarkerStatusUnknownPersonAndTeam(t *testing.T) {
	store := dbTestStore(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	personKnown, teamKnown, err := store.PersonSyncMarkerStatus(context.Background(), orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonSyncMarkerStatus: %v", err)
	}
	if personKnown {
		t.Error("PersonSyncMarkerStatus personKnown = true for a person with no sync marker at all, want false")
	}
	if teamKnown {
		t.Error("PersonSyncMarkerStatus teamKnown = true for a (person, team) with no sync marker at all, want false")
	}
}

// TestPersonSyncMarkerStatusSkipsAnyoneAlreadyCaughtByRefreshSweep is the
// discovery pass's own dedup guarantee: once ReplacePersonBlockouts (or any
// of the three sync-marker tables) has run for a person even once - stale
// or fresh, doesn't matter - PersonSyncMarkerStatus must report them known,
// so discoverRecentPersonTeamPairs (controllers/pco_background_sync.go)
// never re-enqueues someone StalePersonSyncTargets would already refresh.
func TestPersonSyncMarkerStatusSkipsAnyoneAlreadyCaughtByRefreshSweep(t *testing.T) {
	store := dbTestStore(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	teamID := testScopeID("team")
	cleanupPersonDataCache(t, store, orgID)

	// A real sync pass, however old - ReplacePersonBlockouts stamps
	// synced_at = now(), same as an organic sync would.
	if err := store.ReplacePersonBlockouts(context.Background(), orgID, personID, nil); err != nil {
		t.Fatalf("ReplacePersonBlockouts: %v", err)
	}

	personKnown, teamKnown, err := store.PersonSyncMarkerStatus(context.Background(), orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonSyncMarkerStatus: %v", err)
	}
	if !personKnown {
		t.Error("PersonSyncMarkerStatus personKnown = false for a person with an existing blockout sync marker, want true (already caught by the refresh sweep, shouldn't be rediscovered)")
	}
	// This person's blockout marker says nothing about this specific team -
	// team-level knowledge is independent.
	if teamKnown {
		t.Error("PersonSyncMarkerStatus teamKnown = true for a (person, team) with no team-history sync marker, want false")
	}

	if err := store.ReplacePersonTeamPlanHistory(context.Background(), orgID, personID, teamID, nil); err != nil {
		t.Fatalf("ReplacePersonTeamPlanHistory: %v", err)
	}
	_, teamKnown, err = store.PersonSyncMarkerStatus(context.Background(), orgID, personID, teamID)
	if err != nil {
		t.Fatalf("PersonSyncMarkerStatus (after team sync): %v", err)
	}
	if !teamKnown {
		t.Error("PersonSyncMarkerStatus teamKnown = false after ReplacePersonTeamPlanHistory ran for this (person, team), want true")
	}
}

func TestPersonSyncMarkerStatusEmptyTeamIDNeverKnown(t *testing.T) {
	store := dbTestStore(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	if err := store.ReplacePersonBlockouts(context.Background(), orgID, personID, nil); err != nil {
		t.Fatalf("ReplacePersonBlockouts: %v", err)
	}

	// A plan assignment with no resolvable team (teamID == "") should never
	// report teamKnown=true, regardless of what's synced for this person -
	// there's no team to enqueue a team-history sync for anyway.
	_, teamKnown, err := store.PersonSyncMarkerStatus(context.Background(), orgID, personID, "")
	if err != nil {
		t.Fatalf("PersonSyncMarkerStatus: %v", err)
	}
	if teamKnown {
		t.Error("PersonSyncMarkerStatus teamKnown = true for an empty teamID, want always false")
	}
}
