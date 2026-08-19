package controllers

import (
	"testing"

	"pco-assistant/internal/scheduling"

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
