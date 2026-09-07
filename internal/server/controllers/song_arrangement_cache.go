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

// --- Song Info (arrangement cache queue-on-miss sync) -----------------------
//
// The set builder's own read-through cache (resolveArrangementCacheInfo, in
// scheduling.go) used to backfill song_arrangement_cache inline, blocking
// the render on a live PCO fetch whenever a song wasn't cached yet - plus a
// standalone /scheduling/song-info page with a "Sync now" button for
// warming a whole library at once. Both are gone now: a cache miss enqueues
// a songSyncRequest (enqueueSongSync) and returns a zero-value info
// immediately instead of blocking, and buildSetBuilderData/
// buildSetBuilderDataWithErr (sets_builder.go) call triggerSongInfoQueueSync
// right before they return, which drains whatever's pending for this org
// into a paced background sync - mirroring person_data_cache.go's
// personSyncPending/triggerPersonDataSync architecture exactly (see that
// file's own header comment). A song gets its info the first time any set
// builder render actually needs it, with no button or nag anywhere in the
// UI - see docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2 for the full
// rationale this replaced.
//
// songSyncInProgress (below) still guards against a second concurrent pass
// for the same org - now guarding the queue-drain pass instead of a
// whole-library one, but the same reasoning applies: a pass already running
// hasn't necessarily reached every pending song yet, so a second one would
// just double PCO call volume for no benefit.

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

// songSyncRequest is one pending (organizationID, songID) sync need,
// deduplicated in songQueuePending by being used as a map key (both fields
// are plain strings, so it's comparable) - Song Info's counterpart to
// personSyncRequest (person_data_cache.go), simpler since there's only one
// kind of Song Info sync (arrangement + key), unlike that struct's three
// kinds.
type songSyncRequest struct {
	organizationID string
	songID         string
}

// songQueuePending records song sync requests still needing a pass,
// guarded by the *same* songSyncMu that guards songSyncInProgress above -
// not a second mutex, so triggerSongInfoQueueSync's check-in-progress-then-
// drain-pending sequence (below) happens atomically under one lock, the
// same way triggerPersonDataSync's does (person_data_cache.go).
var songQueuePending = map[songSyncRequest]bool{}

// enqueueSongSync records that req still needs a sync pass - called from
// resolveArrangementCacheInfo's cache-miss path (scheduling.go) instead of
// blocking that render on a live PCO call. Safe to call repeatedly for the
// same request before it's picked up (the map dedupes) and safe to call
// while a sync for this organization is already running (it'll just be
// picked up by the next pass) - mirrors enqueuePersonSync exactly
// (person_data_cache.go).
func enqueueSongSync(req songSyncRequest) {
	songSyncMu.Lock()
	songQueuePending[req] = true
	songSyncMu.Unlock()
}

// songInfoSyncBatchSize batches songs needing a sync into groups of this
// many, run concurrently via scheduling.RunConcurrently, with a pause
// (songInfoSyncBatchPause) between batches - see runSongInfoQueueSync for
// why batching-with-pauses, not one big concurrent fan-out, is the right
// shape here. Reuses scheduling.PCOFanOutLimit rather than a separate
// constant, for consistency with the batch size every other bounded PCO
// fan-out in this codebase already uses.
const songInfoSyncBatchSize = scheduling.PCOFanOutLimit

// songInfoSyncBatchPause is the pause between song-info sync batches.
// Each song needing a sync costs 2 PCO calls - one GetArrangements call
// (arrangement + key, the key sideloaded via include=keys since go-pco
// v0.21.0's Phase 1b fold, not a second call of its own) plus one
// GetAttachments call added by fetchAndCacheSongAttachments (scheduling.go,
// see docs/plans/shipped/song-lead-sheet-attachments-plan.md) - so one
// songInfoSyncBatchSize-song batch (10 songs) costs up to 20 calls, fired
// near-simultaneously since the batch itself runs concurrently - fast
// enough that, for this pacing calculation, a batch's own PCO calls all
// effectively land in the same instant relative to a 20-second window.
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
// 100-song backlog (10 batches) takes on the order of 10 * 10s = 100
// seconds to drain. That's an acceptable trade now that the sync runs in
// the background instead of blocking an HTTP response - see
// triggerSongInfoQueueSync.
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

// triggerSongInfoQueueSync starts a background sync pass for whatever's
// currently queued in songQueuePending for organizationID, if anything is
// pending and no pass is already running for it - mirrors
// triggerPersonDataSync's check-in-progress-then-drain-pending sequence
// exactly (person_data_cache.go). RefreshSetSongArrangementInfo (below)
// doesn't go through this guard at all - it's a synchronous, deliberate
// single-song user action, not a queue drain - so the two can never
// conflict regardless.
//
// Called from buildSetBuilderData/buildSetBuilderDataWithErr themselves
// (sets_builder.go), right before they return, rather than from each of
// their ~20-plus call sites across build.go/sets_detail.go/sets_publish.go/
// sets_reorder.go/run_sheet.go/song_arrangement_cache.go - seeded from
// docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2 "trigger mechanism"
// decision: person_data_cache.go's per-handler-call convention works for
// its own ~6 well-known roster handlers, but copying that here would mean
// ~20 mechanical call sites today and a new one to remember on every future
// addition. Calling it once, inside the shared builder function itself,
// covers every caller (present and future) for free.
//
// ctx is the same context.Context buildSetBuilderData was itself called
// with (middleware.RequestContext(c) at each of its call sites) - the
// access token and request id this function needs to build an
// independently-lived background context are pulled back out of it via
// pco.AccessTokenFromContext/logger.RequestIDFromContext instead of being
// threaded through as separate parameters, so none of those ~20 call sites
// need to change to also pass a token/request id explicitly. A ctx built
// without a token (never happens for a real signed-in request today, but
// keeps this safe for a future non-interactive caller, e.g. a Phase 3
// cron) makes this a no-op - matches every other best-effort PCO pattern
// in this codebase.
func triggerSongInfoQueueSync(ctx context.Context, store *scheduling.Store, organizationID string) {
	if organizationID == "" {
		return
	}
	accessToken, ok := pco.AccessTokenFromContext(ctx)
	if !ok {
		return
	}
	requestID, _ := logger.RequestIDFromContext(ctx)

	songSyncMu.Lock()
	if !songSyncTryStart(songSyncInProgress, organizationID) {
		songSyncMu.Unlock()
		return
	}
	var songIDs []string
	for req := range songQueuePending {
		if req.organizationID == organizationID {
			songIDs = append(songIDs, req.songID)
			delete(songQueuePending, req)
		}
	}
	if len(songIDs) == 0 {
		// Nothing actually pending for this org - release the in-progress
		// guard immediately rather than leaving it held with nothing to do,
		// which would block a genuine future request from ever starting a
		// pass (same reasoning as triggerPersonDataSync's identical check).
		delete(songSyncInProgress, organizationID)
		songSyncMu.Unlock()
		return
	}
	songSyncMu.Unlock()

	// accessToken, not ctx: this goroutine outlives the request - ctx's own
	// context is cancelled the moment buildSetBuilderData's caller's handler
	// returns, so a fresh context.Background()-derived context carrying the
	// same token is built instead - logger.WithRequestID carries this
	// request's id along the same way, so runSongInfoQueueSync's own
	// logging can still be traced back to whichever request triggered it.
	// Priority 20: an unattended background sync should never delay this
	// same token's interactive traffic if go-pco's throttle is enabled
	// (PCO_THROTTLE_ENABLED - off by default, see go-pco's README) - see
	// docs/pco-api-risks.md's throttle-adoption addendum.
	syncCtx := pco.WithPriority(pco.WithAccessToken(logger.WithRequestID(context.Background(), requestID), accessToken), 20)
	go runSongInfoQueueSync(syncCtx, store, organizationID, songIDs)
}

// runSongInfoQueueSync is triggerSongInfoQueueSync's actual work, run in a
// background goroutine outside the request/response cycle - the queue-drain
// counterpart to runPersonDataSync (person_data_cache.go), which this
// mirrors exactly: songIDs is split into songInfoSyncBatchSize batches,
// each fanned out concurrently via runPacedBatches with a
// songInfoSyncBatchPause pause between batches, trading wall-clock time for
// staying comfortably under PCO's rate limit regardless of how many songs
// are pending - see that constant's doc comment for the call-volume-per-
// 20-seconds math. Same best-effort error handling as every other sync in
// this codebase: one song's failure is logged and skipped, never aborting
// the batch or the rest of the pass.
func runSongInfoQueueSync(ctx context.Context, store *scheduling.Store, organizationID string, songIDs []string) {
	defer func() {
		songSyncMu.Lock()
		delete(songSyncInProgress, organizationID)
		songSyncMu.Unlock()
	}()

	batches := songSyncBatches(songIDs, songInfoSyncBatchSize)
	// applied/failed are written from every batch's concurrent goroutines
	// (via scheduling.RunConcurrently) - atomic rather than plain ints
	// since multiple songs in the same batch finish at once.
	var applied, failed atomic.Int64
	runPacedBatches(batches, songInfoSyncBatchPause, func(songID string) {
		if _, err := fetchAndCacheArrangementInfo(ctx, store, organizationID, songID); err != nil {
			logger.ErrorContext(ctx, "background song info sync: caching arrangement failed", "organization_id", organizationID, "song_id", songID, "error", err)
			failed.Add(1)
			return
		}
		applied.Add(1)
	})

	logger.InfoContext(ctx, "background song info sync complete",
		"organization_id", organizationID,
		"requested", len(songIDs),
		"newly_cached", applied.Load(),
		"failed", failed.Load(),
	)
}

// RefreshSetSongArrangementInfo force-refreshes one song's cached
// arrangement info (key/BPM/meter/length) from Planning Center right now,
// synchronously, overwriting whatever's currently cached for it - the
// single-song, always-live counterpart to the queue-on-miss sync above
// (triggerSongInfoQueueSync/runSongInfoQueueSync), which only fills gaps
// and never re-fetches an already-cached song. This is for the case where
// a song's arrangement was actually edited in Planning Center after this
// app cached it (key changed, tempo retuned, ...) and the cached copy is
// now stale, with no way to tell that apart from "never synced" short of
// asking Planning Center again. fetchAndCacheArrangementInfo itself has no
// "skip if already cached" guard (confirmed by reading it - it's a plain
// unconditional fetch-and-upsert), so no separate force variant is needed
// here, unlike a hypothetical "skip-if-cached" helper would require.
//
// A single live PCO call here is fine - this is a small, deliberate,
// user-triggered action bounded to one song (the Build page's song detail
// card - see songDetailCard, song_detail_card.templ), not a hot render
// path, same reasoning as the roster builder's on-demand candidate-
// availability check (docs/pco-api-risks.md's manual-refresh addendum).
func (sc *SchedulingController) RefreshSetSongArrangementInfo(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	rowID, err := c.ParamsInt("set_song_id")
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, "set_song_id must be an integer")))
	}

	errMsg := ""
	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		errMsg = err.Error()
	} else if setSongs, err := sc.store.ListSetSongs(ctx, set.ID); err != nil {
		errMsg = err.Error()
	} else {
		found := false
		for _, ss := range setSongs {
			if ss.ID == int64(rowID) {
				found = true
				if _, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, ss.SongID); err != nil {
					errMsg = "refreshing song info from Planning Center: " + err.Error()
				}
				break
			}
		}
		if !found {
			errMsg = appendErr(errMsg, "song is no longer in this set")
		}
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	song, ok := findSongViewByRowID(data, int64(rowID))
	if !ok {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	return render(c, views.SongArrangementInfoRefreshFragment(data, song, errMsg))
}

// runPacedBatches runs fn once for every item across batches, one batch at
// a time: each batch's items run concurrently via scheduling.RunConcurrently
// (bounded to scheduling.PCOFanOutLimit at once), with a pause of
// batchPause between the end of one batch and the start of the next - see
// songInfoSyncBatchPause's doc comment for the call-volume-per-20-seconds
// math this pacing is built around. Kept as its own function, independent
// of the DB/PCO-backed work runSongInfoQueueSync's fn actually does, so the
// pacing mechanism itself (batch size, concurrency bound, inter-batch
// pause) is testable on its own with a lightweight fn and no database/PCO
// dependency required - see TestRunPacedBatchesWaitsBetweenBatches in
// song_arrangement_cache_test.go, the one test in this task the user most
// needed confidence in. Shared with runPersonDataSync (person_data_cache.go)
// - the exact same pacing shape, just over a different item type.
func runPacedBatches[T any](batches [][]T, batchPause time.Duration, fn func(T)) {
	for i, batch := range batches {
		scheduling.RunConcurrently(batch, fn)

		if i < len(batches)-1 {
			time.Sleep(batchPause)
		}
	}
}
