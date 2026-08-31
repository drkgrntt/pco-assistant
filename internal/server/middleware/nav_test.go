package middleware

import (
	"testing"
	"time"
)

func TestCacheEntryFreshWithinTTL(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 30, 0, time.UTC)
	computedAt := now.Add(-30 * time.Second)

	if !cacheEntryFresh(computedAt, now, navStateTTL) {
		t.Errorf("cacheEntryFresh(%v, %v, %v) = false, want true (well within TTL)", computedAt, now, navStateTTL)
	}
}

func TestCacheEntryFreshStaleAfterTTL(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 30, 0, time.UTC)
	computedAt := now.Add(-90 * time.Second)

	if cacheEntryFresh(computedAt, now, navStateTTL) {
		t.Errorf("cacheEntryFresh(%v, %v, %v) = true, want false (well past TTL)", computedAt, now, navStateTTL)
	}
}

func TestCacheEntryFreshAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)
	computedAt := now.Add(-navStateTTL)

	// now.Sub(computedAt) == ttl exactly - treated as stale (strictly less
	// than ttl is fresh), so an entry computed exactly navStateTTL ago
	// triggers a fresh live lookup rather than serving one more cache hit.
	if cacheEntryFresh(computedAt, now, navStateTTL) {
		t.Errorf("cacheEntryFresh(%v, %v, %v) = true, want false (exactly at TTL boundary)", computedAt, now, navStateTTL)
	}
}

func TestCacheEntryFreshJustInsideBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 1, 0, 0, time.UTC)
	computedAt := now.Add(-navStateTTL + time.Millisecond)

	if !cacheEntryFresh(computedAt, now, navStateTTL) {
		t.Errorf("cacheEntryFresh(%v, %v, %v) = false, want true (one millisecond inside TTL)", computedAt, now, navStateTTL)
	}
}
