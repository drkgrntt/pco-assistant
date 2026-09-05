package jobs

// Plain-Go tests for the background-sync cron's per-org pass logic - no
// live Postgres or PCO OAuth endpoint required, per
// docs/pco-background-sync-plan.md's own "Testing" section: the
// token-refresh-then-delete-on-failure branch and the org/session
// selection ("already-in-progress orgs get skipped") logic are both
// exercised here against fakes; StalePersonSyncTargets/PersonSyncMarkerStatus
// (the DB-backed coverage that doc also calls for) live in
// internal/scheduling instead, alongside person_data_cache_db_test.go's
// existing backdated-synced_at tests.

import (
	"context"
	"errors"
	"testing"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/scheduling"
)

// fakeSessionStore is a minimal in-memory backgroundSyncSessionStore -
// enough to drive runOrgPassWith's branching without a real Postgres
// connection.
type fakeSessionStore struct {
	session auth.Session
	ok      bool
	findErr error

	updatedID           string
	updatedAccessToken  string
	updatedRefreshToken string
	updateErr           error

	deletedID string
	deleteErr error
}

func (f *fakeSessionStore) SessionForBackgroundSync(ctx context.Context, organizationID string) (auth.Session, bool, error) {
	if f.findErr != nil {
		return auth.Session{}, false, f.findErr
	}
	return f.session, f.ok, nil
}

func (f *fakeSessionStore) UpdateSessionTokensByID(ctx context.Context, id, accessToken, refreshToken string, tokenExpiresAt time.Time) error {
	f.updatedID = id
	f.updatedAccessToken = accessToken
	f.updatedRefreshToken = refreshToken
	return f.updateErr
}

func (f *fakeSessionStore) DeleteSessionByID(ctx context.Context, id string) error {
	f.deletedID = id
	return f.deleteErr
}

func fakeRefreshSuccess(ctx context.Context, refreshToken string) (auth.TokenResult, error) {
	return auth.TokenResult{AccessToken: "new-access-" + refreshToken, RefreshToken: "new-refresh-" + refreshToken, ExpiresAt: time.Now().Add(2 * time.Hour)}, nil
}

func fakeRefreshFailure(ctx context.Context, refreshToken string) (auth.TokenResult, error) {
	return auth.TokenResult{}, errors.New("refresh token no longer honored")
}

func TestRunOrgPassDeletesSessionOnRefreshFailure(t *testing.T) {
	t.Cleanup(func() {
		pcoBackgroundSyncMu.Lock()
		pcoBackgroundSyncInProgress = map[string]bool{}
		pcoBackgroundSyncMu.Unlock()
	})

	store := &fakeSessionStore{session: auth.Session{ID: "session-1", RefreshToken: "ref-1"}, ok: true}
	syncCalled := false
	sync := func(ctx context.Context, s *scheduling.Store, organizationID string) { syncCalled = true }

	runOrgPassWith(context.Background(), store, fakeRefreshFailure, nil, sync, "org-1")

	if store.deletedID != "session-1" {
		t.Errorf("DeleteSessionByID called with id = %q, want %q", store.deletedID, "session-1")
	}
	if store.updatedID != "" {
		t.Error("UpdateSessionTokensByID should not have been called on a refresh failure")
	}
	if syncCalled {
		t.Error("sync should not have been called on a refresh failure")
	}
}

func TestRunOrgPassPersistsRefreshedTokenAndSyncsOnSuccess(t *testing.T) {
	t.Cleanup(func() {
		pcoBackgroundSyncMu.Lock()
		pcoBackgroundSyncInProgress = map[string]bool{}
		pcoBackgroundSyncMu.Unlock()
	})

	store := &fakeSessionStore{session: auth.Session{ID: "session-1", RefreshToken: "ref-1"}, ok: true}
	var syncedOrgID string
	sync := func(ctx context.Context, s *scheduling.Store, organizationID string) { syncedOrgID = organizationID }

	runOrgPassWith(context.Background(), store, fakeRefreshSuccess, nil, sync, "org-1")

	if store.deletedID != "" {
		t.Error("DeleteSessionByID should not have been called on a refresh success")
	}
	if store.updatedID != "session-1" {
		t.Errorf("UpdateSessionTokensByID called with id = %q, want %q", store.updatedID, "session-1")
	}
	if store.updatedAccessToken != "new-access-ref-1" {
		t.Errorf("UpdateSessionTokensByID called with access token = %q, want the refreshed one", store.updatedAccessToken)
	}
	if syncedOrgID != "org-1" {
		t.Errorf("sync called with organizationID = %q, want %q", syncedOrgID, "org-1")
	}
}

func TestRunOrgPassSkipsOrgWithNoLiveSession(t *testing.T) {
	t.Cleanup(func() {
		pcoBackgroundSyncMu.Lock()
		pcoBackgroundSyncInProgress = map[string]bool{}
		pcoBackgroundSyncMu.Unlock()
	})

	store := &fakeSessionStore{ok: false}
	refreshCalled := false
	refresh := func(ctx context.Context, refreshToken string) (auth.TokenResult, error) {
		refreshCalled = true
		return auth.TokenResult{}, nil
	}
	syncCalled := false
	sync := func(ctx context.Context, s *scheduling.Store, organizationID string) { syncCalled = true }

	runOrgPassWith(context.Background(), store, refresh, nil, sync, "org-1")

	if refreshCalled {
		t.Error("RefreshAccessToken should never be called when there's no live session")
	}
	if syncCalled {
		t.Error("sync should never be called when there's no live session")
	}
}

func TestRunOrgPassSkipsAlreadyInProgressOrg(t *testing.T) {
	t.Cleanup(func() {
		pcoBackgroundSyncMu.Lock()
		pcoBackgroundSyncInProgress = map[string]bool{}
		pcoBackgroundSyncMu.Unlock()
	})

	pcoBackgroundSyncMu.Lock()
	pcoBackgroundSyncInProgress["org-1"] = true
	pcoBackgroundSyncMu.Unlock()

	store := &fakeSessionStore{session: auth.Session{ID: "session-1", RefreshToken: "ref-1"}, ok: true}
	refreshCalled := false
	refresh := func(ctx context.Context, refreshToken string) (auth.TokenResult, error) {
		refreshCalled = true
		return auth.TokenResult{}, nil
	}

	runOrgPassWith(context.Background(), store, refresh, nil, nil, "org-1")

	if refreshCalled {
		t.Error("an already-in-progress org's pass should be skipped entirely, before ever refreshing a token")
	}
}

// --- pcoBackgroundSyncTryStart ------------------------------------------
// Mirrors controllers.songSyncTryStart/personSyncTryStart's own test shape.

func TestPCOBackgroundSyncTryStartFirstCallSucceeds(t *testing.T) {
	inProgress := map[string]bool{}

	if !pcoBackgroundSyncTryStart(inProgress, "org-1") {
		t.Error("pcoBackgroundSyncTryStart on an org with nothing running = false, want true")
	}
	if !inProgress["org-1"] {
		t.Error("pcoBackgroundSyncTryStart should have marked org-1 as running")
	}
}

func TestPCOBackgroundSyncTryStartSecondCallFails(t *testing.T) {
	inProgress := map[string]bool{}

	if !pcoBackgroundSyncTryStart(inProgress, "org-1") {
		t.Fatal("first pcoBackgroundSyncTryStart call should succeed")
	}
	if pcoBackgroundSyncTryStart(inProgress, "org-1") {
		t.Error("second pcoBackgroundSyncTryStart while org-1 is already running = true, want false")
	}
}

func TestPCOBackgroundSyncTryStartIndependentPerOrg(t *testing.T) {
	inProgress := map[string]bool{}

	if !pcoBackgroundSyncTryStart(inProgress, "org-1") {
		t.Fatal("pcoBackgroundSyncTryStart for org-1 should succeed")
	}
	if !pcoBackgroundSyncTryStart(inProgress, "org-2") {
		t.Error("pcoBackgroundSyncTryStart for org-2 while only org-1 is running = false, want true (orgs are independent)")
	}
}
