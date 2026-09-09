package scheduling

import "testing"

// Unit tests for computeVocalRangeNotePair (song_vocal_range.templ) - the
// low/high display+ints helper the Song Vocal Range table's play buttons
// and text both come from, replacing the old songVocalRangeLowDisplay/
// songVocalRangeHighDisplay pair (each of which discarded its computed
// pitch-class/octave ints into a string, computing EffectiveVocalRange/
// PitchClassAndOctave twice per row for no reason). Updated for migration
// 00043's shape (the exact entered pitch class/octave, not an offset from
// a re-anchored tonic).

func TestComputeVocalRangeNotePairAsMeasuredNoTransposition(t *testing.T) {
	// Reference key D, low note C3 (pitch class 0, octave 3), high note G4
	// (pitch class 7, octave 4). targetKey == referenceKey, so no
	// transposition shift is applied - the pair reconstructs exactly what
	// was entered, byte-for-byte, no anchor-octave guess involved.
	pair := computeVocalRangeNotePair("D", 0, 3, 7, 4, "D")
	if !pair.OK {
		t.Fatal("expected pair.OK, got false")
	}
	if pair.LowPitchClass != 0 || pair.LowOctave != 3 {
		t.Errorf("low = (pc %d, oct %d), want (pc 0, oct 3) - C3", pair.LowPitchClass, pair.LowOctave)
	}
	if pair.HighPitchClass != 7 || pair.HighOctave != 4 {
		t.Errorf("high = (pc %d, oct %d), want (pc 7, oct 4) - G4", pair.HighPitchClass, pair.HighOctave)
	}
}

func TestComputeVocalRangeNotePairTransposedIntoDifferentKey(t *testing.T) {
	// Same stored range as above (reference D, C3-G4), but this time
	// transposed a whole step down into C - every note should shift down 2
	// semitones from the as-measured case, the interval width preserved
	// exactly.
	measured := computeVocalRangeNotePair("D", 0, 3, 7, 4, "D")
	transposed := computeVocalRangeNotePair("D", 0, 3, 7, 4, "C")
	if !transposed.OK {
		t.Fatal("expected transposed.OK, got false")
	}

	measuredLowAbs := measured.LowOctave*12 + measured.LowPitchClass
	transposedLowAbs := transposed.LowOctave*12 + transposed.LowPitchClass
	if measuredLowAbs-transposedLowAbs != 2 {
		t.Errorf("low shifted by %d semitones, want 2 (D -> C is a whole step down)", measuredLowAbs-transposedLowAbs)
	}

	measuredHighAbs := measured.HighOctave*12 + measured.HighPitchClass
	transposedHighAbs := transposed.HighOctave*12 + transposed.HighPitchClass
	if measuredHighAbs-transposedHighAbs != 2 {
		t.Errorf("high shifted by %d semitones, want 2 (D -> C is a whole step down)", measuredHighAbs-transposedHighAbs)
	}

	// Interval width must be preserved by transposition.
	if (transposedHighAbs - transposedLowAbs) != (measuredHighAbs - measuredLowAbs) {
		t.Errorf("interval width changed by transposition: measured %d, transposed %d", measuredHighAbs-measuredLowAbs, transposedHighAbs-transposedLowAbs)
	}
}

func TestComputeVocalRangeNotePairUnparseableKeyIsNotOK(t *testing.T) {
	if pair := computeVocalRangeNotePair("not-a-key", 0, 3, 7, 4, "D"); pair.OK {
		t.Error("expected pair.OK = false for an unparseable reference key")
	}
	if pair := computeVocalRangeNotePair("D", 0, 3, 7, 4, "not-a-key"); pair.OK {
		t.Error("expected pair.OK = false for an unparseable target key")
	}
}
