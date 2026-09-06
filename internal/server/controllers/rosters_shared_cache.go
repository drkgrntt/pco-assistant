package controllers

// Cache-aside layer for fetchRosterSharedPCODataLive's two-call PCO fetch
// (GetTeamMembers + GetNeededPositions, see rosters_scoped.go) - closes
// the last deliberately-open gap in docs/pco-api-risks.md's "Current
// state" table ("Team/position lists and team-member names ... fetched
// live, uncached, on every roster render"). Every caller that needs this
// data (buildRosterPositionGroupData, buildRosterLightData,
// buildRosterBuilderData's full-rebuild path, and every rosters_detail.go/
// rosters_person_data_refresh.go handler that calls one of those) already
// goes through fetchRosterSharedPCOData below, so wrapping just that one
// function covers every one of them with no call-site changes.
//
// Package boundary: this cache stays in internal/server/controllers rather
// than internal/scheduling (unlike scheduling.FetchActiveSongs/
// FetchServiceTypes, pco_songs.go/pco_service_types.go) because the value
// being cached - *rosterSharedPCOData - carries TeamInfo, built from
// teamInfoFromIncluded (rosters_builder.go), a controllers-package-local
// type. Moving the cache into internal/scheduling would mean exporting
// that type (or re-deriving it there from the raw PCO response) purely to
// satisfy a package boundary, for no benefit over keeping the cache next
// to the struct and function it wraps.
//
// TTL is intentionally shorter than the 60s norm (scheduling.songCacheTTL,
// scheduling.serviceTypeCacheTTL): a song library or service-type list is
// effectively static within a scheduling session, but team membership can
// change *during* one - an admin adding someone to a team directly in
// Planning Center, right before someone here tries to schedule that same
// person. 20s bounds that staleness window tightly while still absorbing
// the overwhelming majority of render/mutation-driven re-fetches a single
// roster page visit generates. The manual "Refresh team data" button
// (RefreshRosterTeamData, rosters_team_data_refresh.go) is the intended
// remedy for the remaining window, not a longer TTL.
//
// No invalidation on local roster writes: draft roster
// assignments/preferences are local-only until Publish
// (scheduling.Store.AddPersonToPosition et al. write to this app's own
// Postgres, never to PCO) - so nothing this app's own roster mutations do
// ever changes what GetTeamMembers/GetNeededPositions actually return.
// The only source of staleness is a change made directly in Planning
// Center, which expiry (or the manual refresh below) is what handles -
// unlike scheduling.InvalidateActiveSongsCache's use case, there's no
// local-write code path that needs to reach into this cache at all.
import (
	"context"
	"sync"
	"time"
)

// rosterSharedCacheTTL - see this file's own header comment for why this
// is shorter than scheduling.songCacheTTL/scheduling.serviceTypeCacheTTL's
// 60s.
const rosterSharedCacheTTL = 20 * time.Second

// rosterSharedCacheKey scopes one cache entry to one plan. serviceTypeID
// alone doesn't uniquely identify a plan, so both are part of the key;
// organizationID isn't needed alongside them - PCO ids are globally
// unique, and fetchRosterSharedPCOData's own signature already takes no
// orgID for the same reason (see rosters_scoped.go).
type rosterSharedCacheKey struct {
	serviceTypeID string
	planID        string
}

// cachedRosterShared is one (serviceTypeID, planID)'s last successfully
// fetched rosterSharedPCOData, plus when it was fetched.
type cachedRosterShared struct {
	data     *rosterSharedPCOData
	cachedAt time.Time
}

var (
	rosterSharedCacheMu sync.Mutex
	rosterSharedCache   = map[rosterSharedCacheKey]cachedRosterShared{}
)

// rosterSharedCacheEntryFresh reports whether a cache entry fetched at
// cachedAt is still usable at now, given ttl. Duplicated from
// scheduling.songCacheEntryFresh (internal/scheduling/pco_songs.go) - same
// pure 3-line freshness check, same reasoning documented there: that
// helper is unexported in a different package, and a 3-line pure function
// isn't worth introducing a shared package (or exporting it) for.
func rosterSharedCacheEntryFresh(cachedAt, now time.Time, ttl time.Duration) bool {
	return now.Sub(cachedAt) < ttl
}

// fetchRosterSharedPCOData is the cache-aside entry point every caller in
// this package uses for rosterSharedPCOData - see
// fetchRosterSharedPCODataLive (rosters_scoped.go) for the actual
// two-call PCO fetch this wraps. A cache hit within rosterSharedCacheTTL
// returns the cached result with no PCO call at all; a miss (no entry, or
// one past its TTL) fetches live and, on full success, caches the result
// for the next caller (for this plan, within the window).
//
// Fail-safe, matching scheduling.FetchActiveSongs/FetchServiceTypes'
// convention: only a fetch with both MembersErr and NeededErr nil is
// cached. A transient PCO failure on either call is never cached, so one
// bad response can't pin a stale/partial result in for the rest of the
// TTL window - the caller gets that error back directly (uncached) and
// the next call simply retries live.
func fetchRosterSharedPCOData(ctx context.Context, serviceTypeID, planID string) *rosterSharedPCOData {
	now := time.Now()
	key := rosterSharedCacheKey{serviceTypeID: serviceTypeID, planID: planID}

	rosterSharedCacheMu.Lock()
	entry, ok := rosterSharedCache[key]
	rosterSharedCacheMu.Unlock()
	if ok && rosterSharedCacheEntryFresh(entry.cachedAt, now, rosterSharedCacheTTL) {
		return entry.data
	}

	d := fetchRosterSharedPCODataLive(ctx, serviceTypeID, planID)
	if d.MembersErr == nil && d.NeededErr == nil {
		rosterSharedCacheMu.Lock()
		rosterSharedCache[key] = cachedRosterShared{data: d, cachedAt: now}
		rosterSharedCacheMu.Unlock()
	}
	return d
}

// invalidateRosterSharedPCOCache clears (serviceTypeID, planID)'s cached
// rosterSharedPCOData, if any, so the very next fetchRosterSharedPCOData
// call for that plan is guaranteed a fresh live fetch rather than a
// possibly-stale cache hit. Called by RefreshRosterTeamData
// (rosters_team_data_refresh.go) - the roster page's manual "Refresh team
// data" button - immediately before it rebuilds the page, matching
// scheduling.InvalidateActiveSongsCache/InvalidateServiceTypesCache's
// shape. Unexported (unlike those two): both this cache and its only
// caller live in this same package, so there's no cross-package boundary
// to export across.
func invalidateRosterSharedPCOCache(serviceTypeID, planID string) {
	rosterSharedCacheMu.Lock()
	delete(rosterSharedCache, rosterSharedCacheKey{serviceTypeID: serviceTypeID, planID: planID})
	rosterSharedCacheMu.Unlock()
}
