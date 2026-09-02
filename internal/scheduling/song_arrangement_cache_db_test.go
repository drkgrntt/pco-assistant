package scheduling

// DB-backed test for ClearSongArrangementCache (the Account page's "nuclear"
// song info cache clear). Gated behind RUN_DB_TESTS=1 - see
// person_data_cache_db_test.go's doc comment for why, and for the
// dbTestStore/testScopeID helpers this file reuses.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run ClearSongArrangementCache -v

import (
	"context"
	"testing"
)

// cleanupSongArrangementCache deletes every row song_arrangement_cache
// could hold for organizationID - registered via t.Cleanup, same pattern
// as cleanupPersonDataCache.
func cleanupSongArrangementCache(t *testing.T, store *Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := store.db.ExecContext(context.Background(), `DELETE FROM song_arrangement_cache WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("cleanup: deleting from song_arrangement_cache for org %s: %v", organizationID, err)
		}
	})
}

func TestClearSongArrangementCache(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	songB := testScopeID("song")
	cleanupSongArrangementCache(t, store, orgID)

	if err := store.UpsertSongArrangementInfo(ctx, orgID, songA, SongArrangementInfo{Key: "G", BPM: 120}); err != nil {
		t.Fatalf("UpsertSongArrangementInfo (songA): %v", err)
	}
	if err := store.UpsertSongArrangementInfo(ctx, orgID, songB, SongArrangementInfo{Key: "D", BPM: 96}); err != nil {
		t.Fatalf("UpsertSongArrangementInfo (songB): %v", err)
	}

	cache, err := store.SongArrangementInfo(ctx, orgID)
	if err != nil {
		t.Fatalf("SongArrangementInfo (before clear): %v", err)
	}
	if len(cache) != 2 {
		t.Fatalf("SongArrangementInfo (before clear) = %d entries, want 2", len(cache))
	}

	if err := store.ClearSongArrangementCache(ctx, orgID); err != nil {
		t.Fatalf("ClearSongArrangementCache: %v", err)
	}

	cache, err = store.SongArrangementInfo(ctx, orgID)
	if err != nil {
		t.Fatalf("SongArrangementInfo (after clear): %v", err)
	}
	if len(cache) != 0 {
		t.Fatalf("SongArrangementInfo (after clear) = %d entries, want 0", len(cache))
	}

	// A different org's cache must be untouched - this is an org-scoped
	// delete, not a table wipe.
	otherOrgID := testScopeID("org")
	cleanupSongArrangementCache(t, store, otherOrgID)
	if err := store.UpsertSongArrangementInfo(ctx, otherOrgID, songA, SongArrangementInfo{Key: "A", BPM: 140}); err != nil {
		t.Fatalf("UpsertSongArrangementInfo (other org): %v", err)
	}
	if err := store.ClearSongArrangementCache(ctx, orgID); err != nil {
		t.Fatalf("ClearSongArrangementCache (re-run on already-empty org): %v", err)
	}
	otherCache, err := store.SongArrangementInfo(ctx, otherOrgID)
	if err != nil {
		t.Fatalf("SongArrangementInfo (other org, after clearing orgID): %v", err)
	}
	if len(otherCache) != 1 {
		t.Fatalf("SongArrangementInfo (other org) = %d entries after clearing a different org, want 1 (untouched)", len(otherCache))
	}
}
