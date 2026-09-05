package controllers

// Pure-function coverage for MergeSet's already-tracked-item reconciliation
// (reconcileTrackedItems/trackedItemOrderChanged, sets_publish.go) - the
// live-sync-reflection plan's step 3
// (docs/plans/in-progress/live-sync-reflection-plan.md). Reuses
// sets_builder_test.go's helper constructors (foreignItem/songItem/setSong/
// publishedSetSong) rather than introducing fixtures or a mocking
// framework, same convention as deriveMergeImportRows' own tests.

import (
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// trackedRow is publishedSetSong plus an explicit row ID and
// Description/KeyOverride/InfoPending - the extra fields
// reconcileTrackedItems actually reads that publishedSetSong's plain
// constructor leaves zero-valued.
func trackedRow(id int64, songID, pcoItemID, description, keyOverride string, infoPending bool) scheduling.SetSong {
	ss := publishedSetSong(songID, "", 0, pcoItemID)
	ss.ID = id
	ss.Description = description
	ss.KeyOverride = keyOverride
	ss.InfoPending = infoPending
	return ss
}

func TestReconcileTrackedItemsUpdatesWhenInfoPendingFalse(t *testing.T) {
	items := []pco.ItemData{songItemKeyed("item-1", "song-1", "Amazing Grace", "Bb")}
	items[0].Attributes.Description = "New description from PCO"

	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "old description", "G", false),
	}

	got := reconcileTrackedItems(items, setSongs)

	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
	change := got[0]
	if change.RowID != 1 || change.Missing {
		t.Fatalf("expected an update for row 1, got %+v", change)
	}
	if change.NewDescription != "New description from PCO" || change.NewKeyOverride != "Bb" {
		t.Errorf("expected PCO's live description/key, got %+v", change)
	}
}

func TestReconcileTrackedItemsSkipsInfoPendingRows(t *testing.T) {
	items := []pco.ItemData{songItemKeyed("item-1", "song-1", "Amazing Grace", "Bb")}
	items[0].Attributes.Description = "New description from PCO"

	// Same PCO-side change as above, but this row has an unpushed local
	// edit (InfoPending true) - the conflict-policy decision says the
	// local edit always wins, so this must be left completely untouched.
	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "my own pending note", "C", true),
	}

	got := reconcileTrackedItems(items, setSongs)

	if len(got) != 0 {
		t.Fatalf("expected no changes for an InfoPending row, got %+v", got)
	}
}

func TestReconcileTrackedItemsClearsMissingPCOItem(t *testing.T) {
	// No items at all live in PCO any more - song-1's item-1 was deleted
	// directly in Planning Center.
	var items []pco.ItemData

	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "a note", "G", false),
	}

	got := reconcileTrackedItems(items, setSongs)

	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
	if got[0].RowID != 1 || !got[0].Missing {
		t.Errorf("expected row 1 reported Missing, got %+v", got[0])
	}
}

func TestReconcileTrackedItemsNoChangeWhenAlreadyInSync(t *testing.T) {
	items := []pco.ItemData{songItemKeyed("item-1", "song-1", "Amazing Grace", "G")}
	items[0].Attributes.Description = "same note"

	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "same note", "G", false),
	}

	got := reconcileTrackedItems(items, setSongs)

	if len(got) != 0 {
		t.Fatalf("expected no changes when local already matches PCO, got %+v", got)
	}
}

func TestReconcileTrackedItemsIgnoresUnpublishedRows(t *testing.T) {
	// A row with no PCOItemID yet (never published) has nothing in PCO to
	// reconcile against - MergeSet's net-new-import step handles this
	// case, not this one.
	setSongs := []scheduling.SetSong{setSong("song-1", "", 0)}

	got := reconcileTrackedItems(nil, setSongs)

	if len(got) != 0 {
		t.Fatalf("expected no changes for an unpublished row, got %+v", got)
	}
}

func TestTrackedItemOrderChangedFalseWhenUntouched(t *testing.T) {
	items := []pco.ItemData{
		foreignItem("header-1", "Welcome", pco.ItemTypeHeader),
		songItem("item-1", "song-1", "Song One"),
		songItem("item-2", "song-2", "Song Two"),
	}
	items[1].Attributes.Sequence = 1
	items[2].Attributes.Sequence = 2

	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "", "", false),
		trackedRow(2, "song-2", "item-2", "", "", false),
	}
	setSongs[0].AnchorItemID = "header-1"
	setSongs[1].AnchorItemID = "item-1"

	if trackedItemOrderChanged(items, setSongs) {
		t.Errorf("expected no order-changed signal when nothing was reordered")
	}
}

func TestTrackedItemOrderChangedTrueOnPureReorder(t *testing.T) {
	// Same items, same anchors as the untouched case above - but item-2 now
	// sits ahead of item-1 in the plan's live Sequence order (already
	// sorted by Sequence here, same as every real caller does before
	// calling this - see MergeSet), the way a pure PCO-side
	// pco.ReorderItems call would produce (live-verified: this does NOT
	// bump either item's UpdatedAt - see pcoItemsFingerprint's own doc
	// comment). Neither Position nor AnchorItemID on either SetSong
	// changes - this check must catch the drift from Sequence alone.
	items := []pco.ItemData{
		foreignItem("header-1", "Welcome", pco.ItemTypeHeader),
		songItem("item-2", "song-2", "Song Two"),
		songItem("item-1", "song-1", "Song One"),
	}
	items[1].Attributes.Sequence = 1
	items[2].Attributes.Sequence = 2

	setSongs := []scheduling.SetSong{
		trackedRow(1, "song-1", "item-1", "", "", false),
		trackedRow(2, "song-2", "item-2", "", "", false),
	}
	setSongs[0].AnchorItemID = "header-1"
	setSongs[1].AnchorItemID = "item-1"

	if !trackedItemOrderChanged(items, setSongs) {
		t.Errorf("expected a pure reorder among tracked items to be detected")
	}

	// Confirm this check really is read-only - Position/AnchorItemID must
	// come back exactly as passed in.
	if setSongs[0].Position != 0 || setSongs[0].AnchorItemID != "header-1" {
		t.Errorf("expected setSongs[0] untouched, got %+v", setSongs[0])
	}
	if setSongs[1].Position != 0 || setSongs[1].AnchorItemID != "item-1" {
		t.Errorf("expected setSongs[1] untouched, got %+v", setSongs[1])
	}
}

func TestTrackedItemOrderChangedFalseWhenNoTrackedSongsPublished(t *testing.T) {
	items := []pco.ItemData{foreignItem("header-1", "Welcome", pco.ItemTypeHeader)}
	setSongs := []scheduling.SetSong{setSong("song-1", "", 0)} // never published

	if trackedItemOrderChanged(items, setSongs) {
		t.Errorf("expected no order-changed signal with nothing published yet")
	}
}
