package controllers

// Cache-aside + windowed-fetch layer for the Build page's "Past plans" tab.
// Root cause this closes: fetchPastPlans (build.go) had no date bound and
// no caching at all - opening the Past tab, every search-box submit, and
// every Prev/Next pagination click each fanned fetchPlansByFilter out
// across a service type's *entire* historical plan list from PCO, every
// single time. UI-side search/pagination were already correct - they just
// operate in-memory on whatever the fetch returns, so the fetch itself was
// the whole problem.
//
// Two independent fixes, mirroring internal/scheduling/pco_songs.go's
// FetchActiveSongs shape (see that file's own doc comments for the
// pattern this follows) plus rosters_shared_cache.go for a second
// reference point:
//
//  1. fetchPastPlansCached below caches the windowed fetch per
//     (organization, service type) - past plans are per-service-type, not
//     global per-org, so both are part of the key (unlike
//     rosterSharedCacheKey, which doesn't need an org id since PCO ids are
//     already globally unique - this key needs one because it's *not*
//     keyed by a PCO id on the org side, just organizationID itself).
//  2. fetchPastPlansWindowed bounds the underlying PCO fetch to roughly
//     the last buildPastPlansWindowMonths, instead of walking every page
//     of a service type's full history, using the fact that "past"
//     results already arrive newest-first (-sort_date) to stop paging
//     once a page's plans are old enough - see that function's own doc
//     comment for why this can't just reuse fetchPlansByFilter's existing
//     concurrent fan-out (deliberately left untouched, per
//     docs/pco-api-risks.md).
//
// Package boundary: stays in internal/server/controllers, alongside
// fetchPastPlans/fetchPlansByFilter (build.go) which this wraps and falls
// back to - same reasoning rosters_shared_cache.go's own header comment
// gives for staying out of internal/scheduling (no scheduling.Store
// dependency here either, just a wrapper around this package's own
// PCO-fetch helpers).
import (
	"context"
	"strings"
	"sync"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// buildPastPlansWindowMonths bounds the Past tab's default (cold-cache)
// fetch to plans from roughly the last N months, rather than paging
// through a service type's entire history on every cache miss. 18 months
// covers well over a full year of "what did we do last [season]" lookback
// for the common case (weekly or near-weekly services) while keeping a
// cold fetch to a handful of pages for nearly every real church, instead
// of one page per 1-2 months of history stretching back years for a
// long-running org. This bounds the *default* load only - a search that
// comes up empty against the windowed result still reaches further back
// (see fetchPastPlansForSearch), so an older plan is never made
// permanently unreachable by this window.
const buildPastPlansWindowMonths = 18

// buildPastPlansWindowMaxPages is a safety net on fetchPastPlansWindowed's
// own early-exit loop, not a normal-case budget - same spirit as
// buildPlansFetchSanityMaxPages. If a service type's plans somehow never
// cross buildPastPlansWindowMonths (bad/missing SortDate data, or a
// genuinely enormous plan count within the window), this stops the
// "windowed" fetch from silently degrading into an unbounded one. 50
// pages is 5,000 plans - far beyond what buildPastPlansWindowMonths would
// ever need to actually walk for a real org.
const buildPastPlansWindowMaxPages = 50

// buildPastPlansCacheTTL is much longer than songCacheTTL/
// rosterSharedCacheTTL (60s/20s) because past-plan data doesn't change
// once a plan is actually in the past - its title and dates are fixed,
// unlike a song library (songs get added) or team membership (people get
// added/removed mid-session). 30 minutes absorbs the overwhelming
// majority of a single Past-tab browsing session - repeated searches,
// Prev/Next clicks - behind one PCO fetch, while still being short enough
// that a plan crossing from "upcoming" into "past" during a long session
// shows up within half an hour rather than requiring a full page reload
// to ever notice.
const buildPastPlansCacheTTL = 30 * time.Minute

// pastPlansCacheKey scopes one cache entry to one organization's one
// service type - unlike rosterSharedCacheKey (keyed on PCO ids alone,
// which are already globally unique), organizationID has to be part of
// this key since it isn't itself a PCO id.
type pastPlansCacheKey struct {
	organizationID string
	serviceTypeID  string
}

// cachedPastPlans is one (organization, service type)'s last successfully
// fetched windowed past-plan list, plus when it was fetched and whether
// that fetch was truncated by the window/page-cap before naturally
// running out of plans (see fetchPastPlansWindowed).
type cachedPastPlans struct {
	plans     []pco.PlanData
	truncated bool
	cachedAt  time.Time
}

var (
	pastPlansCacheMu sync.Mutex
	pastPlansCache   = map[pastPlansCacheKey]cachedPastPlans{}
)

// pastPlansCacheEntryFresh reports whether a cache entry fetched at
// cachedAt is still usable at now, given ttl. Duplicated from
// scheduling.songCacheEntryFresh/rosterSharedCacheEntryFresh - same pure
// 3-line freshness check, same "not worth a shared package for this"
// reasoning both of those already document.
func pastPlansCacheEntryFresh(cachedAt, now time.Time, ttl time.Duration) bool {
	return now.Sub(cachedAt) < ttl
}

// fetchPastPlansWindowed pages sequentially (not scheduling.RunConcurrently'd
// - see below) through the "past" filter, newest-first ("-sort_date",
// matching fetchPastPlans's own ordering), stopping as soon as a page's
// oldest plan (its last row, since each page itself already arrives
// sorted newest-first) falls outside buildPastPlansWindowMonths - instead
// of walking every page of a service type's entire history the way the
// old unbounded fetchPastPlans call did on every single Past-tab
// interaction.
//
// Deliberately sequential, not fanned out: the whole point is to stop
// early once the cutoff is reached, but a concurrent fan-out (like
// fetchPlansByFilter's) needs to know the total page count *before*
// fetching any of them, which defeats "stop as soon as it's old enough."
// This never touches fetchPlansByFilter's own fan-out - it's a separate,
// simpler function for a separate need (bounded-by-date, not
// bounded-by-total-count). In the common case this still costs only a
// handful of sequential round trips (most real orgs' plans cross 18
// months of lookback within 2-4 pages), and that cost is paid at most
// once per buildPastPlansCacheTTL window per (org, service type) thanks
// to fetchPastPlansCached below, not on every Prev/Next click.
//
// Returns truncated=true when the window/page-cap stopped the fetch
// before it naturally ran out of plans - the caller's signal that older,
// unfetched plans may still exist, worth reaching for with a deeper fetch
// if a search comes up empty (see fetchPastPlansForSearch).
func fetchPastPlansWindowed(ctx context.Context, serviceTypeID string) (plans []pco.PlanData, truncated bool, err error) {
	cutoff := time.Now().AddDate(0, -buildPastPlansWindowMonths, 0)

	offset := 0
	for page := 0; page < buildPastPlansWindowMaxPages; page++ {
		resp, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
			Filter:  "past",
			OrderBy: "-sort_date",
			PerPage: buildPlansFetchPageSize,
			Offset:  offset,
		})
		if err != nil {
			return nil, false, err
		}

		plans = append(plans, resp.Data...)

		if len(resp.Data) < buildPlansFetchPageSize {
			// A short page means this is genuinely everything PCO has -
			// nothing left to reach for, so this isn't a truncation.
			return plans, false, nil
		}

		if oldest := resp.Data[len(resp.Data)-1].Attributes.SortDate; oldest.Before(cutoff) {
			return plans, true, nil
		}

		offset += buildPlansFetchPageSize
	}

	// Hit buildPastPlansWindowMaxPages without a short page or crossing
	// the cutoff - the safety net described on that const, not the normal
	// case. Still a truncation: there may well be more.
	return plans, true, nil
}

// fetchPastPlansCached is fetchPastPlansWindowed's cache-aside entry point
// - the one every Past-tab caller should use instead of calling
// fetchPastPlansWindowed (or the old unbounded fetchPastPlans) directly. A
// cache hit within buildPastPlansCacheTTL returns the cached slice (and
// its truncated flag) with no PCO call at all.
//
// Fail-safe, matching FetchActiveSongs/FetchServiceTypes/
// fetchRosterSharedPCOData's shared convention: only a successful fetch is
// cached, so one bad PCO response can't pin a stale/empty result in for
// the rest of the TTL window - the next call just retries live.
func fetchPastPlansCached(ctx context.Context, organizationID, serviceTypeID string) ([]pco.PlanData, bool, error) {
	now := time.Now()
	key := pastPlansCacheKey{organizationID: organizationID, serviceTypeID: serviceTypeID}

	pastPlansCacheMu.Lock()
	entry, ok := pastPlansCache[key]
	pastPlansCacheMu.Unlock()
	if ok && pastPlansCacheEntryFresh(entry.cachedAt, now, buildPastPlansCacheTTL) {
		return entry.plans, entry.truncated, nil
	}

	plans, truncated, err := fetchPastPlansWindowed(ctx, serviceTypeID)
	if err != nil {
		return nil, false, err
	}

	pastPlansCacheMu.Lock()
	pastPlansCache[key] = cachedPastPlans{plans: plans, truncated: truncated, cachedAt: now}
	pastPlansCacheMu.Unlock()

	return plans, truncated, nil
}

// fetchPastPlansForSearch is combinedPlansForServiceType's actual past-tab
// entry point: the cached, windowed fetch above covers the overwhelming
// majority of calls (a fresh Past tab, a Prev/Next click, most searches)
// at a fraction of the old unbounded cost. Only when a search term is
// present, doesn't match anything in the cached/windowed result, *and*
// that windowed result was itself truncated (meaning older, unfetched
// plans might contain a match) does this fall back to one full/unbounded
// fetch (fetchPastPlans, unchanged from before this change existed) - so
// searching for an older plan never comes up empty just because it's
// outside the default window. That deep fallback is deliberately
// uncached: a search actually missing the window entirely is a rare path,
// not worth a second cache key/TTL to reason about next to the one above.
//
// notes is passed through to the same pre-check filterUpcomingPlans call
// combinedPlansForServiceType makes for the results it actually renders,
// so a search matching only a plan's local note (not its title/dates)
// still counts as "found within the window" and doesn't trigger an
// unnecessary deep fetch.
func fetchPastPlansForSearch(ctx context.Context, organizationID, serviceTypeID, search string, notes map[string]string) ([]pco.PlanData, error) {
	plans, truncated, err := fetchPastPlansCached(ctx, organizationID, serviceTypeID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(search) == "" || !truncated {
		return plans, nil
	}

	if len(filterUpcomingPlans(plans, search, notes)) > 0 {
		return plans, nil
	}

	return fetchPastPlans(ctx, serviceTypeID)
}

// invalidatePastPlansCache clears (organizationID, serviceTypeID)'s cached
// past-plan list, if any, so the next fetchPastPlansCached call for that
// service type is guaranteed a fresh windowed fetch rather than a
// possibly-stale cache hit. Exported for any future action that expects a
// guaranteed-fresh pull (mirroring scheduling.InvalidateActiveSongsCache's
// own "kept exported, no current caller" precedent) - no current
// production call site: nothing this app does locally changes a plan's
// own title/dates/existence in PCO for an already-past plan, so
// TTL-only expiry (unlike the roster shared cache, which does have a
// manual "Refresh team data" button for its own shorter window) is enough
// for now.
func InvalidatePastPlansCache(organizationID, serviceTypeID string) {
	pastPlansCacheMu.Lock()
	delete(pastPlansCache, pastPlansCacheKey{organizationID: organizationID, serviceTypeID: serviceTypeID})
	pastPlansCacheMu.Unlock()
}
