package controllers

// Unit tests for RepositionPlanItem's pure helpers - resolving a drag-and-
// drop "after" token to the nearest real Planning Center item
// (anchorRealItemID) and applying that as a move within the plan's real
// item list (buildReorderedItemIDs/itemOrderEqual). All pure functions over
// already-in-hand data, same reasoning as mergeServiceOrder's own tests in
// sets_builder_test.go, whose foreignItem/setSong/publishedSetSong
// constructors this file reuses.

import (
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func TestAnchorRealItemIDDirectItemToken(t *testing.T) {
	got := anchorRealItemID(nil, "item:i2")
	if got != "i2" {
		t.Errorf("expected i2, got %q", got)
	}
}

func TestAnchorRealItemIDEmptyTokenIsTopOfOrder(t *testing.T) {
	if got := anchorRealItemID(nil, ""); got != "" {
		t.Errorf("expected \"\" (top of order), got %q", got)
	}
}

func TestAnchorRealItemIDPublishedRowResolvesToItsOwnItemID(t *testing.T) {
	ss := scheduling.SetSong{ID: 5, SongID: "s1", PCOItemID: "i1"}
	merged := []mergedRow{{Song: &ss}}

	got := anchorRealItemID(merged, "row:5")
	if got != "i1" {
		t.Errorf("expected i1, got %q", got)
	}
}

// TestAnchorRealItemIDUnpublishedRowWalksBackToNearestRealItem covers the
// case a plain drag-and-drop drop can produce that PublishSet never has to:
// dropping a foreign item right after a set song that hasn't been
// published yet, so it has no real PCOItemID of its own to anchor on. The
// nearest real item behind it (here, a foreign header two rows back) is
// what Planning Center's own order actually needs.
func TestAnchorRealItemIDUnpublishedRowWalksBackToNearestRealItem(t *testing.T) {
	welcome := foreignItem("i1", "Welcome", pco.ItemTypeHeader)
	published := scheduling.SetSong{ID: 1, SongID: "opener", AnchorItemID: "i1", PCOItemID: "i-opener"}
	draft := scheduling.SetSong{ID: 2, SongID: "unpublished", AnchorItemID: "i1", Position: 1}

	merged := []mergedRow{
		{Item: &welcome},
		{Song: &published},
		{Song: &draft},
	}

	got := anchorRealItemID(merged, "row:2")
	if got != "i-opener" {
		t.Errorf("expected i-opener (the nearest real item), got %q", got)
	}
}

// TestAnchorRealItemIDUnpublishedRowAtTopFallsBackToTopOfOrder covers an
// unpublished song with nothing real before it at all - there's no real
// item to anchor on, so the drop resolves to the top of Planning Center's
// own order instead of a missing/invalid anchor.
func TestAnchorRealItemIDUnpublishedRowAtTopFallsBackToTopOfOrder(t *testing.T) {
	draft := scheduling.SetSong{ID: 1, SongID: "opener"}
	merged := []mergedRow{{Song: &draft}}

	if got := anchorRealItemID(merged, "row:1"); got != "" {
		t.Errorf("expected \"\" (top of order), got %q", got)
	}
}

func TestBuildReorderedItemIDsMovesAfterAnchor(t *testing.T) {
	items := []pco.ItemData{
		{ID: "i1"}, {ID: "i2"}, {ID: "i3"},
	}

	got := buildReorderedItemIDs(items, "i1", "i3")

	want := []string{"i2", "i3", "i1"}
	assertStringOrder(t, got, want)
}

func TestBuildReorderedItemIDsEmptyAnchorMovesToTop(t *testing.T) {
	items := []pco.ItemData{
		{ID: "i1"}, {ID: "i2"}, {ID: "i3"},
	}

	got := buildReorderedItemIDs(items, "i3", "")

	want := []string{"i3", "i1", "i2"}
	assertStringOrder(t, got, want)
}

// TestBuildReorderedItemIDsSelfAnchorIsTreatedAsTop confirms a dragged
// item can never end up anchored to itself (which would silently drop it
// from the reorder, since it's already excluded from the candidate list
// before the anchor is placed) - falls back to the top instead.
func TestBuildReorderedItemIDsSelfAnchorIsTreatedAsTop(t *testing.T) {
	items := []pco.ItemData{{ID: "i1"}, {ID: "i2"}}

	got := buildReorderedItemIDs(items, "i1", "i1")

	want := []string{"i1", "i2"}
	assertStringOrder(t, got, want)
}

// TestBuildReorderedItemIDsStaleAnchorAppendsAtEnd mirrors
// TestMergeServiceOrderStaleAnchorAppendsAtEnd's own reasoning: an anchor
// that no longer names a real item (deleted in Planning Center between
// this page's last render and the drop, say) shouldn't silently drop the
// dragged item from the reorder call entirely.
func TestBuildReorderedItemIDsStaleAnchorAppendsAtEnd(t *testing.T) {
	items := []pco.ItemData{{ID: "i1"}, {ID: "i2"}}

	got := buildReorderedItemIDs(items, "i1", "deleted")

	want := []string{"i2", "i1"}
	assertStringOrder(t, got, want)
}

func TestItemOrderEqualTrueForSameOrder(t *testing.T) {
	items := []pco.ItemData{{ID: "i1"}, {ID: "i2"}}

	if !itemOrderEqual(items, []string{"i1", "i2"}) {
		t.Error("expected the same order to be reported equal")
	}
}

func TestItemOrderEqualFalseForDifferentOrder(t *testing.T) {
	items := []pco.ItemData{{ID: "i1"}, {ID: "i2"}}

	if itemOrderEqual(items, []string{"i2", "i1"}) {
		t.Error("expected a swapped order to be reported unequal")
	}
}

func TestItemOrderEqualFalseForDifferentLength(t *testing.T) {
	items := []pco.ItemData{{ID: "i1"}, {ID: "i2"}}

	if itemOrderEqual(items, []string{"i1"}) {
		t.Error("expected a shorter list to be reported unequal")
	}
}

func assertStringOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
