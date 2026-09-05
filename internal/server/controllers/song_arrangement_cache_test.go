package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// --- formatSongStructure ---------------------------------------------------

func TestFormatSongStructurePrefersSequenceShort(t *testing.T) {
	got := formatSongStructure([]string{"Intro", "V1", "C"}, []string{"Intro", "Verse 1", "Chorus"})

	want := "Intro, V1, C"
	if got != want {
		t.Errorf("formatSongStructure = %q, want %q (SequenceShort should be used directly when present)", got, want)
	}
}

func TestFormatSongStructureFallsBackToSequence(t *testing.T) {
	got := formatSongStructure(nil, []string{"Intro", "Verse 1", "Chorus"})

	want := "Intro, Verse 1, Chorus"
	if got != want {
		t.Errorf("formatSongStructure = %q, want %q (empty SequenceShort should fall back to Sequence)", got, want)
	}
}

func TestFormatSongStructureBothEmpty(t *testing.T) {
	got := formatSongStructure(nil, nil)

	if got != "" {
		t.Errorf("formatSongStructure(nil, nil) = %q, want \"\"", got)
	}
}

// --- keysFromIncluded -------------------------------------------------

// TestKeysFromIncludedParsesKeyEntries mirrors
// TestTeamNamesFromIncludedParsesTeamEntries/
// TestPlanSortDatesFromIncludedParsesPlanEntries (rosters_builder_test.go)
// - a non-Key entry in Included (an Arrangement, here - the resource this
// response's Data actually carries) must be silently skipped, not mistaken
// for a Key.
func TestKeysFromIncludedParsesKeyEntries(t *testing.T) {
	included := []any{
		map[string]any{
			"type": "Key",
			"id":   "key-1",
			"attributes": map[string]any{
				"starting_key": "G",
			},
		},
		map[string]any{
			"type": "Arrangement",
			"id":   "arr-1",
		},
	}

	got := keysFromIncluded(included)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 key (the Arrangement entry should be skipped), got %+v", got)
	}
	if got[0].ID != "key-1" {
		t.Errorf("expected key id %q, got %q", "key-1", got[0].ID)
	}
	if got[0].Attributes.StartingKey != "G" {
		t.Errorf("expected starting key %q, got %q", "G", got[0].Attributes.StartingKey)
	}
}

// TestKeysFromIncludedSkipsEntriesMissingID confirms a malformed Key entry
// (no id) is dropped rather than surfaced as a usable-looking zero-value
// key - same "silently skip, don't fail the page" spirit as
// teamNamesFromIncluded/planSortDatesFromIncluded.
func TestKeysFromIncludedSkipsEntriesMissingID(t *testing.T) {
	included := []any{
		map[string]any{
			"type": "Key",
			"attributes": map[string]any{
				"starting_key": "G",
			},
		},
	}

	got := keysFromIncluded(included)

	if len(got) != 0 {
		t.Errorf("expected no keys (missing id should be skipped), got %+v", got)
	}
}

// TestKeysFromIncludedEmptyInput confirms an empty/nil Included array
// (no include=keys sideload, or an arrangement with no Key row at all)
// comes back as no keys rather than panicking.
func TestKeysFromIncludedEmptyInput(t *testing.T) {
	if got := keysFromIncluded(nil); len(got) != 0 {
		t.Errorf("keysFromIncluded(nil) = %+v, want empty", got)
	}
}

// --- songSyncBatches ------------------------------------------------------

func TestSongSyncBatchesSplitsEvenly(t *testing.T) {
	songIDs := []string{"1", "2", "3", "4", "5", "6"}

	got := songSyncBatches(songIDs, 2)

	want := [][]string{{"1", "2"}, {"3", "4"}, {"5", "6"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("songSyncBatches(%v, 2) = %v, want %v", songIDs, got, want)
	}
}

func TestSongSyncBatchesLastBatchSmaller(t *testing.T) {
	songIDs := []string{"1", "2", "3", "4", "5"}

	got := songSyncBatches(songIDs, 2)

	want := [][]string{{"1", "2"}, {"3", "4"}, {"5"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("songSyncBatches(%v, 2) = %v, want %v", songIDs, got, want)
	}
}

func TestSongSyncBatchesFewerSongsThanBatchSize(t *testing.T) {
	songIDs := []string{"1", "2"}

	got := songSyncBatches(songIDs, 10)

	want := [][]string{{"1", "2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("songSyncBatches(%v, 10) = %v, want %v", songIDs, got, want)
	}
}

func TestSongSyncBatchesExactMultiple(t *testing.T) {
	songIDs := []string{"1", "2", "3", "4"}

	got := songSyncBatches(songIDs, 2)

	if len(got) != 2 {
		t.Fatalf("songSyncBatches(%v, 2) produced %d batches, want 2", songIDs, len(got))
	}
	for _, batch := range got {
		if len(batch) != 2 {
			t.Errorf("batch %v has %d songs, want 2 (no leftover short batch expected)", batch, len(batch))
		}
	}
}

func TestSongSyncBatchesEmptyInput(t *testing.T) {
	got := songSyncBatches(nil, 10)
	if got != nil {
		t.Errorf("songSyncBatches(nil, 10) = %v, want nil", got)
	}
}

func TestSongSyncBatchesZeroSizeFallsBackToOneBatch(t *testing.T) {
	songIDs := []string{"1", "2", "3"}

	got := songSyncBatches(songIDs, 0)

	want := [][]string{{"1", "2", "3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("songSyncBatches(%v, 0) = %v, want %v (a non-positive size shouldn't panic or loop forever)", songIDs, got, want)
	}
}

func TestSongSyncBatchesRealBatchSizeCallCount(t *testing.T) {
	// A 237-song library (bigger than any real church's, but plausible as
	// a sanity check) at the real songInfoSyncBatchSize should need
	// ceil(237/10) = 24 batches - the number of pauses (batches-1) is what
	// determines a big sync's wall-clock time, see
	// songInfoSyncBatchPause's doc comment.
	songIDs := make([]string, 237)
	for i := range songIDs {
		songIDs[i] = "song"
	}

	got := songSyncBatches(songIDs, songInfoSyncBatchSize)

	wantBatches := 24
	if len(got) != wantBatches {
		t.Fatalf("songSyncBatches(237 songs, %d) produced %d batches, want %d", songInfoSyncBatchSize, len(got), wantBatches)
	}
	if len(got[len(got)-1]) != 7 {
		t.Errorf("last batch has %d songs, want 7 (237 %% %d)", len(got[len(got)-1]), songInfoSyncBatchSize)
	}
}

// --- songSyncTryStart ------------------------------------------------------

func TestSongSyncTryStartFirstCallSucceeds(t *testing.T) {
	inProgress := map[string]bool{}

	if !songSyncTryStart(inProgress, "org-1") {
		t.Error("songSyncTryStart on an org with nothing running = false, want true")
	}
	if !inProgress["org-1"] {
		t.Error("songSyncTryStart should have marked org-1 as running")
	}
}

func TestSongSyncTryStartSecondCallFails(t *testing.T) {
	inProgress := map[string]bool{}

	if !songSyncTryStart(inProgress, "org-1") {
		t.Fatal("first songSyncTryStart call should succeed")
	}
	if songSyncTryStart(inProgress, "org-1") {
		t.Error("second songSyncTryStart while org-1 is already running = true, want false")
	}
}

func TestSongSyncTryStartIndependentPerOrg(t *testing.T) {
	inProgress := map[string]bool{}

	if !songSyncTryStart(inProgress, "org-1") {
		t.Fatal("songSyncTryStart for org-1 should succeed")
	}
	if !songSyncTryStart(inProgress, "org-2") {
		t.Error("songSyncTryStart for org-2 while only org-1 is running = false, want true (orgs are independent)")
	}
}

func TestSongSyncTryStartAfterClearingCanStartAgain(t *testing.T) {
	inProgress := map[string]bool{}

	if !songSyncTryStart(inProgress, "org-1") {
		t.Fatal("first songSyncTryStart call should succeed")
	}
	delete(inProgress, "org-1") // what runSongInfoQueueSync's deferred cleanup does

	if !songSyncTryStart(inProgress, "org-1") {
		t.Error("songSyncTryStart after the previous sync finished (cleared) = false, want true")
	}
}

// --- enqueueSongSync / songQueuePending -------------------------------------
// enqueueSongSync and triggerSongInfoQueueSync share package-level state
// (songSyncMu/songQueuePending/songSyncInProgress) with each other and with
// resolveArrangementCacheInfo's own tests below - every test here uses a
// unique organizationID/songID and cleans up after itself so they can't
// interfere with each other regardless of test execution order.

func TestEnqueueSongSyncDedupsRepeatedRequest(t *testing.T) {
	req := songSyncRequest{organizationID: "enqueue-dedup-org", songID: "song-1"}
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req)
		songSyncMu.Unlock()
	})

	enqueueSongSync(req)
	enqueueSongSync(req)

	songSyncMu.Lock()
	count := 0
	for r := range songQueuePending {
		if r == req {
			count++
		}
	}
	songSyncMu.Unlock()

	if count != 1 {
		t.Errorf("songQueuePending has %d entries for %+v after enqueuing it twice, want 1 (a map key naturally dedups)", count, req)
	}
}

func TestEnqueueSongSyncTracksMultipleSongsIndependently(t *testing.T) {
	req1 := songSyncRequest{organizationID: "enqueue-multi-org", songID: "song-1"}
	req2 := songSyncRequest{organizationID: "enqueue-multi-org", songID: "song-2"}
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req1)
		delete(songQueuePending, req2)
		songSyncMu.Unlock()
	})

	enqueueSongSync(req1)
	enqueueSongSync(req2)

	songSyncMu.Lock()
	got1, got2 := songQueuePending[req1], songQueuePending[req2]
	songSyncMu.Unlock()

	if !got1 || !got2 {
		t.Errorf("songQueuePending[%+v]=%v, songQueuePending[%+v]=%v, want both true", req1, got1, req2, got2)
	}
}

// --- triggerSongInfoQueueSync ------------------------------------------------
// The check-in-progress-then-drain-pending sequence, mirroring
// person_data_cache_test.go's coverage of triggerPersonDataSync's identical
// shape. Each test passes store=nil: every branch exercised here returns
// before ever dereferencing it (the no-token check, the in-progress guard,
// and the nothing-pending drain all return early) - see
// TestTriggerSongInfoQueueSyncDrainsPendingAndStartsBackgroundSync below for
// the one path that actually needs a real (fake) PCO server.

func TestTriggerSongInfoQueueSyncNoOpWithoutAccessToken(t *testing.T) {
	const orgID = "trigger-no-token-org"
	req := songSyncRequest{organizationID: orgID, songID: "song-1"}
	enqueueSongSync(req)
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req)
		delete(songSyncInProgress, orgID)
		songSyncMu.Unlock()
	})

	// context.Background() carries no access token - triggerSongInfoQueueSync
	// should treat this exactly like every other best-effort PCO pattern in
	// this codebase and skip entirely, per its own doc comment.
	triggerSongInfoQueueSync(context.Background(), nil, orgID)

	songSyncMu.Lock()
	stillPending := songQueuePending[req]
	inProgress := songSyncInProgress[orgID]
	songSyncMu.Unlock()

	if !stillPending {
		t.Error("triggerSongInfoQueueSync with no access token in ctx should leave the request pending (no-op), but it was drained")
	}
	if inProgress {
		t.Error("triggerSongInfoQueueSync with no access token in ctx should never mark the org in-progress")
	}
}

func TestTriggerSongInfoQueueSyncSkipsWhenAlreadyInProgress(t *testing.T) {
	const orgID = "trigger-already-running-org"
	req := songSyncRequest{organizationID: orgID, songID: "song-1"}
	enqueueSongSync(req)

	songSyncMu.Lock()
	songSyncInProgress[orgID] = true
	songSyncMu.Unlock()

	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req)
		delete(songSyncInProgress, orgID)
		songSyncMu.Unlock()
	})

	ctx := pco.WithAccessToken(context.Background(), "fake-token")
	triggerSongInfoQueueSync(ctx, nil, orgID)

	songSyncMu.Lock()
	stillPending := songQueuePending[req]
	songSyncMu.Unlock()

	if !stillPending {
		t.Error("triggerSongInfoQueueSync should not drain the queue while a pass is already in progress for this org - a second pass would double PCO call volume for no benefit")
	}
}

func TestTriggerSongInfoQueueSyncNoOpWithNothingPending(t *testing.T) {
	const orgID = "trigger-nothing-pending-org"
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songSyncInProgress, orgID)
		songSyncMu.Unlock()
	})

	ctx := pco.WithAccessToken(context.Background(), "fake-token")
	triggerSongInfoQueueSync(ctx, nil, orgID)

	songSyncMu.Lock()
	inProgress := songSyncInProgress[orgID]
	songSyncMu.Unlock()

	if inProgress {
		t.Error("triggerSongInfoQueueSync with nothing pending should release the in-progress guard immediately, not leave it held (that would block a genuine future request from ever starting a pass)")
	}
}

// TestTriggerSongInfoQueueSyncDrainsPendingAndStartsBackgroundSync proves
// the one path the tests above don't reach: given a real access token and
// something actually pending, triggerSongInfoQueueSync drains
// songQueuePending synchronously (before returning) and hands the drained
// song IDs to a background runSongInfoQueueSync pass, which clears
// songSyncInProgress again once it finishes. The fake PCO server replies
// with no arrangement data, so fetchAndCacheArrangementInfo's per-song call
// fails fast without ever touching a (nil, here) store - proving the drain
// and in-progress bookkeeping work without needing a real database.
func TestTriggerSongInfoQueueSyncDrainsPendingAndStartsBackgroundSync(t *testing.T) {
	const orgID = "trigger-drain-org"
	req := songSyncRequest{organizationID: orgID, songID: "song-1"}
	enqueueSongSync(req)
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req)
		delete(songSyncInProgress, orgID)
		songSyncMu.Unlock()
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()
	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	ctx := pco.WithAccessToken(context.Background(), "fake-token")
	triggerSongInfoQueueSync(ctx, nil, orgID)

	// The drain itself is synchronous (happens before triggerSongInfoQueueSync
	// returns), so this can be asserted immediately.
	songSyncMu.Lock()
	stillPending := songQueuePending[req]
	songSyncMu.Unlock()
	if stillPending {
		t.Fatal("triggerSongInfoQueueSync should have drained the pending request synchronously before returning")
	}

	// The background sync itself runs in a goroutine - poll for
	// songSyncInProgress to clear (its deferred cleanup) rather than
	// asserting immediately, with a generous deadline since this is CI-timing
	// sensitive; a single-song sync has no inter-batch pause to wait out (see
	// runPacedBatches), so in practice this clears almost immediately.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		songSyncMu.Lock()
		inProgress := songSyncInProgress[orgID]
		songSyncMu.Unlock()
		if !inProgress {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("songSyncInProgress was never cleared - the background sync's deferred cleanup did not run within the deadline")
}

// --- resolveArrangementCacheInfo ---------------------------------------------
// resolveArrangementCacheInfo lives in scheduling.go, not this file, but its
// tests live here since it shares songSyncMu/songQueuePending with
// enqueueSongSync/triggerSongInfoQueueSync above.

func TestResolveArrangementCacheInfoHitReturnsCachedValueWithoutEnqueuing(t *testing.T) {
	const orgID, songID = "resolve-hit-org", "resolve-hit-song"
	req := songSyncRequest{organizationID: orgID, songID: songID}

	want := scheduling.SongArrangementInfo{Key: "G", BPM: 120, Meter: "4/4"}
	cache := map[string]scheduling.SongArrangementInfo{songID: want}

	// ctx/store are nil/background here on purpose: a cache hit must never
	// touch either.
	got := resolveArrangementCacheInfo(context.Background(), nil, orgID, cache, songID)
	if got != want {
		t.Errorf("resolveArrangementCacheInfo on a cache hit = %+v, want %+v", got, want)
	}

	songSyncMu.Lock()
	pending := songQueuePending[req]
	songSyncMu.Unlock()
	if pending {
		t.Error("resolveArrangementCacheInfo on a cache hit should not enqueue a sync request")
	}
}

// TestResolveArrangementCacheInfoMissEnqueuesAndReturnsZeroValueImmediately
// is the fast, deterministic unit test the miss path never had before Phase
// 2 (docs/pco-call-efficiency-rollout-plan.md): previously this branch made
// a live (or mocked) PCO call, so testing it at all meant a fake server;
// now it's pure - enqueue and return - so this proves that with a nil store
// and a background context that would panic or hang if either were
// actually used.
func TestResolveArrangementCacheInfoMissEnqueuesAndReturnsZeroValueImmediately(t *testing.T) {
	const orgID, songID = "resolve-miss-org", "resolve-miss-song"
	req := songSyncRequest{organizationID: orgID, songID: songID}
	t.Cleanup(func() {
		songSyncMu.Lock()
		delete(songQueuePending, req)
		songSyncMu.Unlock()
	})

	cache := map[string]scheduling.SongArrangementInfo{} // no entry for songID: a miss

	got := resolveArrangementCacheInfo(context.Background(), nil, orgID, cache, songID)

	if got != (scheduling.SongArrangementInfo{}) {
		t.Errorf("resolveArrangementCacheInfo on a cache miss = %+v, want the zero value (a miss must never block on a live PCO call any more)", got)
	}

	songSyncMu.Lock()
	pending := songQueuePending[req]
	songSyncMu.Unlock()
	if !pending {
		t.Error("resolveArrangementCacheInfo on a cache miss should enqueue a songSyncRequest, but none was found in songQueuePending")
	}
}
