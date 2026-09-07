package controllers

// DB-backed test for currentKeysBySong (song_vocal_range.go) - the Song
// Vocal Range page's current-key lookup. Gated behind RUN_DB_TESTS=1, same
// as person_data_cache_db_test.go/song_arrangement_cache_db_test.go in this
// same package.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/server/controllers/... -run CurrentKeysBySong -v

import (
	"context"
	"testing"

	"pco-assistant/internal/scheduling"
)

// TestCurrentKeysBySongResolvesCachedKeysAndSkipsMissesQuietly proves the
// three cases currentKeysBySong's own doc comment describes: a song with a
// cached, non-empty arrangement key is present in the returned map; a song
// with no cache entry at all (a miss - resolveArrangementCacheInfo enqueues
// a background sync and returns a zero value) is simply absent, not an
// error or a placeholder; and a song whose cache entry exists but has an
// empty Key (PCO has none set) is likewise absent.
func TestCurrentKeysBySongResolvesCachedKeysAndSkipsMissesQuietly(t *testing.T) {
	sc, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songWithKey := testScopeID("song")
	songUncached := testScopeID("song")
	songEmptyKey := testScopeID("song")
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM song_arrangement_cache WHERE organization_id = $1`, orgID)
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM song_vocal_ranges WHERE organization_id = $1`, orgID)
	})

	if err := store.UpsertSongArrangementInfo(ctx, orgID, songWithKey, scheduling.SongArrangementInfo{Key: "G"}); err != nil {
		t.Fatalf("UpsertSongArrangementInfo (songWithKey): %v", err)
	}
	if err := store.UpsertSongArrangementInfo(ctx, orgID, songEmptyKey, scheduling.SongArrangementInfo{Key: ""}); err != nil {
		t.Fatalf("UpsertSongArrangementInfo (songEmptyKey): %v", err)
	}

	ranges := []scheduling.SongVocalRange{
		{SongID: songWithKey},
		{SongID: songUncached},
		{SongID: songEmptyKey},
	}

	keys := sc.currentKeysBySong(ctx, orgID, ranges)

	if got, want := keys[songWithKey], "G"; got != want {
		t.Errorf("keys[songWithKey] = %q, want %q", got, want)
	}
	if _, ok := keys[songUncached]; ok {
		t.Errorf("keys[songUncached] present, want absent (uncached miss should be a quiet skip)")
	}
	if _, ok := keys[songEmptyKey]; ok {
		t.Errorf("keys[songEmptyKey] present, want absent (empty cached key should be a quiet skip)")
	}
	if len(keys) != 1 {
		t.Errorf("len(keys) = %d, want 1 (only songWithKey should be present)", len(keys))
	}
}
