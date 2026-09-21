package scheduling

// DB-backed tests for the dismissed-warnings store methods (migration
// 00049) - see person_data_cache_db_test.go's package doc comment for why
// these need a real Postgres (RUN_DB_TESTS=1) rather than a plain
// in-memory unit test: DismissWarning's upsert (ON CONFLICT DO UPDATE) and
// the UNIQUE constraint it relies on are real SQL behavior, not Go-side
// logic.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run DismissedWarning -v

import (
	"context"
	"testing"
)

func cleanupDismissedWarnings(t *testing.T, store *Store, organizationIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, orgID := range organizationIDs {
			if _, err := store.db.ExecContext(context.Background(), `DELETE FROM dismissed_warnings WHERE organization_id = $1`, orgID); err != nil {
				t.Errorf("cleanup: deleting dismissed_warnings for org %s: %v", orgID, err)
			}
		}
	})
}

// TestDismissWarningThenUndismiss confirms the basic round trip: dismissing
// a warning makes it show up in DismissedWarningKeys, and undismissing it
// removes it again.
func TestDismissWarningThenUndismiss(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personID := testScopeID("person")
	subjectKey := testScopeID("song") + ":" + testScopeID("song")
	cleanupDismissedWarnings(t, store, orgID)

	if _, err := store.DismissWarning(ctx, orgID, personID, WarningKindAvoidPairing, subjectKey); err != nil {
		t.Fatalf("DismissWarning: %v", err)
	}

	keys, err := store.DismissedWarningKeys(ctx, orgID, WarningKindAvoidPairing)
	if err != nil {
		t.Fatalf("DismissedWarningKeys: %v", err)
	}
	if !keys[subjectKey] {
		t.Fatalf("expected %q to be dismissed, keys: %+v", subjectKey, keys)
	}

	if err := store.UndismissWarning(ctx, orgID, WarningKindAvoidPairing, subjectKey); err != nil {
		t.Fatalf("UndismissWarning: %v", err)
	}

	keys, err = store.DismissedWarningKeys(ctx, orgID, WarningKindAvoidPairing)
	if err != nil {
		t.Fatalf("DismissedWarningKeys (after undismiss): %v", err)
	}
	if keys[subjectKey] {
		t.Fatalf("expected %q to no longer be dismissed after UndismissWarning", subjectKey)
	}
}

// TestDismissWarningIdempotentRedismiss confirms re-dismissing an
// already-dismissed warning (the ON CONFLICT DO UPDATE path) succeeds
// rather than erroring on the UNIQUE constraint, and still leaves exactly
// one dismissal on record.
func TestDismissWarningIdempotentRedismiss(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personA := testScopeID("person")
	personB := testScopeID("person")
	subjectKey := testScopeID("plan") + ":" + testScopeID("song")
	cleanupDismissedWarnings(t, store, orgID)

	if _, err := store.DismissWarning(ctx, orgID, personA, WarningKindRecentRepeat, subjectKey); err != nil {
		t.Fatalf("DismissWarning (first): %v", err)
	}
	dw, err := store.DismissWarning(ctx, orgID, personB, WarningKindRecentRepeat, subjectKey)
	if err != nil {
		t.Fatalf("DismissWarning (re-dismiss): %v", err)
	}
	if dw.DismissedByPersonID != personB {
		t.Errorf("expected re-dismiss to update dismissed_by_person_id to %q, got %q", personB, dw.DismissedByPersonID)
	}

	keys, err := store.DismissedWarningKeys(ctx, orgID, WarningKindRecentRepeat)
	if err != nil {
		t.Fatalf("DismissedWarningKeys: %v", err)
	}
	if !keys[subjectKey] || len(keys) != 1 {
		t.Fatalf("expected exactly one dismissed key (%q), got %+v", subjectKey, keys)
	}
}

// TestDismissedWarningKeysOrgScoped confirms a dismissal in one organization
// is invisible when querying a different organization - the same
// org-scoping every other scheduling.Store table enforces.
func TestDismissedWarningKeysOrgScoped(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgA := testScopeID("org")
	orgB := testScopeID("org")
	personID := testScopeID("person")
	subjectKey := testScopeID("song") + ":" + testScopeID("song")
	cleanupDismissedWarnings(t, store, orgA, orgB)

	if _, err := store.DismissWarning(ctx, orgA, personID, WarningKindVocalRangeMismatch, subjectKey); err != nil {
		t.Fatalf("DismissWarning: %v", err)
	}

	keysA, err := store.DismissedWarningKeys(ctx, orgA, WarningKindVocalRangeMismatch)
	if err != nil {
		t.Fatalf("DismissedWarningKeys (org A): %v", err)
	}
	if !keysA[subjectKey] {
		t.Fatalf("expected org A to see its own dismissal")
	}

	keysB, err := store.DismissedWarningKeys(ctx, orgB, WarningKindVocalRangeMismatch)
	if err != nil {
		t.Fatalf("DismissedWarningKeys (org B): %v", err)
	}
	if keysB[subjectKey] {
		t.Fatalf("expected org B to NOT see org A's dismissal, keys: %+v", keysB)
	}

	// Undismissing from org B's scope must not affect org A's dismissal -
	// the DELETE is org-scoped too.
	if err := store.UndismissWarning(ctx, orgB, WarningKindVocalRangeMismatch, subjectKey); err != nil {
		t.Fatalf("UndismissWarning (org B): %v", err)
	}
	keysA, err = store.DismissedWarningKeys(ctx, orgA, WarningKindVocalRangeMismatch)
	if err != nil {
		t.Fatalf("DismissedWarningKeys (org A, after org B's undismiss): %v", err)
	}
	if !keysA[subjectKey] {
		t.Fatalf("expected org A's dismissal to survive org B's unrelated undismiss")
	}
}
