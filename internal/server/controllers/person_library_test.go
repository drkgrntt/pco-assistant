package controllers

import (
	"reflect"
	"testing"

	"pco-assistant/internal/scheduling"
)

func TestDedupeIDs(t *testing.T) {
	tests := []struct {
		name   string
		groups [][]string
		want   []string
	}{
		{name: "no groups", groups: nil, want: nil},
		{name: "empty groups", groups: [][]string{{}, {}}, want: nil},
		{
			name:   "no overlap",
			groups: [][]string{{"b"}, {"a"}},
			want:   []string{"a", "b"},
		},
		{
			name:   "overlap across groups collapses to one",
			groups: [][]string{{"a", "b"}, {"b", "c"}, {"c"}},
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "duplicate within a single group also collapses",
			groups: [][]string{{"a", "a", "b"}},
			want:   []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeIDs(tt.groups...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupeIDs(%v) = %v, want %v", tt.groups, got, tt.want)
			}
		})
	}
}

func TestPersonLibraryRowsResolvesNamesAndSortsCaseInsensitively(t *testing.T) {
	ids := []string{"p1", "p2", "p3"}
	lookups := map[string]personLookup{
		"p1": {Name: "zack", AvatarURL: "https://example.com/zack.png"},
		"p2": {Name: "Amy", AvatarURL: "https://example.com/amy.png"},
		// p3 deliberately missing - a failed/unavailable batched lookup.
	}

	rows := personLibraryRows(ids, lookups)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %+v", len(rows), rows)
	}

	// Case-insensitive sort by display name: "Amy" before "p3" (falls back to
	// raw id) before "zack".
	wantOrder := []string{"p2", "p3", "p1"}
	for i, row := range rows {
		if row.PersonID != wantOrder[i] {
			t.Errorf("rows[%d].PersonID = %q, want %q (order: %v)", i, row.PersonID, wantOrder[i], rows)
		}
	}

	// p3 has no lookup entry - falls back to showing the raw id, same
	// convention as displayNameOrID elsewhere in this app.
	for _, row := range rows {
		if row.PersonID == "p3" && row.Name != "p3" {
			t.Errorf("p3's Name = %q, want fallback to raw id %q", row.Name, "p3")
		}
	}
}

func TestPersonDetailVocalRangeViewNoRange(t *testing.T) {
	view := personDetailVocalRangeView(scheduling.PersonVocalRange{}, false)
	if view.Has {
		t.Fatal("expected Has = false when hasRange is false")
	}
	if view.Line.VocalRangeDisplay != "" {
		t.Errorf("expected zero-value Line when there's no range, got %+v", view.Line)
	}
}

func TestPersonDetailVocalRangeViewHasRange(t *testing.T) {
	vr := scheduling.PersonVocalRange{
		ID:                42,
		LowestPitchClass:  7, // G
		LowestOctave:      3,
		HighestPitchClass: 2, // D
		HighestOctave:     5,
	}

	view := personDetailVocalRangeView(vr, true)

	if !view.Has {
		t.Fatal("expected Has = true")
	}
	if view.ID != 42 {
		t.Errorf("view.ID = %d, want 42 (the underlying PersonVocalRange row's own id, for the Delete button's URL)", view.ID)
	}
	if want := "G3-D5"; view.Line.VocalRangeDisplay != want {
		t.Errorf("view.Line.VocalRangeDisplay = %q, want %q", view.Line.VocalRangeDisplay, want)
	}
	if !view.Line.VocalRangeHasPlayableNotes {
		t.Error("expected VocalRangeHasPlayableNotes = true so personVocalRangeLine renders its tap-to-hear buttons")
	}
	if view.LowestDisplay != "G3" || view.HighestDisplay != "D5" {
		t.Errorf("LowestDisplay/HighestDisplay = %q/%q, want %q/%q (pre-filling the add/edit form)", view.LowestDisplay, view.HighestDisplay, "G3", "D5")
	}
}
