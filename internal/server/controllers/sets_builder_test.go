package controllers

import (
	"context"
	"testing"
	"time"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func foreignItem(id, title, itemType string) pco.ItemData {
	return pco.ItemData{ID: id, Attributes: pco.ItemAttributes{Title: title, ItemType: itemType}}
}

func songItem(id, songID, title string) pco.ItemData {
	item := pco.ItemData{ID: id, Attributes: pco.ItemAttributes{Title: title, ItemType: pco.ItemTypeSong}}
	item.Relationships.Song.Data = &pco.General{Type: "Song", ID: songID}
	return item
}

// songItemKeyed is songItem plus a live KeyName, for exercising
// deriveImportRows/deriveMergeImportRows' capture of a plan item's own
// current key - see TestDeriveImportRowsCapturesLiveKey.
func songItemKeyed(id, songID, title, key string) pco.ItemData {
	item := songItem(id, songID, title)
	item.Attributes.KeyName = key
	return item
}

func setSong(songID, anchor string, position int) scheduling.SetSong {
	return scheduling.SetSong{SongID: songID, AnchorItemID: anchor, Position: position}
}

// publishedSetSong is setSong for a song that's already been pushed to
// Planning Center as pcoItemID - mergeServiceOrder's duplicate-skip keys
// off this, not song id, since a song can now appear more than once (only
// one occurrence "owns" a given real PCO item).
func publishedSetSong(songID, anchor string, position int, pcoItemID string) scheduling.SetSong {
	ss := setSong(songID, anchor, position)
	ss.PCOItemID = pcoItemID
	return ss
}

// rowLabel returns a foreign item's title or a song's id, for compact
// assertions against mergeServiceOrder's output order.
func rowLabel(row mergedRow) string {
	if row.Item != nil {
		return row.Item.Attributes.Title
	}
	return row.Song.SongID
}

func TestMergeServiceOrderTopOfServiceFirst(t *testing.T) {
	items := []pco.ItemData{foreignItem("i1", "Welcome", pco.ItemTypeHeader)}
	songs := []scheduling.SetSong{setSong("s1", "", 0)}

	got := mergeServiceOrder(items, songs)

	want := []string{"s1", "Welcome"}
	assertRowOrder(t, got, want)
}

func TestMergeServiceOrderAnchoredAfterForeignItem(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		foreignItem("i2", "Sermon", pco.ItemTypeHeader),
		foreignItem("i3", "Response Time", pco.ItemTypeHeader),
	}
	songs := []scheduling.SetSong{
		setSong("opener", "", 0),     // top of service
		setSong("s2", "i1", 0),       // right after Welcome
		setSong("response", "i3", 0), // right after Response Time
	}

	got := mergeServiceOrder(items, songs)

	want := []string{"opener", "Welcome", "s2", "Sermon", "Response Time", "response"}
	assertRowOrder(t, got, want)
}

func TestMergeServiceOrderGroupOrderedByPosition(t *testing.T) {
	items := []pco.ItemData{foreignItem("i1", "Worship", pco.ItemTypeHeader)}
	songs := []scheduling.SetSong{
		setSong("second", "i1", 1),
		setSong("first", "i1", 0),
	}

	got := mergeServiceOrder(items, songs)

	want := []string{"Worship", "first", "second"}
	assertRowOrder(t, got, want)
}

func TestMergeServiceOrderSkipsItemsForTrackedSongs(t *testing.T) {
	// A plan item that's already one of our tracked songs (already
	// published, linked by pco_item_id) shouldn't render twice - once as
	// its own song row.
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Some Song"),
	}
	songs := []scheduling.SetSong{publishedSetSong("song-a", "i1", 0, "i2")}

	got := mergeServiceOrder(items, songs)

	want := []string{"Welcome", "song-a"}
	assertRowOrder(t, got, want)
}

// TestMergeServiceOrderDuplicateSongOnlySkipsItsOwnItem confirms the
// duplicate-skip is keyed by PCO item id, not song id: a second, unrelated
// PCO item that happens to link to the same song (e.g. a human added it
// directly in Planning Center) must still show up as a genuine foreign
// row, not be hidden just because some *other* item linked to that song is
// one we track.
func TestMergeServiceOrderDuplicateSongOnlySkipsItsOwnItem(t *testing.T) {
	items := []pco.ItemData{
		songItem("i1", "song-a", "Some Song"), // ours, tracked
		songItem("i2", "song-a", "Some Song"), // same song, but NOT ours
	}
	songs := []scheduling.SetSong{publishedSetSong("song-a", "", 0, "i1")}

	got := mergeServiceOrder(items, songs)

	want := []string{"song-a", "Some Song"}
	assertRowOrder(t, got, want)
	if got[1].Item == nil || got[1].Item.ID != "i2" {
		t.Errorf("expected the second row to be the untracked foreign item i2, got %+v", got[1])
	}
}

func TestMergeServiceOrderStaleAnchorAppendsAtEnd(t *testing.T) {
	items := []pco.ItemData{foreignItem("i1", "Welcome", pco.ItemTypeHeader)}
	songs := []scheduling.SetSong{
		setSong("current", "i1", 0),
		setSong("orphaned", "i-deleted", 0), // anchor no longer exists in items
	}

	got := mergeServiceOrder(items, songs)

	want := []string{"Welcome", "current", "orphaned"}
	assertRowOrder(t, got, want)
}

func TestDeriveImportRows(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Song A"),
		songItem("i3", "song-b", "Song B"),
		foreignItem("i4", "Sermon", pco.ItemTypeHeader),
		songItem("i5", "song-c", "Song C"),
	}

	got := deriveImportRows(items)

	want := []scheduling.SetSong{
		{SongID: "song-a", Position: 0, PCOItemID: "i2", AnchorItemID: "i1"},
		{SongID: "song-b", Position: 1, PCOItemID: "i3", AnchorItemID: "i1"},
		{SongID: "song-c", Position: 2, PCOItemID: "i5", AnchorItemID: "i4"},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i, row := range got {
		if row != want[i] {
			t.Errorf("row %d: expected %+v, got %+v", i, want[i], row)
		}
	}
}

// TestDeriveImportRowsThenMergeRoundTrips confirms a revert-then-display
// round trip reproduces the exact same order PCO actually has - the two
// functions are meant to be inverses of each other.
func TestDeriveImportRowsThenMergeRoundTrips(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Song A"),
		songItem("i3", "song-b", "Song B"),
		foreignItem("i4", "Sermon", pco.ItemTypeHeader),
		songItem("i5", "song-c", "Song C"),
	}

	imported := deriveImportRows(items)
	merged := mergeServiceOrder(items, imported)

	want := []string{"Welcome", "song-a", "song-b", "Sermon", "song-c"}
	assertRowOrder(t, merged, want)
}

// TestDeriveImportRowsCapturesLiveKey guards against the real bug this
// covers for: a plan with keys "B A A A" in Planning Center was showing up
// in the app as "B A Bb A" after a revert, because the imported row never
// captured the item's own live key at all - display fell back to
// song_arrangement_cache's single org-wide cached key for that song,
// which is only correct when the song has one arrangement. Every imported
// row's KeyOverride must come from its own item, not be left empty.
func TestDeriveImportRowsCapturesLiveKey(t *testing.T) {
	items := []pco.ItemData{
		songItemKeyed("i1", "song-a", "Song A", "B"),
		songItemKeyed("i2", "song-b", "Song B", "A"),
		songItemKeyed("i3", "song-b", "Song B", "A"),
		songItemKeyed("i4", "song-b", "Song B", "A"),
	}

	got := deriveImportRows(items)

	want := []string{"B", "A", "A", "A"}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i, row := range got {
		if row.KeyOverride != want[i] {
			t.Errorf("row %d: expected key %q, got %q", i, want[i], row.KeyOverride)
		}
	}
}

// TestDeriveMergeImportRowsSkipsTrackedInsertsNew is the core MergeSet
// scenario: a plan with a song already tracked locally (whether or not it's
// still one of the set's active rows - see the tombstoned variant below)
// plus one new song added directly in Planning Center. Only the new song
// should come back for import, anchored to the tracked item that precedes
// it - existing local rows (active or tombstoned pending changes) are never
// this function's concern; it only ever computes what's missing.
func TestDeriveMergeImportRowsSkipsTrackedInsertsNew(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Song A"), // already tracked (published or tombstoned)
		songItem("i3", "song-b", "Song B"), // new, added directly in PCO
	}
	tracked := map[string]bool{"i2": true}

	got := deriveMergeImportRows(items, tracked)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 new row, got %+v", got)
	}
	want := scheduling.SetSong{SongID: "song-b", PCOItemID: "i3", AnchorItemID: "i2"}
	if got[0] != want {
		t.Errorf("expected %+v, got %+v", want, got[0])
	}
}

// TestDeriveMergeImportRowsCapturesLiveKey is deriveMergeImportRows' half
// of TestDeriveImportRowsCapturesLiveKey - a merged-in row must carry its
// own item's live key too, not just deriveImportRows' full-revert path.
func TestDeriveMergeImportRowsCapturesLiveKey(t *testing.T) {
	items := []pco.ItemData{
		songItem("i1", "song-a", "Song A"), // already tracked
		songItemKeyed("i2", "song-b", "Song B", "A"),
	}
	tracked := map[string]bool{"i1": true}

	got := deriveMergeImportRows(items, tracked)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 new row, got %+v", got)
	}
	if got[0].KeyOverride != "A" {
		t.Errorf("expected key %q, got %q", "A", got[0].KeyOverride)
	}
}

// TestDeriveMergeImportRowsTombstonedItemStillCountsAsTracked confirms a
// PCO item id tracked via a locally tombstoned (pending-deletion) row - not
// just an active one - is still skipped: it was deliberately removed here,
// and re-importing it out from under that removal would silently undo a
// real local decision.
func TestDeriveMergeImportRowsTombstonedItemStillCountsAsTracked(t *testing.T) {
	items := []pco.ItemData{songItem("i1", "song-a", "Song A")}
	tracked := map[string]bool{"i1": true} // tombstoned locally, but still tracked

	got := deriveMergeImportRows(items, tracked)

	if len(got) != 0 {
		t.Errorf("expected a tombstoned item to be skipped, not re-imported, got %+v", got)
	}
}

// TestDeriveMergeImportRowsNothingNewReturnsEmpty confirms a plan where
// every song-linked item is already tracked locally yields nothing to
// import.
func TestDeriveMergeImportRowsNothingNewReturnsEmpty(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Song A"),
	}
	tracked := map[string]bool{"i2": true}

	got := deriveMergeImportRows(items, tracked)

	if len(got) != 0 {
		t.Errorf("expected nothing new to import, got %+v", got)
	}
}

// TestTrackedPCOItemIDsIncludesActiveAndTombstoned confirms
// trackedPCOItemIDs (shared by MergeSet and buildSetBuilderData's
// MergeAvailableCount) counts a set song's PCOItemID whether it's still
// active or already tombstoned as a pending deletion, and skips any row
// with no PCOItemID at all (never published, so there's no PCO item id to
// track).
func TestTrackedPCOItemIDsIncludesActiveAndTombstoned(t *testing.T) {
	setSongs := []scheduling.SetSong{
		publishedSetSong("song-a", "", 0, "i1"),
		setSong("song-b", "", 1), // unpublished - no PCOItemID yet
	}
	pendingDeletions := []scheduling.SetSong{
		publishedSetSong("song-c", "", 0, "i2"),
	}

	got := trackedPCOItemIDs(setSongs, pendingDeletions)

	want := map[string]bool{"i1": true, "i2": true}
	if len(got) != len(want) {
		t.Fatalf("trackedPCOItemIDs() = %+v, want %+v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("trackedPCOItemIDs() missing %q, got %+v", id, got)
		}
	}
}

// TestDeriveMergeImportRowsAnchorsToPrecedingForeignItem confirms a new
// song with nothing tracked before it anchors to the nearest foreign
// (header/non-song) item, same as deriveImportRows.
func TestDeriveMergeImportRowsAnchorsToPrecedingForeignItem(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("i1", "Welcome", pco.ItemTypeHeader),
		songItem("i2", "song-a", "Song A"),
	}

	got := deriveMergeImportRows(items, map[string]bool{})

	want := []scheduling.SetSong{{SongID: "song-a", PCOItemID: "i2", AnchorItemID: "i1"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// TestDeriveMergeImportRowsChainsAcrossConsecutiveNewSongs confirms two new
// songs in a row (nothing tracked or foreign between them) each anchor to
// whatever immediately precedes them in the walk - the second to the
// first's own PCO item id - so their relative order survives even though
// both are new.
func TestDeriveMergeImportRowsChainsAcrossConsecutiveNewSongs(t *testing.T) {
	items := []pco.ItemData{
		songItem("i1", "song-a", "Song A"),
		songItem("i2", "song-b", "Song B"),
	}

	got := deriveMergeImportRows(items, map[string]bool{})

	want := []scheduling.SetSong{
		{SongID: "song-a", PCOItemID: "i1", AnchorItemID: ""},
		{SongID: "song-b", PCOItemID: "i2", AnchorItemID: "i1"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}

// TestMergeServiceOrderPlacesImportedRowRightAfterTrackedAnchor is the
// rendering half of the MergeSet scenario above: a freshly-imported row
// anchored to an already-tracked item's own PCO item id must land right
// after that tracked item in the merged order, not get pushed to the very
// end as a stale anchor - the fix mergeServiceOrder needed once anchoring
// to a tracked item's id became possible (see its own doc comment).
func TestMergeServiceOrderPlacesImportedRowRightAfterTrackedAnchor(t *testing.T) {
	items := []pco.ItemData{
		songItem("i1", "song-a", "Song A"), // tracked, top of service
		songItem("i2", "song-b", "Song B"), // freshly imported, anchored to i1
		foreignItem("i3", "Sermon", pco.ItemTypeHeader),
	}
	songs := []scheduling.SetSong{
		publishedSetSong("song-a", "", 0, "i1"),
		publishedSetSong("song-b", "i1", 0, "i2"),
	}

	got := mergeServiceOrder(items, songs)

	want := []string{"song-a", "song-b", "Sermon"}
	assertRowOrder(t, got, want)
}

func TestSongsInBothOverlapOnly(t *testing.T) {
	a := []pco.ItemData{
		songItem("i1", "song-a", "Song A"),
		songItem("i2", "song-b", "Song B"),
	}
	b := []pco.ItemData{
		songItem("i3", "song-b", "Song B"),
		songItem("i4", "song-c", "Song C"),
	}

	got := songsInBoth(a, b)

	if len(got) != 1 || !got["song-b"] {
		t.Errorf("expected only song-b to be flagged as repeated, got %+v", got)
	}
}

func TestSongsInBothNoOverlap(t *testing.T) {
	a := []pco.ItemData{songItem("i1", "song-a", "Song A")}
	b := []pco.ItemData{songItem("i2", "song-b", "Song B")}

	got := songsInBoth(a, b)

	if len(got) != 0 {
		t.Errorf("expected no repeats, got %+v", got)
	}
}

// TestSongsInBothIgnoresNonSongItems confirms a header/media item (no
// relationships.song) never counts toward the overlap, even one that
// happens to share an id with something in the other plan.
func TestSongsInBothIgnoresNonSongItems(t *testing.T) {
	a := []pco.ItemData{foreignItem("i1", "Welcome", pco.ItemTypeHeader)}
	b := []pco.ItemData{foreignItem("i1", "Welcome", pco.ItemTypeHeader)}

	got := songsInBoth(a, b)

	if len(got) != 0 {
		t.Errorf("expected no repeats from non-song items, got %+v", got)
	}
}

// includedItemNote builds one "included" array entry shaped like a
// sideloaded ItemNote (type/id/attributes/relationships.item), for
// TestItemNotesFromIncluded - mirrors go-pco's real JSON:API shape for an
// ItemNote resource.
func includedItemNote(id, itemID, categoryID string) map[string]any {
	rels := map[string]any{}
	if itemID != "" {
		rels["item"] = map[string]any{
			"data": map[string]any{"type": "Item", "id": itemID},
		}
	}
	if categoryID != "" {
		rels["item_note_category"] = map[string]any{
			"data": map[string]any{"type": "ItemNoteCategory", "id": categoryID},
		}
	}
	return map[string]any{
		"type":          "ItemNote",
		"id":            id,
		"attributes":    map[string]any{"content": "some note"},
		"relationships": rels,
	}
}

func TestItemNotesFromIncluded(t *testing.T) {
	tests := []struct {
		name     string
		included []any
		want     map[string][]string // item id -> note ids, in order
	}{
		{
			name: "well-formed ItemNote entries are grouped by item id",
			included: []any{
				includedItemNote("n1", "item-1", "cat-1"),
				includedItemNote("n2", "item-1", "cat-2"),
				includedItemNote("n3", "item-2", "cat-1"),
			},
			want: map[string][]string{
				"item-1": {"n1", "n2"},
				"item-2": {"n3"},
			},
		},
		{
			name: "non-ItemNote entries are skipped",
			included: []any{
				includedItemNote("n1", "item-1", "cat-1"),
				map[string]any{"type": "Song", "id": "s1"},
			},
			want: map[string][]string{"item-1": {"n1"}},
		},
		{
			name: "an ItemNote with no item relationship is skipped",
			included: []any{
				includedItemNote("n1", "", "cat-1"),
			},
			want: map[string][]string{},
		},
		{
			name:     "empty included yields an empty map",
			included: []any{},
			want:     map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemNotesFromIncluded(tt.included)

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d items with notes, got %+v", len(tt.want), got)
			}
			for itemID, wantIDs := range tt.want {
				gotNotes, ok := got[itemID]
				if !ok {
					t.Fatalf("expected notes for item %q, got none", itemID)
				}
				if len(gotNotes) != len(wantIDs) {
					t.Fatalf("item %q: expected %d notes, got %d: %+v", itemID, len(wantIDs), len(gotNotes), gotNotes)
				}
				for i, wantID := range wantIDs {
					if gotNotes[i].ID != wantID {
						t.Errorf("item %q note %d: expected id %q, got %q", itemID, i, wantID, gotNotes[i].ID)
					}
				}
			}
		})
	}
}

func TestBuildSongNoteViews(t *testing.T) {
	categories := []pco.ItemNoteCategoryData{
		{ID: "cat-1", Attributes: pco.ItemNoteCategoryAttributes{Name: "Audio/Visual"}},
		{ID: "cat-2", Attributes: pco.ItemNoteCategoryAttributes{Name: "Band"}},
	}
	noteForCategory := func(id, categoryID, content string) pco.ItemNoteData {
		n := pco.ItemNoteData{ID: id, Attributes: pco.ItemNoteAttributes{Content: content}}
		n.Relationships.ItemNoteCategory.Data = &pco.General{Type: "ItemNoteCategory", ID: categoryID}
		return n
	}

	t.Run("unpublished song (empty itemID) returns nil without consulting itemNotes", func(t *testing.T) {
		itemNotes := map[string][]pco.ItemNoteData{"item-1": {noteForCategory("n1", "cat-1", "should be ignored")}}

		if got := buildSongNoteViews("", categories, itemNotes); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("no note categories returns nil", func(t *testing.T) {
		if got := buildSongNoteViews("item-1", nil, map[string][]pco.ItemNoteData{}); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("one row per category, populated from itemNotes when present", func(t *testing.T) {
		itemNotes := map[string][]pco.ItemNoteData{
			"item-1": {noteForCategory("n1", "cat-1", "Turn up the click")},
		}

		got := buildSongNoteViews("item-1", categories, itemNotes)

		if len(got) != 2 {
			t.Fatalf("expected 2 note views (one per category), got %+v", got)
		}
		if got[0].CategoryID != "cat-1" || got[0].NoteID != "n1" || got[0].Content != "Turn up the click" {
			t.Errorf("expected cat-1 populated from the existing note, got %+v", got[0])
		}
		if got[1].CategoryID != "cat-2" || got[1].NoteID != "" || got[1].Content != "" {
			t.Errorf("expected cat-2 empty (no note on this item), got %+v", got[1])
		}
	})

	t.Run("item with no entry in itemNotes gets every category empty", func(t *testing.T) {
		got := buildSongNoteViews("item-missing", categories, map[string][]pco.ItemNoteData{
			"item-1": {noteForCategory("n1", "cat-1", "not this item")},
		})

		if len(got) != 2 {
			t.Fatalf("expected 2 note views, got %+v", got)
		}
		for _, v := range got {
			if v.NoteID != "" || v.Content != "" {
				t.Errorf("expected empty note view for an item with no notes, got %+v", v)
			}
		}
	})
}

// fingerprintItem builds a bare pco.ItemData carrying just what
// pcoItemsFingerprint reads - id, Sequence, UpdatedAt - for
// TestPCOItemsFingerprint below.
func fingerprintItem(id string, sequence int, updatedAt time.Time) pco.ItemData {
	return pco.ItemData{ID: id, Attributes: pco.ItemAttributes{Sequence: sequence, UpdatedAt: updatedAt}}
}

// TestPCOItemsFingerprint covers pcoItemsFingerprint's contract (see its
// own doc comment): two fingerprints are equal iff the same set of item
// ids exist with the same Sequence and UpdatedAt values each. Each case
// compares against a fixed baseline (two items, i1 then i2) rather than
// comparing pairs directly, so "same"/"different" reads naturally per
// case.
func TestPCOItemsFingerprint(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	base := []pco.ItemData{
		fingerprintItem("i1", 0, t1),
		fingerprintItem("i2", 1, t1),
	}
	want := pcoItemsFingerprint(base)

	tests := []struct {
		name  string
		items []pco.ItemData
		same  bool
	}{
		{
			name:  "identical input produces an identical fingerprint",
			items: []pco.ItemData{fingerprintItem("i1", 0, t1), fingerprintItem("i2", 1, t1)},
			same:  true,
		},
		{
			name:  "item order in the slice passed in doesn't affect the result",
			items: []pco.ItemData{fingerprintItem("i2", 1, t1), fingerprintItem("i1", 0, t1)},
			same:  true,
		},
		{
			name:  "a title/description/key edit (different UpdatedAt, same Sequence) changes it",
			items: []pco.ItemData{fingerprintItem("i1", 0, t2), fingerprintItem("i2", 1, t1)},
			same:  false,
		},
		{
			name:  "a pure reorder (swapped Sequence, same UpdatedAt) changes it",
			items: []pco.ItemData{fingerprintItem("i1", 1, t1), fingerprintItem("i2", 0, t1)},
			same:  false,
		},
		{
			name:  "an added item changes it",
			items: []pco.ItemData{fingerprintItem("i1", 0, t1), fingerprintItem("i2", 1, t1), fingerprintItem("i3", 2, t1)},
			same:  false,
		},
		{
			name:  "a removed item changes it",
			items: []pco.ItemData{fingerprintItem("i1", 0, t1)},
			same:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pcoItemsFingerprint(tt.items)
			if (got == want) != tt.same {
				t.Errorf("expected same=%v against base fingerprint %q, got %q", tt.same, want, got)
			}
		})
	}
}

// renderServiceOrderRow renders rows through renderServiceOrder with a nil
// store (safe as long as arrangementCache already has an entry for every
// song row - see resolveArrangementCacheInfo's cache-hit path, which never
// touches store on a hit) and returns the one song row whose SongID matches
// - a small helper for the RecentlyRepeated/ConflictTitles/
// KeyTransitionLabel tests below, which only care about one row's own
// resulting SetSongView.
func renderServiceOrderRow(t *testing.T, rows []mergedRow, cache map[string]scheduling.SongArrangementInfo, titles map[string]string, repeated map[string]bool, conflictTitlesBySong map[string][]string, transitionsByRow map[int64]keyTransitionWarning, songID string) views.SetSongView {
	t.Helper()
	out := renderServiceOrder(context.Background(), nil, "org1", cache, nil, nil, nil, rows, titles, nil, nil, repeated, conflictTitlesBySong, transitionsByRow)
	for _, row := range out {
		if row.IsSong && row.Song.SongID == songID {
			return row.Song
		}
	}
	t.Fatalf("no rendered row found for song %q", songID)
	return views.SetSongView{}
}

// TestRenderServiceOrderPopulatesRecentlyRepeated confirms a set song whose
// id is in the repeated map (see fetchRecentPlanHistory.RepeatedSongs) gets
// RecentlyRepeated set on its rendered SetSongView - the same badge
// CandidateSongView.RecentlyRepeated already gets for a not-yet-added
// candidate, now also rendered inline on an already-in-the-set song's own
// row (see docs/plans/shipped/in-place-set-warnings-plan.md).
func TestRenderServiceOrderPopulatesRecentlyRepeated(t *testing.T) {
	a := setSong("song-a", "", 0)
	b := setSong("song-b", "", 1)
	rows := []mergedRow{{Song: &a}, {Song: &b}}
	cache := map[string]scheduling.SongArrangementInfo{}
	repeated := map[string]bool{"song-a": true}

	gotA := renderServiceOrderRow(t, rows, cache, map[string]string{}, repeated, map[string][]string{}, map[int64]keyTransitionWarning{}, "song-a")
	if !gotA.RecentlyRepeated {
		t.Error("expected song-a's RecentlyRepeated to be true")
	}

	gotB := renderServiceOrderRow(t, rows, cache, map[string]string{}, repeated, map[string][]string{}, map[int64]keyTransitionWarning{}, "song-b")
	if gotB.RecentlyRepeated {
		t.Error("expected song-b's RecentlyRepeated to be false")
	}
}

// TestRenderServiceOrderPopulatesConflictTitles confirms conflictTitlesBySong
// (built from Store.AvoidPairings' existing pairs - see buildSetBuilderData)
// attaches each conflict's *other* song title onto this song's own
// ConflictTitles, the same shape CandidateSongView.ConflictTitles already
// has for a not-yet-added candidate.
func TestRenderServiceOrderPopulatesConflictTitles(t *testing.T) {
	a := setSong("song-a", "", 0)
	b := setSong("song-b", "", 1)
	rows := []mergedRow{{Song: &a}, {Song: &b}}
	cache := map[string]scheduling.SongArrangementInfo{}
	titles := map[string]string{"song-a": "Song A", "song-b": "Song B"}
	conflictTitlesBySong := map[string][]string{
		"song-a": {"Song B"},
		"song-b": {"Song A"},
	}

	gotA := renderServiceOrderRow(t, rows, cache, titles, map[string]bool{}, conflictTitlesBySong, map[int64]keyTransitionWarning{}, "song-a")
	if len(gotA.ConflictTitles) != 1 || gotA.ConflictTitles[0] != "Song B" {
		t.Errorf("expected song-a's ConflictTitles to be [\"Song B\"], got %+v", gotA.ConflictTitles)
	}
}

// TestRenderServiceOrderAttachesKeyTransitionToSecondSongOnly confirms a
// transitionsByRow entry (keyed by the second song's own row id, see
// keyTransitionWarning.SongRowID) attaches KeyTransitionLabel/Message onto
// that later song's rendered row only - never the earlier one, per
// docs/plans/shipped/in-place-set-warnings-plan.md's resolved rendering
// decision.
func TestRenderServiceOrderAttachesKeyTransitionToSecondSongOnly(t *testing.T) {
	e := setSong("song-e", "", 0)
	e.ID = 1
	f := setSong("song-f", "", 1)
	f.ID = 2
	rows := []mergedRow{{Song: &e}, {Song: &f}}
	cache := map[string]scheduling.SongArrangementInfo{}
	transitionsByRow := map[int64]keyTransitionWarning{
		f.ID: {SongRowID: f.ID, Label: "half-step from previous song", Message: "Song E (E) into Song F (F) is a half-step key change..."},
	}

	gotE := renderServiceOrderRow(t, rows, cache, map[string]string{}, map[string]bool{}, map[string][]string{}, transitionsByRow, "song-e")
	if gotE.KeyTransitionLabel != "" {
		t.Errorf("expected song-e (the earlier song) to carry no key-transition badge, got %q", gotE.KeyTransitionLabel)
	}

	gotF := renderServiceOrderRow(t, rows, cache, map[string]string{}, map[string]bool{}, map[string][]string{}, transitionsByRow, "song-f")
	if gotF.KeyTransitionLabel == "" {
		t.Error("expected song-f (the later song) to carry the key-transition badge label")
	}
	if gotF.KeyTransitionMessage == "" {
		t.Error("expected song-f to carry the full key-transition message")
	}
}

func assertRowOrder(t *testing.T, got []mergedRow, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i, row := range got {
		if label := rowLabel(row); label != want[i] {
			t.Errorf("row %d: expected %q, got %q", i, want[i], label)
		}
	}
}
