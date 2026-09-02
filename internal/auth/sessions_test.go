package auth

// DB-backed tests for ListActiveSessions/DeleteSessionsForPerson - the
// "who's signed in" list and "sign this person out" action behind the
// Account page. This package has no other DB-backed test file yet, so
// this follows internal/scheduling/person_data_cache_db_test.go's
// established pattern exactly (see that file's doc comment for the full
// rationale): gated behind RUN_DB_TESTS=1 so a plain `go test
// ./...`/`make test` never silently runs it (needs a real reachable
// Postgres via the existing DB_* env vars, and it writes/deletes real
// rows - every row here is scoped to a random test-only
// organization_id/person_id that can't collide with real data).
//
// Run with: RUN_DB_TESTS=1 go test ./internal/auth/... -run Session -v
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

// cleanupSessions deletes every session row this test could have written
// for organizationID, registered via t.Cleanup so it runs even if the
// test fails partway through.
func cleanupSessions(t *testing.T, store *Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := store.db.ExecContext(ctx, `DELETE FROM sessions WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("cleanup: deleting sessions for org %s: %v", organizationID, err)
		}
	})
}

// insertExpiredSession writes a session row directly via SQL with an
// already-past expires_at - CreateSession always sets a future expiry
// (now + SessionTTL), so this is the only way to get an expired row to
// prove ListActiveSessions actually filters them out.
func insertExpiredSession(t *testing.T, store *Store, id, personID, personName, orgID string) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(), `
		INSERT INTO sessions (id, person_id, person_name, organization_id, organization_name, expires_at, access_token, refresh_token, token_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, personID, personName, orgID, "Test Org", time.Now().Add(-time.Hour), "tok", "reftok", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insertExpiredSession: %v", err)
	}
}

func TestListActiveSessions(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	otherOrgID := testScopeID("org")
	cleanupSessions(t, store, orgID)
	cleanupSessions(t, store, otherOrgID)

	// Empty for an org with no sessions at all.
	sessions, err := store.ListActiveSessions(ctx, orgID)
	if err != nil {
		t.Fatalf("ListActiveSessions (empty): unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("ListActiveSessions (empty) = %d sessions, want 0", len(sessions))
	}

	personA := testScopeID("person")
	personB := testScopeID("person")

	if _, err := store.CreateSession(ctx, personA, "Alice", orgID, "Test Org", "tok-a", "ref-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (personA): %v", err)
	}
	// Ensure a distinct created_at ordering (created_at has second-ish
	// resolution in Postgres by default for now(), so sleep briefly
	// rather than relying on sub-millisecond ordering).
	time.Sleep(1100 * time.Millisecond)
	if _, err := store.CreateSession(ctx, personB, "Bob", orgID, "Test Org", "tok-b", "ref-b", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (personB): %v", err)
	}

	// A session in a different org must not leak in.
	if _, err := store.CreateSession(ctx, testScopeID("person"), "Other Org Person", otherOrgID, "Other Org", "tok-o", "ref-o", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (other org): %v", err)
	}

	// An expired session for this org must not show up either.
	insertExpiredSession(t, store, testScopeID("sess"), testScopeID("person"), "Expired Person", orgID)

	sessions, err = store.ListActiveSessions(ctx, orgID)
	if err != nil {
		t.Fatalf("ListActiveSessions: unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListActiveSessions = %d sessions, want 2 (got %+v)", len(sessions), sessions)
	}
	// Most recent first: Bob was created after Alice.
	if sessions[0].PersonName != "Bob" || sessions[1].PersonName != "Alice" {
		t.Errorf("ListActiveSessions order = [%s, %s], want [Bob, Alice] (most recent first)", sessions[0].PersonName, sessions[1].PersonName)
	}
	for _, sess := range sessions {
		if sess.CreatedAt.IsZero() {
			t.Errorf("session %s CreatedAt is zero, want a real timestamp", sess.PersonName)
		}
		if sess.ExpiresAt.IsZero() {
			t.Errorf("session %s ExpiresAt is zero, want a real timestamp", sess.PersonName)
		}
	}
}

func TestDeleteSessionsForPerson(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	cleanupSessions(t, store, orgID)

	personA := testScopeID("person")
	personB := testScopeID("person")

	// personA signs in on two devices.
	if _, err := store.CreateSession(ctx, personA, "Alice", orgID, "Test Org", "tok-a1", "ref-a1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (personA, device 1): %v", err)
	}
	if _, err := store.CreateSession(ctx, personA, "Alice", orgID, "Test Org", "tok-a2", "ref-a2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (personA, device 2): %v", err)
	}
	if _, err := store.CreateSession(ctx, personB, "Bob", orgID, "Test Org", "tok-b", "ref-b", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession (personB): %v", err)
	}

	// A person with no sessions: 0 deleted, no error.
	count, err := store.DeleteSessionsForPerson(ctx, orgID, testScopeID("person"))
	if err != nil {
		t.Fatalf("DeleteSessionsForPerson (no sessions): unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("DeleteSessionsForPerson (no sessions) = %d, want 0", count)
	}

	count, err = store.DeleteSessionsForPerson(ctx, orgID, personA)
	if err != nil {
		t.Fatalf("DeleteSessionsForPerson (personA): %v", err)
	}
	if count != 2 {
		t.Errorf("DeleteSessionsForPerson (personA) = %d, want 2 (both devices)", count)
	}

	sessions, err := store.ListActiveSessions(ctx, orgID)
	if err != nil {
		t.Fatalf("ListActiveSessions (after delete): %v", err)
	}
	if len(sessions) != 1 || sessions[0].PersonName != "Bob" {
		t.Fatalf("ListActiveSessions (after delete) = %+v, want exactly [Bob] (personB untouched)", sessions)
	}
}
