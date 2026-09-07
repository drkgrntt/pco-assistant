package scheduling

import "testing"

// Unit tests for computeVocalRangeNotePair (song_vocal_range.templ) - the
// low/high display+ints helper the Song Vocal Range table's play buttons
// and text both come from, replacing the old songVocalRangeLowDisplay/
// songVocalRangeHighDisplay pair (each of which discarded its computed
// pitch-class/octave ints into a string, computing EffectiveVocalRange/
// PitchClassAndOctave twice per row for no reason).

func TestComputeVocalRangeNotePairAsMeasuredNoTransposition(t *testing.T) {
	// Reference key D, low offset 10, high offset 29 (the same stored
	// offsets as the ComputeVocalRangeOffsets worked example in
	// scheduling/song_vocal_range_test.go, D/C3/G4). targetKey ==
	// referenceKey, so no transposition shift is applied - but per
	// EffectiveVocalRange's own doc comment, the reconstructed notes land
	// at the fixed referenceTonicOctave anchor (3), not necessarily the
	// original entry octave: D's tonic at octave 3 is absolute semitone
	// 38, so low (38+10=48) is pitch class 0 octave 4, high (38+29=67) is
	// pitch class 7 octave 5.
	pair := computeVocalRangeNotePair("D", 10, 29, "D")
	if !pair.OK {
		t.Fatal("expected pair.OK, got false")
	}
	if pair.LowPitchClass != 0 || pair.LowOctave != 4 {
		t.Errorf("low = (pc %d, oct %d), want (pc 0, oct 4)", pair.LowPitchClass, pair.LowOctave)
	}
	if pair.HighPitchClass != 7 || pair.HighOctave != 5 {
		t.Errorf("high = (pc %d, oct %d), want (pc 7, oct 5)", pair.HighPitchClass, pair.HighOctave)
	}
}

func TestComputeVocalRangeNotePairTransposedIntoDifferentKey(t *testing.T) {
	// Same stored range as above (reference D, offsets 10/29), but this
	// time transposed a whole step down into C - every note should shift
	// down 2 semitones from the as-measured case, the interval width
	// (29-10=19 semitones) preserved exactly.
	measured := computeVocalRangeNotePair("D", 10, 29, "D")
	transposed := computeVocalRangeNotePair("D", 10, 29, "C")
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
	if pair := computeVocalRangeNotePair("not-a-key", 0, 5, "D"); pair.OK {
		t.Error("expected pair.OK = false for an unparseable reference key")
	}
	if pair := computeVocalRangeNotePair("D", 0, 5, "not-a-key"); pair.OK {
		t.Error("expected pair.OK = false for an unparseable target key")
	}
}
