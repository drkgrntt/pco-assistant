package scheduling

import "testing"

// Unit tests for song_vocal_range.go's pure math -
// docs/plans/in-progress/vocal-range-plan.md's Phase 2. ComputeVocalRangeOffsets
// worked examples deliberately mirror the plan's own anchoring-rule
// section: one where the reference key sits higher than the low note
// (forcing the octave-down step), one where it already sits at/below it.

func TestComputeVocalRangeOffsetsReferenceKeyHigherThanLowNoteStepsDown(t *testing.T) {
	// Reference key D (pitch class 2), low note C3 (pitch class 0, octave
	// 3): D's natural placement in octave 3 (AbsoluteSemitone(2,3)=38) sits
	// above C3's absolute value (36), so the tonic steps down one octave
	// to 26 - lowOffset = 36-26 = 10, still non-negative and < 12 as the
	// anchoring rule guarantees.
	lowOffset, highOffset, ok := ComputeVocalRangeOffsets("D", 0, 3, 7, 4) // low C3, high G4
	if !ok {
		t.Fatal("expected reference key \"D\" to parse")
	}
	if lowOffset != 10 {
		t.Errorf("expected lowOffset 10, got %d", lowOffset)
	}
	if highOffset != 29 {
		t.Errorf("expected highOffset 29, got %d", highOffset)
	}
	if highOffset < lowOffset {
		t.Errorf("expected highOffset (%d) >= lowOffset (%d)", highOffset, lowOffset)
	}
}

func TestComputeVocalRangeOffsetsReferenceKeyAtOrBelowLowNoteNoAdjustment(t *testing.T) {
	// Reference key C (pitch class 0), low note G3 (pitch class 7, octave
	// 3): C's natural placement in octave 3 (AbsoluteSemitone(0,3)=36)
	// already sits below G3's absolute value (43), so no octave-down step
	// is needed - lowOffset = 43-36 = 7.
	lowOffset, highOffset, ok := ComputeVocalRangeOffsets("C", 7, 3, 2, 5) // low G3, high D5
	if !ok {
		t.Fatal("expected reference key \"C\" to parse")
	}
	if lowOffset != 7 {
		t.Errorf("expected lowOffset 7, got %d", lowOffset)
	}
	if highOffset != 26 {
		t.Errorf("expected highOffset 26, got %d", highOffset)
	}
}

// TestComputeVocalRangeOffsetsLowOffsetAlwaysBoundedByOctave confirms the
// anchoring rule's own guarantee - lowOffset is always in [0, 11], since
// the tonic is always placed at or immediately below the low note,
// regardless of which real octave the low note happens to be in.
func TestComputeVocalRangeOffsetsLowOffsetAlwaysBoundedByOctave(t *testing.T) {
	cases := []struct {
		referenceKey                   string
		lowPC, lowOct, highPC, highOct int
	}{
		{"G", 0, 2, 4, 4},
		{"Bb", 9, 5, 2, 6},
		{"F#", 7, 3, 7, 3},
	}

	for _, c := range cases {
		lowOffset, _, ok := ComputeVocalRangeOffsets(c.referenceKey, c.lowPC, c.lowOct, c.highPC, c.highOct)
		if !ok {
			t.Fatalf("expected reference key %q to parse", c.referenceKey)
		}
		if lowOffset < 0 || lowOffset > 11 {
			t.Errorf("expected lowOffset in [0,11] for %+v, got %d", c, lowOffset)
		}
	}
}

func TestComputeVocalRangeOffsetsRejectsUnparseableReferenceKey(t *testing.T) {
	if _, _, ok := ComputeVocalRangeOffsets("capo 2", 0, 3, 7, 4); ok {
		t.Error("expected an unparseable reference key to fail")
	}
}

// TestEffectiveVocalRangeSameKeyNoShift confirms transposing into the same
// key as the reference applies no shift at all - the effective range
// should just be the offsets applied to the fixed tonic anchor.
func TestEffectiveVocalRangeSameKeyNoShift(t *testing.T) {
	lowOffset, highOffset, ok := ComputeVocalRangeOffsets("G", 7, 3, 2, 5) // low G3, high D5, measured in G
	if !ok {
		t.Fatal("expected reference key \"G\" to parse")
	}

	effLow, effHigh, ok := EffectiveVocalRange("G", lowOffset, highOffset, "G")
	if !ok {
		t.Fatal("expected both keys to parse")
	}
	if effHigh-effLow != highOffset-lowOffset {
		t.Errorf("expected the same-key interval width to be preserved (%d), got %d", highOffset-lowOffset, effHigh-effLow)
	}
}

// TestEffectiveVocalRangeTransposesByExpectedDelta confirms shifting into
// a different key moves the effective range by exactly
// SignedSemitoneDelta's own shift, in both directions, while preserving
// the interval's width.
func TestEffectiveVocalRangeTransposesByExpectedDelta(t *testing.T) {
	lowOffset, highOffset, ok := ComputeVocalRangeOffsets("C", 0, 3, 4, 4) // low C3, high E4, measured in C
	if !ok {
		t.Fatal("expected reference key \"C\" to parse")
	}

	baseLow, baseHigh, ok := EffectiveVocalRange("C", lowOffset, highOffset, "C")
	if !ok {
		t.Fatal("expected both keys to parse")
	}

	// Up to D (a whole step, delta = +2).
	upLow, upHigh, ok := EffectiveVocalRange("C", lowOffset, highOffset, "D")
	if !ok {
		t.Fatal("expected both keys to parse")
	}
	if upLow-baseLow != 2 || upHigh-baseHigh != 2 {
		t.Errorf("expected a +2 semitone shift into D, got low %+d, high %+d", upLow-baseLow, upHigh-baseHigh)
	}

	// Down to Bb (SignedSemitoneDelta(0, 10) = -2, the shorter path).
	downLow, downHigh, ok := EffectiveVocalRange("C", lowOffset, highOffset, "Bb")
	if !ok {
		t.Fatal("expected both keys to parse")
	}
	if downLow-baseLow != -2 || downHigh-baseHigh != -2 {
		t.Errorf("expected a -2 semitone shift into Bb, got low %+d, high %+d", downLow-baseLow, downHigh-baseHigh)
	}

	// The interval width itself never changes under transposition.
	if (upHigh - upLow) != (baseHigh - baseLow) {
		t.Errorf("expected interval width to be preserved under transposition")
	}
}

func TestEffectiveVocalRangeRejectsUnparseableTargetKey(t *testing.T) {
	if _, _, ok := EffectiveVocalRange("C", 0, 7, "capo 2"); ok {
		t.Error("expected an unparseable target key to fail")
	}
}
