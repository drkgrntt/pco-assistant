package middleware

import (
	"testing"
	"time"
)

func TestRecordPCORateLimitThenReadIsTrueOnceThenConsumed(t *testing.T) {
	id := "req-record-then-read"

	RecordPCORateLimit(id)

	if !HadPCORateLimit(id) {
		t.Fatal("expected HadPCORateLimit to be true right after RecordPCORateLimit")
	}
	if HadPCORateLimit(id) {
		t.Error("expected a second HadPCORateLimit call for the same id to be false - the first read should have consumed it")
	}
}

func TestRecordPCORateLimitBlankIDNoops(t *testing.T) {
	// Should not panic, and should never mark anything readable for a
	// blank id.
	RecordPCORateLimit("")

	if HadPCORateLimit("") {
		t.Error("expected HadPCORateLimit(\"\") to be false - blank id is always a no-op")
	}
}

func TestHadPCORateLimitUnrecordedIDIsFalse(t *testing.T) {
	if HadPCORateLimit("req-never-recorded") {
		t.Error("expected HadPCORateLimit to be false for an id RecordPCORateLimit was never called with")
	}
}

func TestSweepStalePCORateLimitsRemovesOnlyStaleEntries(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	hits := map[string]time.Time{
		"stale":        now.Add(-pcoRateLimitEntryTTL - time.Minute),
		"atBoundary":   now.Add(-pcoRateLimitEntryTTL),
		"fresh":        now.Add(-time.Minute),
		"justRecorded": now,
	}

	sweepStalePCORateLimits(hits, now)

	if _, ok := hits["stale"]; ok {
		t.Error("expected an entry recorded well past the TTL to be swept")
	}
	if _, ok := hits["atBoundary"]; ok {
		t.Error("expected an entry recorded exactly at the TTL boundary to be swept (>= ttl, matching cacheEntryFresh's own strict-less-than convention)")
	}
	if _, ok := hits["fresh"]; !ok {
		t.Error("expected a recently-recorded entry to survive the sweep")
	}
	if _, ok := hits["justRecorded"]; !ok {
		t.Error("expected an entry recorded at exactly now to survive the sweep")
	}
}

func TestRecordPCORateLimitSweepsStaleEntriesOnWrite(t *testing.T) {
	staleID := "req-leaked-by-a-redirect"

	pcoRateLimitMu.Lock()
	pcoRateLimitHits[staleID] = time.Now().Add(-pcoRateLimitEntryTTL - time.Minute)
	pcoRateLimitMu.Unlock()

	// Recording an unrelated id triggers the opportunistic sweep, which
	// should throw away the stale entry above even though nothing ever
	// read it via HadPCORateLimit.
	RecordPCORateLimit("req-unrelated")

	pcoRateLimitMu.Lock()
	_, stillThere := pcoRateLimitHits[staleID]
	pcoRateLimitMu.Unlock()

	if stillThere {
		t.Error("expected a stale, never-read entry to be swept away by a later RecordPCORateLimit call")
	}

	// Clean up the entry this test itself created so it doesn't linger
	// for other tests in this package sharing the same package-level map.
	HadPCORateLimit("req-unrelated")
}
