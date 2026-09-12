package controllers

import (
	"strings"
	"testing"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"
)

// TestClassifyVocalRangeCellFitsAsWritten and its siblings below mirror
// TestVocalRangeWarningForSong*'s own worked examples
// (sets_builder_warnings_test.go) - classifyVocalRangeCell is the shared
// core both vocalRangeWarningForSong and the vocal-range matrix
// (vocal_range_matrix.go) now classify every person/song pair through
// (see that function's own doc comment), so these confirm the extraction
// preserved the exact same three-way verdict.
func TestClassifyVocalRangeCellFitsAsWritten(t *testing.T) {
	// Song range A3-D5 (absolute 45-62) sits fully inside Sarah's stored
	// range A3-D5 - no mismatch.
	got := classifyVocalRangeCell(45, 62, 45, 62)
	if got.Verdict != vocalRangeCellFits {
		t.Errorf("expected vocalRangeCellFits, got %+v", got)
	}
}

func TestClassifyVocalRangeCellTipOctaveShift(t *testing.T) {
	// Song range A4-D6 (absolute 57-74) is Tom's own A3-D5 (45-62) shifted
	// a full octave up - fits sung lower.
	got := classifyVocalRangeCell(57, 74, 45, 62)
	if got.Verdict != vocalRangeCellTip {
		t.Errorf("expected vocalRangeCellTip, got %+v", got)
	}
	if got.Direction != "lower" {
		t.Errorf("expected direction %q, got %q", "lower", got.Direction)
	}
}

func TestClassifyVocalRangeCellHardMismatch(t *testing.T) {
	// Song needs up to F5 (absolute 65) against Sarah's A3-D5 (45-62) -
	// too high even shifted an octave either direction.
	got := classifyVocalRangeCell(50, 65, 45, 62)
	if got.Verdict != vocalRangeCellMismatch {
		t.Errorf("expected vocalRangeCellMismatch, got %+v", got)
	}
	if !strings.Contains(got.Reach, "F5") {
		t.Errorf("expected Reach to name the specific violating note F5, got %q", got.Reach)
	}
}

// TestVocalRangeMatrixCellForUnknownWhenSongHasNoRange/
// TestVocalRangeMatrixCellForUnknownWhenPersonHasNoRange confirm the
// matrix's own scoping decision: a person or song with no stored range
// renders as a neutral Unknown cell, never a false Fits or Mismatch.
func TestVocalRangeMatrixCellForUnknownWhenSongHasNoRange(t *testing.T) {
	song := views.VocalRangeMatrixSong{SongID: "song-a", HasRange: false}
	person := views.VocalRangeMatrixPerson{PersonID: "sarah", HasRange: true}
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5},
	}
	got := vocalRangeMatrixCellFor(song, person, ranges)
	if got.Verdict != views.VocalRangeMatrixUnknown {
		t.Errorf("expected VocalRangeMatrixUnknown when the song has no stored range, got %+v", got)
	}
}

func TestVocalRangeMatrixCellForUnknownWhenPersonHasNoRange(t *testing.T) {
	song := views.VocalRangeMatrixSong{
		SongID: "song-a", HasRange: true,
		LowPitchClass: 9, LowOctave: 3, HighPitchClass: 2, HighOctave: 5,
	}
	person := views.VocalRangeMatrixPerson{PersonID: "sam", HasRange: false}
	got := vocalRangeMatrixCellFor(song, person, map[string]scheduling.PersonVocalRange{})
	if got.Verdict != views.VocalRangeMatrixUnknown {
		t.Errorf("expected VocalRangeMatrixUnknown when the person has no stored range, got %+v", got)
	}
}

func TestVocalRangeMatrixCellForFits(t *testing.T) {
	song := views.VocalRangeMatrixSong{
		SongID: "song-a", HasRange: true,
		LowPitchClass: 9, LowOctave: 3, HighPitchClass: 2, HighOctave: 5, // A3-D5
	}
	person := views.VocalRangeMatrixPerson{PersonID: "sarah", HasRange: true}
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
	}
	got := vocalRangeMatrixCellFor(song, person, ranges)
	if got.Verdict != views.VocalRangeMatrixFits {
		t.Errorf("expected VocalRangeMatrixFits, got %+v", got)
	}
	if got.Message != "" {
		t.Errorf("expected no message for a fitting cell, got %q", got.Message)
	}
}

func TestVocalRangeMatrixCellForTip(t *testing.T) {
	song := views.VocalRangeMatrixSong{
		SongID: "song-a", HasRange: true,
		LowPitchClass: 9, LowOctave: 4, HighPitchClass: 2, HighOctave: 6, // A4-D6
	}
	person := views.VocalRangeMatrixPerson{PersonID: "tom", HasRange: true}
	ranges := map[string]scheduling.PersonVocalRange{
		"tom": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
	}
	got := vocalRangeMatrixCellFor(song, person, ranges)
	if got.Verdict != views.VocalRangeMatrixTip {
		t.Errorf("expected VocalRangeMatrixTip, got %+v", got)
	}
	if !strings.Contains(got.Message, "lower") {
		t.Errorf("expected the message to suggest singing it lower, got %q", got.Message)
	}
}

func TestVocalRangeMatrixCellForMismatch(t *testing.T) {
	song := views.VocalRangeMatrixSong{
		SongID: "song-a", HasRange: true,
		LowPitchClass: 2, LowOctave: 4, HighPitchClass: 5, HighOctave: 5, // D4-F5
	}
	person := views.VocalRangeMatrixPerson{PersonID: "sarah", HasRange: true}
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
	}
	got := vocalRangeMatrixCellFor(song, person, ranges)
	if got.Verdict != views.VocalRangeMatrixMismatch {
		t.Errorf("expected VocalRangeMatrixMismatch, got %+v", got)
	}
	if !strings.Contains(got.Message, "F5") {
		t.Errorf("expected the message to name the violating note F5, got %q", got.Message)
	}
}
