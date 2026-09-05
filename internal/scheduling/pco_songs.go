package scheduling

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pco-assistant/internal/logger"

	pco "github.com/drkgrntt/go-pco"
)

// pcoSongPageSize is PCO's max per_page. pcoSongSanityMaxPages is a
// runaway/bug guard, not a real cap: fetchSongsFromPCO always fans out
// exactly as many pages as pco.Meta.TotalCount says it needs (see below),
// so a real library only ever exceeds this if TotalCount itself is
// implausible - covers ~20,000 songs, far beyond any real church's
// library.
const (
	pcoSongPageSize       = 100
	pcoSongSanityMaxPages = 200
)

// songCacheTTL bounds how stale a cached active-song list can be, matching
// middleware.navStateTTL's value and reasoning (see nav.go): a minute of
// staleness on a song picker/candidate list is a non-issue, and this exists
// purely to stop re-fetching the org's entire PCO song library on every
// page view and htmx add/remove.
const songCacheTTL = 60 * time.Second

// cachedSongs is one organization's last fetched-and-filtered active song
// list, plus when it was fetched.
type cachedSongs struct {
	songs    []pco.SongData
	cachedAt time.Time
}

var (
	songCacheMu sync.Mutex
	songCache   = map[string]cachedSongs{}
)

// songCacheEntryFresh reports whether a cache entry fetched at cachedAt is
// still usable at now, given ttl. Duplicated from
// internal/server/middleware/nav.go's cacheEntryFresh (same pure 3-line
// freshness check, same reasoning) rather than shared cross-package -
// scheduling and middleware don't import each other today, and one
// 3-line pure function isn't worth introducing a new shared package for.
func songCacheEntryFresh(cachedAt, now time.Time, ttl time.Duration) bool {
	return now.Sub(cachedAt) < ttl
}

// fetchSongsFromPCO pages through the PCO song library, ordered by title.
// Fetches page 0 first (synchronously) to learn Meta.TotalCount, then fans
// the remaining pages out concurrently via RunConcurrently - deliberately
// not Links.Next-driven, since that's inherently sequential (page N+1's
// existence is only known after page N returns), which conflicts with a
// concurrent fan-out; TotalCount, known after page 0, is what makes fanning
// out the rest possible.
//
// When includeArchived is false, also passes SongsParams.Hidden=false so
// PCO itself excludes archived songs server-side (smaller pages, fewer
// PCO calls) - this is an optimization only, not the correctness
// guarantee: filterActiveSongs (called by FetchActiveSongs) is what
// actually guarantees no hidden song ever reaches a caller, regardless of
// whether this server-side filter is honored.
//
// Error handling: unlike the old FetchAllSongs, this returns a real error
// (no partial slice) on any page's failure - concurrent fan-out makes
// "return whatever succeeded so far" meaningfully harder to reason about
// (which pages actually landed?) for little practical benefit, since a
// failed page here already means something's wrong enough that a
// wrong/incomplete song list would be worse than a clear error.
func fetchSongsFromPCO(ctx context.Context, includeArchived bool) ([]pco.SongData, error) {
	params := &pco.SongsParams{
		OrderBy: "title",
		PerPage: pcoSongPageSize,
		Offset:  0,
	}
	if !includeArchived {
		hidden := false
		params.Hidden = &hidden
	}

	first, err := pco.GetSongs(ctx, params)
	if err != nil {
		return nil, err
	}

	totalCount := first.Meta.TotalCount
	totalPages := (totalCount + pcoSongPageSize - 1) / pcoSongPageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if totalPages > pcoSongSanityMaxPages {
		logger.ErrorContext(ctx, "PCO song library page count exceeds sanity ceiling - aborting fetch",
			"total_count", totalCount, "total_pages", totalPages, "sanity_max_pages", pcoSongSanityMaxPages)
		return nil, fmt.Errorf("PCO reports %d songs (%d pages) - exceeds sanity ceiling of %d pages, aborting", totalCount, totalPages, pcoSongSanityMaxPages)
	}

	pages := make([][]pco.SongData, totalPages)
	pages[0] = first.Data

	if totalPages > 1 {
		pageIndexes := make([]int, totalPages-1)
		for i := range pageIndexes {
			pageIndexes[i] = i + 1
		}

		var fetchErr error
		var errMu sync.Mutex

		RunConcurrently(pageIndexes, func(page int) {
			pageParams := &pco.SongsParams{
				OrderBy: "title",
				PerPage: pcoSongPageSize,
				Offset:  page * pcoSongPageSize,
			}
			if !includeArchived {
				hidden := false
				pageParams.Hidden = &hidden
			}

			resp, err := pco.GetSongs(ctx, pageParams)
			if err != nil {
				errMu.Lock()
				if fetchErr == nil {
					fetchErr = err
				}
				errMu.Unlock()
				return
			}
			// pages[page] is this goroutine's own, exclusive index into a
			// pre-sized slice - no lock needed (see RunConcurrently's doc
			// comment).
			pages[page] = resp.Data
		})

		if fetchErr != nil {
			return nil, fetchErr
		}
	}

	songs := make([]pco.SongData, 0, totalCount)
	for _, page := range pages {
		songs = append(songs, page...)
	}

	return songs, nil
}

// filterActiveSongs drops every hidden (archived) song, preserving input
// order. This is the real source-of-truth correctness guarantee that
// archived songs never surface anywhere in this app - not just an
// optimization layered on top of fetchSongsFromPCO's server-side Hidden
// filter, which stays best-effort in comparison.
func filterActiveSongs(songs []pco.SongData) []pco.SongData {
	active := make([]pco.SongData, 0, len(songs))
	for _, song := range songs {
		if song.Attributes.Hidden {
			continue
		}
		active = append(active, song)
	}
	return active
}

// FetchActiveSongs is the cached, org-scoped, archived-filtered entry point
// almost every caller in this app should use - see fetchSongsFromPCO/
// filterActiveSongs for how the fetch and the filter each work. Results
// are cached per organization for songCacheTTL: a cache hit within the TTL
// window returns the cached slice with no PCO call at all, so under real
// traffic this costs roughly one full-library PCO fetch per org per
// songCacheTTL window rather than one per page view/htmx action.
//
// A transient failure is never cached (mirrors nav.go's PopulateNavState
// convention) - only a successful fetch is stored, so one bad PCO response
// can't pin a stale/empty result in for the rest of the TTL window; the
// next call just retries the live lookup.
func FetchActiveSongs(ctx context.Context, organizationID string) ([]pco.SongData, error) {
	now := time.Now()

	songCacheMu.Lock()
	entry, ok := songCache[organizationID]
	songCacheMu.Unlock()
	if ok && songCacheEntryFresh(entry.cachedAt, now, songCacheTTL) {
		return entry.songs, nil
	}

	songs, err := fetchSongsFromPCO(ctx, false)
	if err != nil {
		return nil, err
	}
	active := filterActiveSongs(songs)

	songCacheMu.Lock()
	songCache[organizationID] = cachedSongs{songs: active, cachedAt: now}
	songCacheMu.Unlock()

	return active, nil
}

// FetchAllSongsIncludingArchived pages through the PCO song library
// including archived (hidden) songs, uncached and unscoped by
// organization - only for cmd/pcoreset, which needs to see (and delete)
// archived songs too as part of a full org reset. Every other caller
// should use FetchActiveSongs.
func FetchAllSongsIncludingArchived(ctx context.Context) ([]pco.SongData, error) {
	return fetchSongsFromPCO(ctx, true)
}

// InvalidateActiveSongsCache clears organizationID's cached active-song
// list, if any, so the next FetchActiveSongs call for that org is
// guaranteed a fresh live fetch rather than a possibly-stale cache hit.
// Exported for any action that explicitly expects a guaranteed-fresh pull
// from PCO, not a possibly-60s-stale cached one - no current production
// call site (the Song Info "sync now" action this originally backed was
// removed along with the rest of the button/nag it was part of, see
// docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2), kept exported as a
// small, already-tested piece of FetchActiveSongs' own cache API rather
// than deleted purely for lack of a caller today.
func InvalidateActiveSongsCache(organizationID string) {
	songCacheMu.Lock()
	delete(songCache, organizationID)
	songCacheMu.Unlock()
}
