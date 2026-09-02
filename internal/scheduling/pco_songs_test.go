package scheduling

import (
	"testing"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

func songData(id, title string, hidden bool) pco.SongData {
	return pco.SongData{ID: id, Attributes: pco.SongAttributes{Title: title, Hidden: hidden}}
}

// --- songCacheEntryFresh -----------------------------------------------
//
// Same boundary-case coverage as middleware/nav_test.go's cacheEntryFresh
// tests, since this is a deliberate duplicate of that pure function.

func TestSongCacheEntryFreshWithinTTL(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 30, 0, time.UTC)
	cachedAt := now.Add(-30 * time.Second)

	if !songCacheEntryFresh(cachedAt, now, songCacheTTL) {
		t.Errorf("songCacheEntryFresh(%v, %v, %v) = false, want true (well within TTL)", cachedAt, now, songCacheTTL)
	}
}

func TestSongCacheEntryFreshStaleAfterTTL(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 30, 0, time.UTC)
	cachedAt := now.Add(-90 * time.Second)

	if songCacheEntryFresh(cachedAt, now, songCacheTTL) {
		t.Errorf("songCacheEntryFresh(%v, %v, %v) = true, want false (well past TTL)", cachedAt, now, songCacheTTL)
	}
}

func TestSongCacheEntryFreshAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)
	cachedAt := now.Add(-songCacheTTL)

	// now.Sub(cachedAt) == ttl exactly - treated as stale (strictly less
	// than ttl is fresh), same convention as nav.go's cacheEntryFresh.
	if songCacheEntryFresh(cachedAt, now, songCacheTTL) {
		t.Errorf("songCacheEntryFresh(%v, %v, %v) = true, want false (exactly at TTL boundary)", cachedAt, now, songCacheTTL)
	}
}

func TestSongCacheEntryFreshJustInsideBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)
	cachedAt := now.Add(-songCacheTTL + time.Millisecond)

	if !songCacheEntryFresh(cachedAt, now, songCacheTTL) {
		t.Errorf("songCacheEntryFresh(%v, %v, %v) = false, want true (one millisecond inside TTL)", cachedAt, now, songCacheTTL)
	}
}

// --- filterActiveSongs ---------------------------------------------------

func TestFilterActiveSongsDropsHiddenPreservesOrder(t *testing.T) {
	songs := []pco.SongData{
		songData("1", "Alpha", false),
		songData("2", "Beta (archived)", true),
		songData("3", "Gamma", false),
		songData("4", "Delta (archived)", true),
		songData("5", "Epsilon", false),
	}

	got := filterActiveSongs(songs)

	wantIDs := []string{"1", "3", "5"}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d active songs, got %d: %+v", len(wantIDs), len(got), got)
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("index %d: expected song id %s, got %s (order not preserved or wrong song kept)", i, id, got[i].ID)
		}
	}
}

func TestFilterActiveSongsAllHidden(t *testing.T) {
	songs := []pco.SongData{
		songData("1", "Alpha", true),
		songData("2", "Beta", true),
	}

	got := filterActiveSongs(songs)
	if len(got) != 0 {
		t.Errorf("expected no active songs, got %+v", got)
	}
	if got == nil {
		t.Error("expected a non-nil empty slice, got nil")
	}
}

func TestFilterActiveSongsEmptyInput(t *testing.T) {
	got := filterActiveSongs(nil)
	if len(got) != 0 {
		t.Errorf("expected no active songs, got %+v", got)
	}
	if got == nil {
		t.Error("expected a non-nil empty slice for empty input, got nil")
	}
}

func TestFilterActiveSongsNoneHidden(t *testing.T) {
	songs := []pco.SongData{
		songData("1", "Alpha", false),
		songData("2", "Beta", false),
	}

	got := filterActiveSongs(songs)
	if len(got) != 2 {
		t.Errorf("expected both songs kept, got %+v", got)
	}
}
