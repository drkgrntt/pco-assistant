package controllers

import "testing"

func TestParseVocalNote(t *testing.T) {
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
			gotPC, gotOct, ok := parseVocalNote(tt.note)
			if ok != tt.wantOK {
				t.Fatalf("parseVocalNote(%q) ok = %v, want %v", tt.note, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotPC != tt.wantPitchClass || gotOct != tt.wantOctave {
				t.Errorf("parseVocalNote(%q) = (%d, %d), want (%d, %d)", tt.note, gotPC, gotOct, tt.wantPitchClass, tt.wantOctave)
			}
		})
	}
}

func TestAbsoluteVocalSemitoneOrdersRegisterCorrectly(t *testing.T) {
	// A low G (G3) must sit below a high G (G5) once combined into one
	// absolute value - the entire reason a bare pitch class alone isn't
	// enough to store a vocal range (see the plan's storage-shape section).
	lowG := absoluteVocalSemitone(7, 3)
	highG := absoluteVocalSemitone(7, 5)
	if lowG >= highG {
		t.Errorf("expected G3 (%d) < G5 (%d)", lowG, highG)
	}

	// C4 should sit exactly one semitone below C#4.
	c4 := absoluteVocalSemitone(0, 4)
	cSharp4 := absoluteVocalSemitone(1, 4)
	if cSharp4-c4 != 1 {
		t.Errorf("expected C#4 - C4 == 1, got %d", cSharp4-c4)
	}
}

func TestFormatVocalNote(t *testing.T) {
	if got := formatVocalNote(7, 3); got != "G3" {
		t.Errorf("formatVocalNote(7, 3) = %q, want %q", got, "G3")
	}
	if got := formatVocalNote(1, 4); got != "C#4" {
		t.Errorf("formatVocalNote(1, 4) = %q, want %q", got, "C#4")
	}
}
