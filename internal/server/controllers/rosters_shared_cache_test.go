package controllers

import (
	"testing"
	"time"
)

// --- rosterSharedCacheEntryFresh ----------------------------------------
//
// Same boundary-case coverage as scheduling's songCacheEntryFresh tests
// (internal/scheduling/pco_songs_test.go), since this is a deliberate
// duplicate of that pure function - see rosters_shared_cache.go's own doc
// comment for why it isn't shared cross-package instead.

func TestRosterSharedCacheEntryFreshWithinTTL(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 10, 0, time.UTC)
	cachedAt := now.Add(-10 * time.Second)

	if !rosterSharedCacheEntryFresh(cachedAt, now, rosterSharedCacheTTL) {
		t.Errorf("rosterSharedCacheEntryFresh(%v, %v, %v) = false, want true (well within TTL)", cachedAt, now, rosterSharedCacheTTL)
	}
}

func TestRosterSharedCacheEntryFreshStaleAfterTTL(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 30, 0, time.UTC)
	cachedAt := now.Add(-30 * time.Second)

	if rosterSharedCacheEntryFresh(cachedAt, now, rosterSharedCacheTTL) {
		t.Errorf("rosterSharedCacheEntryFresh(%v, %v, %v) = true, want false (well past TTL)", cachedAt, now, rosterSharedCacheTTL)
	}
}

func TestRosterSharedCacheEntryFreshAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 20, 0, time.UTC)
	cachedAt := now.Add(-rosterSharedCacheTTL)

	// now.Sub(cachedAt) == ttl exactly - treated as stale (strictly less
	// than ttl is fresh), same convention as songCacheEntryFresh/
	// middleware.cacheEntryFresh.
	if rosterSharedCacheEntryFresh(cachedAt, now, rosterSharedCacheTTL) {
		t.Errorf("rosterSharedCacheEntryFresh(%v, %v, %v) = true, want false (exactly at TTL boundary)", cachedAt, now, rosterSharedCacheTTL)
	}
}

func TestRosterSharedCacheEntryFreshJustInsideBoundary(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 20, 0, time.UTC)
	cachedAt := now.Add(-rosterSharedCacheTTL + time.Millisecond)

	if !rosterSharedCacheEntryFresh(cachedAt, now, rosterSharedCacheTTL) {
		t.Errorf("rosterSharedCacheEntryFresh(%v, %v, %v) = false, want true (one millisecond inside TTL)", cachedAt, now, rosterSharedCacheTTL)
	}
}
