package controllers

import (
	"context"
	"fmt"

	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// --- Song Info (arrangement cache status + manual sync) ---------------------
//
// The set builder's own read-through cache (resolveArrangementInfo, in
// scheduling.go) already backfills song_arrangement_cache one song at a
// time as sets get built - this page is the "do it all at once" complement,
// for warming a whole library right after connecting a new PCO account
// instead of waiting for it to fill in gradually.

// buildSongInfoData resolves the whole page's status line - how many of
// the org's Planning Center songs already have a cached arrangement.
// Shared between the GET and the POST's re-render (and, via HomeController,
// the signed-in home page's own sync nudge) so all three always reflect
// the current cache state. Takes ctx/organizationID directly rather than
// a *fiber.Ctx, so callers outside this handler's own request don't need
// to fake one.
func (sc *SchedulingController) buildSongInfoData(ctx context.Context, organizationID string) views.SongInfoData {
	songs, songsErr := fetchAllSongs(ctx)
	cache, err := sc.store.SongArrangementInfo(ctx, organizationID)

	data := views.SongInfoData{TotalCount: len(songs)}
	if songsErr != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading songs from Planning Center: "+songsErr.Error())
	}
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return data
	}

	for _, song := range songs {
		if _, ok := cache[song.ID]; ok {
			data.CachedCount++
		}
	}

	return data
}

func (sc *SchedulingController) SongInfoPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)

	data := sc.buildSongInfoData(middleware.RequestContext(c), middleware.CurrentOrganizationID(c))
	return render(c, views.SongInfo(personName, orgName, data))
}

// SyncSongInfo walks every song in the org's Planning Center library and,
// for whichever ones aren't already cached, live-fetches and caches its
// arrangement - the same cache-miss path resolveArrangementInfo takes for
// a single song, just run up front for the whole library instead of
// waiting for each song to be added to a set. Already-cached songs are
// left untouched (not re-fetched) - this fills gaps, it isn't a
// force-refresh.
func (sc *SchedulingController) SyncSongInfo(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchAllSongs(ctx)
	cache, err := sc.store.SongArrangementInfo(ctx, orgID)
	if err != nil {
		data := sc.buildSongInfoData(ctx, orgID)
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return render(c, views.SongInfoBody(data))
	}

	applied, alreadyCached, failed := 0, 0, 0
	for _, song := range songs {
		// info.ArrangementID == "" counts as needing a (re-)fetch too, not
		// just a missing row - same reasoning as PublishSet's own version
		// of this check: a song cached before ArrangementID/KeyID existed
		// as columns has a row that's present but permanently blank on
		// both, so "already cached" alone would never actually correct
		// it - this sync is the one place meant to fix exactly that.
		if info, ok := cache[song.ID]; ok && info.ArrangementID != "" {
			alreadyCached++
			continue
		}
		if _, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, song.ID); err != nil {
			failed++
			continue
		}
		applied++
	}

	data := sc.buildSongInfoData(ctx, orgID)
	if songsErr != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading songs from Planning Center: "+songsErr.Error())
	}
	data.SyncMsg = fmt.Sprintf(
		"Synced %d song(s): %d newly cached, %d already cached, %d failed.",
		len(songs), applied, alreadyCached, failed,
	)

	return render(c, views.SongInfoBody(data))
}
