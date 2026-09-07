package scheduling

import (
	"regexp"
	"strconv"
	"strings"
)

// This file is the "key engine" docs/plans/in-progress/vocal-range-plan.md's
// Phase 2 prerequisite section names: the pitch-class parsing/transposition
// primitives, factored out of internal/server/controllers/sets_builder.go
// (where keyPitchClasses/parseKeyPitchClass/semitoneDistance were inlined
// for the key-transition warning, their only consumer) now that song vocal
// range (Phase 2) is a second, storage-shaped consumer that also needs to
// *transpose* a stored interval, not just compare two bare keys. See that
// plan doc's "Phase 2 prerequisite: the key engine" section for the full
// reasoning on why now, not earlier.
//
// Explicitly NOT moved here: docs/plans/shipped/key-change-tip-plan.md's
// dominant-seventh spelling table/suggestedDominantSeventh helpers stay
// local to sets_builder.go, per that plan's own recommendation - they're
// transient, render-time-only, and not storage-shaped, unlike everything
// in this file.

// keyPitchClasses maps a note letter to its pitch class (C=0 .. B=11) -
// ParseKeyPitchClass's only lookup table. Kept unexported behind the
// exported parse function below, rather than exported itself - nothing
// outside this file needs the raw map, only the parse result.
var keyPitchClasses = map[string]int{
	"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11,
}

// ParseKeyPitchClass parses a key string ("Bb", "F#m", "G") into a 0-11
// pitch class, ignoring major/minor quality entirely - a trailing "m"
// (minor - see resolveKeyID's own comment on this convention, in
// internal/server/controllers/scheduling.go) is accepted but doesn't
// change the result, since root movement is what matters here, not mode.
// Returns ok=false for anything that isn't a bare note letter A-G
// optionally followed by a single "#"/"♯" or "b"/"♭" - good enough for what
// this app and Planning Center's own key picker actually produce, not a
// general music-notation parser. Key *names* never carry an octave in this
// app's existing data (PCO's own key picker doesn't offer one) - see
// ParseNote below for the separate, octave-aware note parser a vocal range
// needs.
func ParseKeyPitchClass(key string) (int, bool) {
	key = strings.TrimSpace(key)
	key = strings.TrimSuffix(key, "m")
	if key == "" {
		return 0, false
	}

	root, ok := keyPitchClasses[strings.ToUpper(key[:1])]
	if !ok {
		return 0, false
	}
	switch key[1:] {
	case "":
	case "#", "♯":
		root++
	case "b", "♭":
		root--
	default:
		return 0, false
	}
	return ((root % 12) + 12) % 12, true
}

// SemitoneDistance is the minimal circular distance (0-6) between two
// pitch classes - 1 either direction (e.g. E to F, or B to C) is a half
// step, the interval the key-transition warning
// (internal/server/controllers/sets_builder.go's keyTransitionWarnings)
// flags. Unsigned - it only ever answers "how far," not "which
// direction" - see SignedSemitoneDelta below for the signed version
// transposing a stored range needs.
func SemitoneDistance(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 6 {
		d = 12 - d
	}
	return d
}

// SignedSemitoneDelta is the minimal SIGNED shift (range -6..6) to move
// from pitch class fromPC to pitch class toPC - direction matters here,
// unlike SemitoneDistance's unsigned magnitude, because transposing a
// stored vocal range (see EffectiveVocalRange, song_vocal_range.go) needs
// to actually shift an absolute semitone value by the right number of
// semitones in the right direction, not just know how far apart two keys
// are. |SignedSemitoneDelta(a, b)| always equals SemitoneDistance(a, b) -
// this just picks the sign of the shorter path (a tie at exactly 6, a
// tritone, resolves to +6).
func SignedSemitoneDelta(fromPC, toPC int) int {
	d := ((toPC-fromPC)%12 + 12) % 12
	if d > 6 {
		d -= 12
	}
	return d
}

// noteRe matches a note-with-octave string like "C#4", "Bb2", "G3" - a
// note letter A-G, an optional single sharp/flat accidental (ASCII or
// Unicode), then a signed integer octave. Backs ParseNote below.
var noteRe = regexp.MustCompile(`^([A-Ga-g])(#|♯|b|♭)?(-?[0-9]+)$`)

// MinNoteOctave/MaxNoteOctave bound what ParseNote accepts - wide enough
// for any real human vocal range (the lowest bass notes sit around E1/E2,
// the highest sopranos/countertenors reach around C6/C7) without accepting
// garbage like "C99".
const (
	MinNoteOctave = 0
	MaxNoteOctave = 8
)

// ParseNote parses a note-with-octave string ("C#4", "Bb2", "G3") into a
// 0-11 pitch class and an octave, reusing ParseKeyPitchClass's letter +
// accidental parsing internally (a letter means the same pitch class
// either way - only the surrounding parse logic, a required trailing
// octave digit rather than nothing, is new). Returns ok=false for anything
// that isn't a note letter, an optional single accidental, then an integer
// octave within [MinNoteOctave, MaxNoteOctave].
//
// Extends ParseKeyPitchClass rather than replacing it - see that
// function's own doc comment on why a bare key name ("G") deliberately has
// no octave concept, while a vocal-range note ("G3") always needs one (a
// "G" alone doesn't say whether it's G3 or G5, which matters for a vocal
// range).
func ParseNote(s string) (pitchClass, octave int, ok bool) {
	s = strings.TrimSpace(s)
	m := noteRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}

	pc, ok := ParseKeyPitchClass(m[1] + m[2])
	if !ok {
		return 0, 0, false
	}

	octave, err := strconv.Atoi(m[3])
	if err != nil || octave < MinNoteOctave || octave > MaxNoteOctave {
		return 0, 0, false
	}

	return pc, octave, true
}

// AbsoluteSemitone combines a pitch class + octave into one
// internally-consistent absolute integer for comparison (octave*12 +
// pitch_class - no requirement to match real MIDI note numbering, just
// that two absolute values are directly comparable and subtractable), per
// docs/plans/in-progress/vocal-range-plan.md's storage-shape section.
func AbsoluteSemitone(pitchClass, octave int) int {
	return octave*12 + pitchClass
}

// PitchClassAndOctave is AbsoluteSemitone's inverse - given an absolute
// semitone value (which may be negative, e.g. from a transposition that
// lands below octave 0), recovers the 0-11 pitch class and the octave
// that combine back into it. Used to turn a computed effective vocal
// range (EffectiveVocalRange, song_vocal_range.go) back into a
// displayable note.
func PitchClassAndOctave(absoluteSemitone int) (pitchClass, octave int) {
	pitchClass = ((absoluteSemitone % 12) + 12) % 12
	octave = (absoluteSemitone - pitchClass) / 12
	return pitchClass, octave
}
