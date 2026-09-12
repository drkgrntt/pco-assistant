package scheduling

import "testing"

// Finding 1 (2.5.7 Dragging Movements / 2.1.1 Keyboard,
// docs/plans/shipped/wcag-aa-audit-plan.md): rowMenuItems' "Move up"/"Move
// down" actions post the same ?after= token the pointer-drag path already
// sends (RepositionSetSong/parseAfterToken) - these tests cover
// rowToken/moveUpAfterToken/moveDownAfterToken purely server-side, mirroring
// scheduling-build.js's own previousTargetRow/onPointerUp logic without a
// browser.

func songOrderRow(id int64) ServiceOrderRow {
	return ServiceOrderRow{IsSong: true, Song: SetSongView{ID: id}}
}

func foreignOrderRow(itemID string) ServiceOrderRow {
	return ServiceOrderRow{IsSong: false, ItemID: itemID}
}

func TestRowToken(t *testing.T) {
	if got := rowToken(songOrderRow(42)); got != "row:42" {
		t.Errorf("rowToken(song) = %q, want %q", got, "row:42")
	}
	if got := rowToken(foreignOrderRow("pco-item-7")); got != "item:pco-item-7" {
		t.Errorf("rowToken(foreign) = %q, want %q", got, "item:pco-item-7")
	}
}

func TestMoveUpAfterToken(t *testing.T) {
	order := []ServiceOrderRow{
		songOrderRow(1), // i=0
		songOrderRow(2), // i=1
		songOrderRow(3), // i=2
		songOrderRow(4), // i=3
	}

	if _, ok := moveUpAfterToken(order, 0); ok {
		t.Error("moving the very first row up should not be possible")
	}

	after, ok := moveUpAfterToken(order, 1)
	if !ok || after != "" {
		t.Errorf("moveUpAfterToken(order, 1) = (%q, %v), want (\"\", true) - moving the second row up lands at the top of service", after, ok)
	}

	after, ok = moveUpAfterToken(order, 2)
	if !ok || after != "row:1" {
		t.Errorf("moveUpAfterToken(order, 2) = (%q, %v), want (%q, true)", after, ok, "row:1")
	}

	after, ok = moveUpAfterToken(order, 3)
	if !ok || after != "row:2" {
		t.Errorf("moveUpAfterToken(order, 3) = (%q, %v), want (%q, true)", after, ok, "row:2")
	}
}

func TestMoveDownAfterToken(t *testing.T) {
	order := []ServiceOrderRow{
		songOrderRow(1),        // i=0
		foreignOrderRow("hdr"), // i=1 - a header/foreign row works as an anchor too
		songOrderRow(3),        // i=2
	}

	after, ok := moveDownAfterToken(order, 0)
	if !ok || after != "item:hdr" {
		t.Errorf("moveDownAfterToken(order, 0) = (%q, %v), want (%q, true)", after, ok, "item:hdr")
	}

	after, ok = moveDownAfterToken(order, 1)
	if !ok || after != "row:3" {
		t.Errorf("moveDownAfterToken(order, 1) = (%q, %v), want (%q, true)", after, ok, "row:3")
	}

	if _, ok := moveDownAfterToken(order, 2); ok {
		t.Error("moving the very last row down should not be possible")
	}
}

func TestAfterValsJSON(t *testing.T) {
	cases := map[string]string{
		"":        `{"after": ""}`,
		"row:5":   `{"after": "row:5"}`,
		"item:99": `{"after": "item:99"}`,
	}
	for after, want := range cases {
		if got := afterValsJSON(after); got != want {
			t.Errorf("afterValsJSON(%q) = %q, want %q", after, got, want)
		}
	}
}
