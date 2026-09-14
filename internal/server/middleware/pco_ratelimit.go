package middleware

import (
	"sync"
	"time"
)

// pcoRateLimitEntryTTL bounds how long a recorded 429 waits to be read
// (and consumed) before RecordPCORateLimit's own opportunistic sweep
// throws it away as garbage. A render that asks "did a 429 happen during
// this request" always runs within milliseconds of the request that
// recorded it - controllers/render.go marks the read right before
// rendering, on the very same request - so 5 minutes is generous headroom,
// not a tuned value: it exists purely to bound a request whose handler
// gets marked but never calls render() at all (a redirect, say), which
// would otherwise pin its entry in the map forever.
const pcoRateLimitEntryTTL = 5 * time.Minute

// pcoRateLimitHits tracks, per request id, when that request's own PCO
// calls hit a 429 - written by RecordPCORateLimit (called from
// internal/server/pco_observability.go's go-pco response hook, mid-
// handler, the moment a 429 comes back), read and consumed by
// HadPCORateLimit (called from controllers/render.go, once the handler's
// own PCO calls are all done and it's about to render the page).
//
// Lives in this package rather than internal/server (where the response
// hook that writes it is registered) because internal/server/controllers
// - where render.go, the reader, lives - can't import internal/server
// without a cycle; internal/server/middleware already sits between both
// packages and already owns the matching per-request identity concept
// (CurrentRequestID) that this map is keyed by, so this is the natural
// shared home for it.
//
// A single package-level map guarded by a mutex, not per-organization or
// per-session cached state the way PopulateNavState's cache is (nav.go) -
// this is deliberately read-once and single-request-scoped: a 429 should
// surface for the one render that asks about it and then be gone, not
// linger to (incorrectly) describe a later render. Request ids are
// UUIDv4 (see requestID.go), so a stale unread entry is never actually
// misattributed to a different later request reusing the same id - the
// read-once contract just keeps the semantics simple regardless: "was
// *this* request marked," not "has anyone, ever, been marked."
var (
	pcoRateLimitMu   sync.Mutex
	pcoRateLimitHits = make(map[string]time.Time)
)

// RecordPCORateLimit marks requestID as having taken at least one PCO 429
// during its handling. Safe to call more than once for the same id - a
// single page render can make well over a hundred PCO calls (see
// docs/pco-api-risks.md's "PCO API calls" section) and more than one of
// them can 429 - a later call just refreshes the timestamp. No-op on a
// blank id (a call site with no request to attribute this to).
//
// Also opportunistically sweeps entries older than pcoRateLimitEntryTTL
// before inserting - see that constant's doc comment for why an entry can
// otherwise leak (marked mid-handler, but the handler never reaches
// render(), e.g. a redirect) and grow the map unbounded over the life of
// the process.
func RecordPCORateLimit(requestID string) {
	if requestID == "" {
		return
	}

	pcoRateLimitMu.Lock()
	defer pcoRateLimitMu.Unlock()

	sweepStalePCORateLimits(pcoRateLimitHits, time.Now())
	pcoRateLimitHits[requestID] = time.Now()
}

// HadPCORateLimit reports whether requestID was marked by
// RecordPCORateLimit, and consumes the mark as part of the same call - a
// second call for the same id returns false. Read-once by design (see the
// package-level doc comment above): a single 429 surfaces for exactly the
// one render that asks about it, not for any render after it. No-op
// (returns false) on a blank id.
func HadPCORateLimit(requestID string) bool {
	if requestID == "" {
		return false
	}

	pcoRateLimitMu.Lock()
	defer pcoRateLimitMu.Unlock()

	_, hit := pcoRateLimitHits[requestID]
	delete(pcoRateLimitHits, requestID)
	return hit
}

// sweepStalePCORateLimits removes every entry recorded pcoRateLimitEntryTTL
// or longer before now from hits, mutating it in place. Pulled out as a
// pure function (mirroring cacheEntryFresh in nav.go) so the sweep rule is
// unit-testable against a controlled map and clock, rather than needing to
// wait on a real 5-minute clock or reach into the package-level map.
func sweepStalePCORateLimits(hits map[string]time.Time, now time.Time) {
	for id, recordedAt := range hits {
		if now.Sub(recordedAt) >= pcoRateLimitEntryTTL {
			delete(hits, id)
		}
	}
}
