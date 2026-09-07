package scheduling

import "testing"

// Unit tests for the key engine (key_engine.go) - docs/plans/in-progress/
// vocal-range-plan.md's Phase 2 prerequisite. TestParseKeyPitchClass*/
// TestSemitoneDistanceWrapsAround below are relocated verbatim from
// internal/server/controllers/sets_builder_warnings_test.go (no behavior
// change - see that plan doc's "Phase 2 prerequisite" section); everything
// else here is new, covering SignedSemitoneDelta/ParseNote/AbsoluteSemitone/
// PitchClassAndOctave.

func TestParseKeyPitchClassBareNote(t *testing.T) {
	got, ok := ParseKeyPitchClass("G")
	if !ok || got != 7 {
		t.Errorf("expected G -> 7, true, got %d, %v", got, ok)
	}
}

func TestParseKeyPitchClassSharpAndFlat(t *testing.T) {
	if got, ok := ParseKeyPitchClass("F#"); !ok || got != 6 {
		t.Errorf("expected F# -> 6, true, got %d, %v", got, ok)
	}
	if got, ok := ParseKeyPitchClass("Bb"); !ok || got != 10 {
		t.Errorf("expected Bb -> 10, true, got %d, %v", got, ok)
	}
}

// TestParseKeyPitchClassIgnoresMinor confirms a trailing "m" (see
// resolveKeyID's comment on this convention) is stripped without changing
// the pitch class - this is about root movement, not mode.
func TestParseKeyPitchClassIgnoresMinor(t *testing.T) {
	got, ok := ParseKeyPitchClass("Em")
	if !ok || got != 4 {
		t.Errorf("expected Em -> 4, true, got %d, %v", got, ok)
	}
}

func TestParseKeyPitchClassRejectsUnparseable(t *testing.T) {
	for _, key := range []string{"", "H", "Cx", "capo 2"} {
		if _, ok := ParseKeyPitchClass(key); ok {
			t.Errorf("expected %q to fail to parse, but it did", key)
		}
	}
}

func TestSemitoneDistanceWrapsAround(t *testing.T) {
	// B (11) to C (0) is a half step going up through the octave boundary,
	// not an 11-semitone gap.
	if got := SemitoneDistance(11, 0); got != 1 {
		t.Errorf("expected B->C to be a 1-semitone distance, got %d", got)
	}
}

// TestSignedSemitoneDeltaBothDirections confirms the sign flips with the
// direction of travel while the magnitude matches SemitoneDistance - C to G
// is shorter going down 5 (not up 7), G to C is shorter going up 5 (not
// down 7).
func TestSignedSemitoneDeltaBothDirections(t *testing.T) {
	if got := SignedSemitoneDelta(0, 7); got != -5 {
		t.Errorf("expected C->G to be -5, got %d", got)
	}
	if got := SignedSemitoneDelta(7, 0); got != 5 {
		t.Errorf("expected G->C to be +5, got %d", got)
	}
}

// TestSignedSemitoneDeltaWrapsAround mirrors
// TestSemitoneDistanceWrapsAround, but confirms the sign is also correct
// across the octave boundary: B up to C is +1 (a half step up), C down to
// B is -1 (a half step down), not an 11-semitone jump either way.
func TestSignedSemitoneDeltaWrapsAround(t *testing.T) {
	if got := SignedSemitoneDelta(11, 0); got != 1 {
		t.Errorf("expected B->C to be +1, got %d", got)
	}
	if got := SignedSemitoneDelta(0, 11); got != -1 {
		t.Errorf("expected C->B to be -1, got %d", got)
	}
}

// TestSignedSemitoneDeltaMagnitudeMatchesDistance confirms
// |SignedSemitoneDelta| always equals SemitoneDistance, for every pair of
// pitch classes - the two functions describe the same shortest path, just
// with and without direction.
func TestSignedSemitoneDeltaMagnitudeMatchesDistance(t *testing.T) {
	for a := 0; a < 12; a++ {
		for b := 0; b < 12; b++ {
			delta := SignedSemitoneDelta(a, b)
			mag := delta
			if mag < 0 {
				mag = -mag
			}
			if want := SemitoneDistance(a, b); mag != want {
				t.Errorf("SignedSemitoneDelta(%d, %d) = %d, magnitude %d != SemitoneDistance %d", a, b, delta, mag, want)
			}
		}
	}
}

// TestParseNote mirrors internal/server/controllers/vocal_range_test.go's
// existing TestParseVocalNote cases exactly (that controller-level parser
// now just wraps this one - see vocal_range.go's parseVocalNote).
func TestParseNote(t *testing.T) {
	tests := []struct {
		name           string
		note           string
		wantPitchClass int
		wantOctave     int
		wantOK         bool
	}{
		{name: "bare natural", note: "G3", wantPitchClass: 7, wantOctave: 3, wantOK: true},
		{name: "low octave", note: "C0", wantPitchClass: 0, wantOctave: 0, wantOK: true},
		{name: "high octave", note: "B8", wantPitchClass: 11, wantOctave: 8, wantOK: true},
		{name: "sharp ascii", note: "C#4", wantPitchClass: 1, wantOctave: 4, wantOK: true},
		{name: "sharp unicode", note: "F♯2", wantPitchClass: 6, wantOctave: 2, wantOK: true},
		{name: "flat ascii", note: "Bb2", wantPitchClass: 10, wantOctave: 2, wantOK: true},
		{name: "flat unicode", note: "E♭5", wantPitchClass: 3, wantOctave: 5, wantOK: true},
		{name: "lowercase letter", note: "d4", wantPitchClass: 2, wantOctave: 4, wantOK: true},
		{name: "surrounding whitespace", note: "  A3  ", wantPitchClass: 9, wantOctave: 3, wantOK: true},
		{name: "empty", note: "", wantOK: false},
		{name: "no octave", note: "G", wantOK: false},
		{name: "invalid letter", note: "H4", wantOK: false},
		{name: "double accidental", note: "C##4", wantOK: false},
		{name: "octave below range", note: "C-1", wantOK: false},
		{name: "octave above range", note: "C9", wantOK: false},
		{name: "not a note at all", note: "capo 2", wantOK: false},
		{name: "trailing minor-style suffix rejected", note: "G3m", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPC, gotOct, ok := ParseNote(tt.note)
			if ok != tt.wantOK {
				t.Fatalf("ParseNote(%q) ok = %v, want %v", tt.note, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotPC != tt.wantPitchClass || gotOct != tt.wantOctave {
				t.Errorf("ParseNote(%q) = (%d, %d), want (%d, %d)", tt.note, gotPC, gotOct, tt.wantPitchClass, tt.wantOctave)
			}
		})
	}
}

// TestAbsoluteSemitoneOrdersRegisterCorrectly mirrors
// internal/server/controllers/vocal_range_test.go's existing
// TestAbsoluteVocalSemitoneOrdersRegisterCorrectly.
func TestAbsoluteSemitoneOrdersRegisterCorrectly(t *testing.T) {
	// A low G (G3) must sit below a high G (G5) once combined into one
	// absolute value - the entire reason a bare pitch class alone isn't
	// enough to store a vocal range.
	lowG := AbsoluteSemitone(7, 3)
	highG := AbsoluteSemitone(7, 5)
	if lowG >= highG {
		t.Errorf("expected G3 (%d) < G5 (%d)", lowG, highG)
	}

	// C4 should sit exactly one semitone below C#4.
	c4 := AbsoluteSemitone(0, 4)
	cSharp4 := AbsoluteSemitone(1, 4)
	if cSharp4-c4 != 1 {
		t.Errorf("expected C#4 - C4 == 1, got %d", cSharp4-c4)
	}
}

// TestPitchClassAndOctaveInvertsAbsoluteSemitone confirms
// PitchClassAndOctave round-trips AbsoluteSemitone for both ordinary and
// negative inputs (a transposition can land below octave 0).
func TestPitchClassAndOctaveInvertsAbsoluteSemitone(t *testing.T) {
	tests := []struct {
		pitchClass, octave int
	}{
		{0, 4}, {7, 3}, {11, 0}, {1, 8}, {7, -1}, {0, -3},
	}

	for _, tt := range tests {
		abs := AbsoluteSemitone(tt.pitchClass, tt.octave)
		gotPC, gotOct := PitchClassAndOctave(abs)
		if gotPC != tt.pitchClass || gotOct != tt.octave {
			t.Errorf("PitchClassAndOctave(AbsoluteSemitone(%d, %d)=%d) = (%d, %d), want (%d, %d)",
				tt.pitchClass, tt.octave, abs, gotPC, gotOct, tt.pitchClass, tt.octave)
		}
	}
}
