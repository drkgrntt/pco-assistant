package middleware

import (
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// PopulateNavState computes, once per request, whether the nav's Song Tag
// Suggestions / Song Info entries have anything actually pending, so
// Layout (and Home's own Setup list) can hide either one once there's
// nothing left to do there. Both checks need the org's full PCO song
// library - there's no cheaper way to know "is everything synced" without
// knowing the real total - fetched once here and reused for both rather
// than twice.
//
// This is a real, accepted cost, not something to pretend is free: it
// runs on every authenticated page view (registered after
// RequireActiveOrganization in routes.go) since the nav these flags
// control is part of every page's shared Layout, not just Home - one more
// PCO song-library fetch per page load, in exchange for the nav actually
// reflecting whether either page still needs attention.
//
// Best-effort like every other page-builder in this codebase: a failed
// lookup just leaves the corresponding flag unset (ctx.Value comes back
// false, nudge hidden) rather than blocking the page - see
// views.navSuggestionsPending/navSongInfoNeedsSync's fail-safe direction.
func PopulateNavState(schedulingStore *scheduling.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgID := CurrentOrganizationID(c)
		if orgID == "" {
			return c.Next()
		}
		ctx := RequestContext(c)

		songs, err := scheduling.FetchAllSongs(ctx)
		if err != nil {
			return c.Next()
		}

		if cache, err := schedulingStore.SongArrangementInfo(ctx, orgID); err == nil {
			cached := 0
			for _, s := range songs {
				if _, ok := cache[s.ID]; ok {
					cached++
				}
			}
			c.Locals(views.NavSongInfoNeedsSyncKey, cached < len(songs))
		}

		tags, tagsErr := schedulingStore.ListSongTags(ctx, orgID, "", "")
		ignored, ignoredErr := schedulingStore.ListIgnoredSongTagSuggestionWords(ctx, orgID)
		if tagsErr == nil && ignoredErr == nil {
			groups := scheduling.SongTagSuggestions(songs, tags, ignored)
			c.Locals(views.NavSuggestionsPendingKey, len(groups) > 0)
		}

		return c.Next()
	}
}
