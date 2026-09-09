package scheduling

import "testing"

// Unit tests for song_vocal_range.go's pure math -
// docs/plans/in-progress/vocal-range-plan.md's Phase 2, EffectiveVocalRange
// section, updated for migration 00043's shape (the exact entered pitch
// class/octave, not an offset from a re-anchored tonic - see that
// migration's own comment for why).

// TestEffectiveVocalRangeSameKeyReconstructsExactEntry confirms the whole
// point of migration 00043: transposing into the same key as the reference
// reproduces the exact notes as entered, byte-for-byte, no anchor-octave
// guess involved - the specific regression this fixed (E3-E4 entered
// against reference key A used to come back as E4-E5, since the old
// offset-based storage could only reconstruct a plausible-looking register
// anchored at a fixed octave, not the real one).
func TestEffectiveVocalRangeSameKeyReconstructsExactEntry(t *testing.T) {
	// E3 (pitch class 4, octave 3) to E4 (pitch class 4, octave 4),
	// measured against reference key A.
	effLow, effHigh, ok := EffectiveVocalRange("A", 4, 3, 4, 4, "A")
	if !ok {
		t.Fatal("expected both keys to parse")
	}
	lowPC, lowOct := PitchClassAndOctave(effLow)
	highPC, highOct := PitchClassAndOctave(effHigh)
	if lowPC != 4 || lowOct != 3 {
		t.Errorf("low = (pc %d, oct %d), want (pc 4, oct 3) - E3", lowPC, lowOct)
	}
	if highPC != 4 || highOct != 4 {
		t.Errorf("high = (pc %d, oct %d), want (pc 4, oct 4) - E4", highPC, highOct)
	}
}

// TestEffectiveVocalRangeTransposesByExpectedDelta confirms shifting into
// a different key moves the effective range by exactly
// SignedSemitoneDelta's own shift, in both directions, while preserving
// the interval's width.
func TestEffectiveVocalRangeTransposesByExpectedDelta(t *testing.T) {
	// C3 to E4, measured in C.
	baseLow, baseHigh, ok := EffectiveVocalRange("C", 0, 3, 4, 4, "C")
	if !ok {
		t.Fatal("expected both keys to parse")
	}

	// Up to D (a whole step, delta = +2).
	upLow, upHigh, ok := EffectiveVocalRange("C", 0, 3, 4, 4, "D")
	if !ok {
		t.Fatal("expected both keys to parse")
	}
	if upLow-baseLow != 2 || upHigh-baseHigh != 2 {
		t.Errorf("expected a +2 semitone shift into D, got low %+d, high %+d", upLow-baseLow, upHigh-baseHigh)
	}

	// Down to Bb (SignedSemitoneDelta(0, 10) = -2, the shorter path).
	downLow, downHigh, ok := EffectiveVocalRange("C", 0, 3, 4, 4, "Bb")
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

func TestEffectiveVocalRangeRejectsUnparseableReferenceKey(t *testing.T) {
	if _, _, ok := EffectiveVocalRange("capo 2", 0, 3, 4, 4, "C"); ok {
		t.Error("expected an unparseable reference key to fail")
	}
}

func TestEffectiveVocalRangeRejectsUnparseableTargetKey(t *testing.T) {
	if _, _, ok := EffectiveVocalRange("C", 0, 3, 7, 4, "capo 2"); ok {
		t.Error("expected an unparseable target key to fail")
	}
}
