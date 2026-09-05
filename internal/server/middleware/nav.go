package middleware

import (
	"sync"
	"time"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// navStateTTL bounds how stale the nav's "needs attention" badges can be.
// A minute of staleness on a nav badge is a non-issue - nobody expects a
// count-style nav entry to update within a second of the underlying data
// changing - so this exists purely to stop refetching the org's entire PCO
// song library on every single page view. See PopulateNavState.
const navStateTTL = 60 * time.Second

// cachedNavState is one organization's last computed nav booleans, plus
// when they were computed.
type cachedNavState struct {
	suggestionsPending bool
	computedAt         time.Time
}

// cacheEntryFresh reports whether a cache entry computed at computedAt is
// still usable at now, given ttl. Pulled out as a pure function (mirroring
// backupsToPrune in internal/server/jobs/backup.go) so the freshness rule
// can be unit tested without a fake clock or a real scheduling.Store.
func cacheEntryFresh(computedAt time.Time, now time.Time, ttl time.Duration) bool {
	return now.Sub(computedAt) < ttl
}

// PopulateNavState computes whether the nav's Song Tag Suggestions entry
// has anything actually pending, so Layout (and Home's own Setup list) can
// hide it once there's nothing left to do there. The check needs the org's
// full PCO song library, fetched once here.
//
// This runs on every authenticated page view (registered after
// RequireActiveOrganization in routes.go) since the nav this flag
// controls is part of every page's shared Layout, not just Home. Rather
// than pay the PCO song-library fetch (plus the store query) on every
// request, results are cached per organization for navStateTTL: a cache
// hit just replays the last computed boolean and skips the live lookup
// entirely, so under real traffic this costs roughly one PCO fetch per
// org per navStateTTL window rather than one per request. The cache lives
// in the closure created here, which happens exactly once at startup
// (see routes.go), so it persists for the life of the process.
//
// Used to also compute a second flag (whether any of an org's songs lacked
// cached Song Info, driving a matching nav nag link) - removed along with
// that page/button once a cache miss started syncing itself in the
// background instead of needing anyone to notice and click something (see
// docs/pco-call-efficiency-rollout-plan.md's Phase 2). The PCO song-library
// fetch stays either way, since Song Tag Suggestions still needs it.
//
// Best-effort like every other page-builder in this codebase: a failed
// lookup just leaves the flag unset (ctx.Value comes back false, nudge
// hidden) rather than blocking the page - see
// views.navSuggestionsPending's fail-safe direction. A failed lookup is
// also never cached, so a transient failure doesn't get pinned in as the
// answer for the rest of the TTL window - the next request just retries
// the live lookup.
func PopulateNavState(schedulingStore *scheduling.Store) fiber.Handler {
	var mu sync.Mutex
	cache := make(map[string]cachedNavState)

	return func(c *fiber.Ctx) error {
		orgID := CurrentOrganizationID(c)
		if orgID == "" {
			return c.Next()
		}

		mu.Lock()
		entry, ok := cache[orgID]
		mu.Unlock()
		if ok && cacheEntryFresh(entry.computedAt, time.Now(), navStateTTL) {
			c.Locals(views.NavSuggestionsPendingKey, entry.suggestionsPending)
			return c.Next()
		}

		ctx := RequestContext(c)

		songs, err := scheduling.FetchActiveSongs(ctx, orgID)
		if err != nil {
			return c.Next()
		}

		tags, tagsErr := schedulingStore.ListSongTags(ctx, orgID, "", "")
		ignored, ignoredErr := schedulingStore.ListIgnoredSongTagSuggestionWords(ctx, orgID)
		if tagsErr == nil && ignoredErr == nil {
			groups := scheduling.SongTagSuggestions(songs, tags, ignored)
			suggestionsPending := len(groups) > 0
			c.Locals(views.NavSuggestionsPendingKey, suggestionsPending)

			mu.Lock()
			cache[orgID] = cachedNavState{
				suggestionsPending: suggestionsPending,
				computedAt:         time.Now(),
			}
			mu.Unlock()
		}

		return c.Next()
	}
}
