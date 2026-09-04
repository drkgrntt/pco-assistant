package controllers

import (
	"reflect"
	"testing"
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
	delete(inProgress, "org-1") // what runSongInfoSync's deferred cleanup does

	if !songSyncTryStart(inProgress, "org-1") {
		t.Error("songSyncTryStart after the previous sync finished (cleared) = false, want true")
	}
}
