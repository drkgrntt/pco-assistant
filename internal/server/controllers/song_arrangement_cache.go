package controllers

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Song Info (arrangement cache status + manual sync) ---------------------
//
// The set builder's own read-through cache (resolveArrangementInfo, in
// scheduling.go) already backfills song_arrangement_cache one song at a
// time as sets get built - this page is the "do it all at once" complement,
// for warming a whole library right after connecting a new PCO account
// instead of waiting for it to fill in gradually.
//
// A first sync for a library of any real size needs two live PCO calls
// per uncached song (arrangement + key) - up to ~200 calls for a 100-song
// library, against PCO's 100-requests/20-seconds-per-user-token budget.
// Running that inline in the request would either block the response for
// a minute-plus (sequential) or risk a 429 (naively concurrent - see
// runSongInfoSync below). Instead SyncSongInfo only kicks off a paced
// background sync and returns immediately; songSyncInProgress guards
// against a second concurrent pass for the same org, and buildSongInfoData
// already recomputes CachedCount/TotalCount live from the DB on every
// render, so simply reloading/revisiting the Song Info page shows the
// sync's progress for free - no separate progress-tracking table needed.

// songSyncInProgress tracks which organizations currently have a
// background song info sync running, so a second click (or a page
// re-render racing the first click) doesn't start a duplicate pass that
// would double PCO call volume for no benefit - already-cached songs are
// skipped either way, but the running goroutine hasn't necessarily reached
// every song yet, so a second pass could still re-fetch ones the first
// pass hasn't gotten to. Same mutex+map pattern as
// internal/server/middleware/nav.go's PopulateNavState cache and
// internal/scheduling/pco_songs.go's songCache - a package-level guard is
// fine here since SchedulingController itself is constructed once and
// lives for the process's whole lifetime (see NewSchedulingController).
var (
	songSyncMu         sync.Mutex
	songSyncInProgress = map[string]bool{}
)

// songSyncTryStart is the pure check-and-set at the core of the
// in-progress guard above: reports whether organizationID was NOT already
// marked running in inProgress, marking it running as a side effect when
// it wasn't. Split out from the package-level songSyncInProgress/
// songSyncMu pair so the check-and-set logic is unit-testable against a
// plain map, without goroutines or a real mutex - see
// song_arrangement_cache_test.go. Callers hold songSyncMu for the
// duration of this call.
func songSyncTryStart(inProgress map[string]bool, organizationID string) bool {
	if inProgress[organizationID] {
		return false
	}
	inProgress[organizationID] = true
	return true
}

// songInfoSyncBatchSize batches songs needing a sync into groups of this
// many, run concurrently via scheduling.RunConcurrently, with a pause
// (songInfoSyncBatchPause) between batches - see runSongInfoSync for why
// batching-with-pauses, not one big concurrent fan-out, is the right
// shape here. Reuses scheduling.PCOFanOutLimit rather than a separate
// constant, for consistency with the batch size every other bounded PCO
// fan-out in this codebase already uses.
const songInfoSyncBatchSize = scheduling.PCOFanOutLimit

// songInfoSyncBatchPause is the pause between song-info sync batches.
// Each song needing a sync costs 2 PCO calls (arrangement + key, see
// fetchAndCacheArrangementInfo), so one songInfoSyncBatchSize-song batch
// (10 songs) costs up to 20 calls, fired near-simultaneously since the
// batch itself runs concurrently - fast enough that, for this pacing
// calculation, a batch's own PCO calls all effectively land in the same
// instant relative to a 20-second window.
//
// Against PCO's 100-requests/20-seconds-per-user-token budget, pausing 10
// seconds between batch starts means at most 3 batch-starts (60 calls)
// can ever land inside any 20-second sliding window (batches at
// t=0,10,20,... - a window can catch at most 3 of those 10s apart). That
// keeps this sync's own sustained rate at 60% of the budget, leaving
// comfortable headroom for the rest of the app's ordinary PCO traffic
// (nav-state fetches, other pages, other signed-in users on the same
// organization's token) sharing that same budget while a sync runs in the
// background - "well under" the limit rather than right up against it,
// which matters because this constant has to be safe for a library
// needing hundreds of calls (dozens of batches), not just a small one.
//
// The cost of this conservatism is wall-clock time, not correctness: a
// 100-song first sync (10 batches) takes on the order of 10 * 10s = 100
// seconds. That's an acceptable trade now that the sync runs in the
// background instead of blocking an HTTP response - see SyncSongInfo.
const songInfoSyncBatchPause = 10 * time.Second

// songSyncBatches splits songIDs into consecutive batches of at most size
// songs each, preserving order. Pure (no PCO/DB/goroutines involved) so
// the "given N songs and a batch size, how many batches, how big is the
// last one" arithmetic is unit-testable on its own - see
// song_arrangement_cache_test.go.
func songSyncBatches(songIDs []string, size int) [][]string {
	if len(songIDs) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(songIDs)
	}

	batches := make([][]string, 0, (len(songIDs)+size-1)/size)
	for i := 0; i < len(songIDs); i += size {
		end := i + size
		if end > len(songIDs) {
			end = len(songIDs)
		}
		batches = append(batches, songIDs[i:end])
	}
	return batches
}

// buildSongInfoData resolves the whole page's status line - how many of
// the org's Planning Center songs already have a cached arrangement, and
// whether a background sync is currently running for this org. Shared
// between the GET and the POST's re-render (and, via HomeController, the
// signed-in home page's own sync nudge) so all three always reflect the
// current cache state. Takes ctx/organizationID directly rather than a
// *fiber.Ctx, so callers outside this handler's own request don't need to
// fake one.
func (sc *SchedulingController) buildSongInfoData(ctx context.Context, organizationID string) views.SongInfoData {
	songs, songsErr := fetchActiveSongs(ctx, organizationID)
	cache, err := sc.store.SongArrangementInfo(ctx, organizationID)

	songSyncMu.Lock()
	syncInProgress := songSyncInProgress[organizationID]
	songSyncMu.Unlock()

	data := views.SongInfoData{TotalCount: len(songs), SyncInProgress: syncInProgress}
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

// SyncSongInfo kicks off a background sync of the org's whole Planning
// Center song library - for whichever songs aren't already cached (the
// same cache-miss path resolveArrangementInfo takes for a single song,
// just run up front for the whole library instead of waiting for each
// song to be added to a set) - and returns immediately rather than
// blocking the response on it; see runSongInfoSync for the actual work
// and why it's paced rather than run all at once. Already-cached songs
// are left untouched (not re-fetched) - this fills gaps, it isn't a
// force-refresh.
//
// If a sync is already running for this org (songSyncInProgress), this
// does not start a second one - it would double PCO call volume for no
// benefit, since the first pass hasn't necessarily reached every song
// yet.
func (sc *SchedulingController) SyncSongInfo(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)

	songSyncMu.Lock()
	started := songSyncTryStart(songSyncInProgress, orgID)
	songSyncMu.Unlock()

	if !started {
		data := sc.buildSongInfoData(ctx, orgID)
		data.SyncMsg = "Sync already in progress - check back in a bit."
		return render(c, views.SongInfoBody(data))
	}

	// This is the one place a user explicitly expects a guaranteed-fresh
	// pull from PCO, not a possibly-60s-stale cached one - invalidate right
	// before the background sync fetches its song list, rather than relying
	// on the TTL to happen to have expired already. This also means the
	// live fetch buildSongInfoData below does to render this response
	// primes the cache, so runSongInfoSync's own FetchActiveSongs call is
	// likely a cache hit rather than a second live fetch.
	scheduling.InvalidateActiveSongsCache(orgID)

	// accessToken, not ctx, is what the background goroutine gets: ctx
	// (middleware.RequestContext(c)) is derived from this fiber request and
	// is cancelled the moment this handler returns, but the sync needs to
	// keep running well after that. context.Background() plus this same
	// access token (see go-pco.WithAccessToken via middleware.AccessToken's
	// doc comment) gives the goroutine an equivalent, independently-lived
	// authenticated context.
	accessToken := middleware.AccessToken(c)
	store := sc.store
	go runSongInfoSync(pco.WithAccessToken(context.Background(), accessToken), store, orgID)

	data := sc.buildSongInfoData(ctx, orgID)
	data.SyncMsg = "Sync started in the background - refresh this page in a bit to see progress."
	return render(c, views.SongInfoBody(data))
}

// runPacedBatches runs fn once for every item across batches, one batch at
// a time: each batch's items run concurrently via scheduling.RunConcurrently
// (bounded to scheduling.PCOFanOutLimit at once), with a pause of
// batchPause between the end of one batch and the start of the next - see
// songInfoSyncBatchPause's doc comment for the call-volume-per-20-seconds
// math this pacing is built around. Extracted from runSongInfoSync's own
// loop, unchanged, so the pacing mechanism itself (batch size, concurrency
// bound, inter-batch pause) is testable on its own with a lightweight fn -
// no database/PCO dependency required - independent of the DB-backed work
// runSongInfoSync's fn actually does; see
// TestRunPacedBatchesWaitsBetweenBatches in song_arrangement_cache_test.go,
// the one test in this task the user most needed confidence in.
func runPacedBatches[T any](batches [][]T, batchPause time.Duration, fn func(T)) {
	for i, batch := range batches {
		scheduling.RunConcurrently(batch, fn)

		if i < len(batches)-1 {
			time.Sleep(batchPause)
		}
	}
}

// runSongInfoSync is SyncSongInfo's actual work, run in a background
// goroutine outside the request/response cycle so an org needing hundreds
// of PCO calls to backfill its song library doesn't block an HTTP
// response for minutes. ctx must be a context.Background()-derived
// context carrying the signed-in user's PCO access token (see
// pco.WithAccessToken) built before the handler returned - not
// middleware.RequestContext(c)'s context, which would already be
// cancelled by the time this runs.
//
// Deliberately NOT "wrap the whole loop in scheduling.RunConcurrently":
// that would make the sync faster, but that means cramming what can be
// hundreds of PCO calls into a much shorter window, making a 429 MORE
// likely, not less - PCO's rate limit is 100 requests/20s per user token,
// and a large library's first sync alone can need double that budget.
// Songs needing a sync are instead split into small batches
// (songInfoSyncBatchSize) and run via runPacedBatches, which fans each
// batch out concurrently via scheduling.RunConcurrently with a deliberate
// pause (songInfoSyncBatchPause) between batches - see that constant's doc
// comment for the call-volume-per-20-seconds math. This trades wall-clock
// time (a big first sync can take minutes) for staying comfortably under
// PCO's rate limit regardless of library size - an acceptable trade now
// that this runs in the background instead of blocking a response.
//
// Same best-effort error handling as the fully-sequential loop this
// replaces: one song's failure is logged and skipped, never aborting the
// batch or the rest of the sync.
func runSongInfoSync(ctx context.Context, store *scheduling.Store, organizationID string) {
	defer func() {
		songSyncMu.Lock()
		delete(songSyncInProgress, organizationID)
		songSyncMu.Unlock()
	}()

	songs, err := scheduling.FetchActiveSongs(ctx, organizationID)
	if err != nil {
		logger.Error("background song info sync: fetching active songs failed", "organization_id", organizationID, "error", err)
		return
	}

	cache, err := store.SongArrangementInfo(ctx, organizationID)
	if err != nil {
		logger.Error("background song info sync: loading arrangement cache failed", "organization_id", organizationID, "error", err)
		return
	}

	var needsSync []string
	for _, song := range songs {
		// info.ArrangementID == "" counts as needing a (re-)fetch too, not
		// just a missing row - same reasoning as PublishSet's own version
		// of this check: a song cached before ArrangementID/KeyID existed
		// as columns has a row that's present but permanently blank on
		// both, so "already cached" alone would never actually correct
		// it - this sync is the one place meant to fix exactly that.
		if info, ok := cache[song.ID]; ok && info.ArrangementID != "" {
			continue
		}
		needsSync = append(needsSync, song.ID)
	}

	batches := songSyncBatches(needsSync, songInfoSyncBatchSize)
	// applied/failed are written from every batch's concurrent goroutines
	// (via scheduling.RunConcurrently) - atomic rather than plain ints
	// since multiple songs in the same batch finish at once.
	var applied, failed atomic.Int64
	runPacedBatches(batches, songInfoSyncBatchPause, func(songID string) {
		if _, err := fetchAndCacheArrangementInfo(ctx, store, organizationID, songID); err != nil {
			logger.Error("background song info sync: caching arrangement failed", "organization_id", organizationID, "song_id", songID, "error", err)
			failed.Add(1)
			return
		}
		applied.Add(1)
	})

	logger.Info("background song info sync complete",
		"organization_id", organizationID,
		"total_songs", len(songs),
		"newly_cached", applied.Load(),
		"failed", failed.Load(),
	)
}
