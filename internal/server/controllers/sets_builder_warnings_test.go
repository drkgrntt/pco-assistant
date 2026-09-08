package controllers

// Unit tests for the set builder's plan-level/positional warning checks:
// total service length vs. this service type's recent average
// (SetBuilderData.ServiceLengthWarning), a half-step/tritone key change
// between two back-to-back songs (keyTransitionWarnings - see
// docs/plans/shipped/in-place-set-warnings-plan.md for why this returns a
// structured []keyTransitionWarning rather than pre-formatted strings), the
// suggested-move tip appended to that same warning
// (suggestedDominantSeventh/suggestedKeyChangeMove - see
// docs/plans/shipped/key-change-tip-plan.md), and the vocal-range mismatch/
// octave-shift-tip check (vocalRangeWarningForSong/vocalRangeFitsShifted/
// vocalRangeViolationNotes - see docs/plans/in-progress/
// vocal-range-plan.md's Phase 3). All are pure functions over already-in-hand
// data, same as mergeServiceOrder/songsInBoth in sets_builder_test.go, so
// they're tested directly here rather than through buildSetBuilderData.

import (
	"strings"
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func TestTotalItemLengthSums(t *testing.T) {
	items := []pco.ItemData{
		{Attributes: pco.ItemAttributes{Length: 180}},
		{Attributes: pco.ItemAttributes{Length: 240}},
	}

	if got := totalItemLength(items); got != 420 {
		t.Errorf("expected 420, got %d", got)
	}
}

func TestServiceOrderLengthSecondsMixesForeignItemsAndSongs(t *testing.T) {
	rows := []mergedRow{
		{Item: &pco.ItemData{Attributes: pco.ItemAttributes{Length: 300}}}, // sermon, 5 min
	}
	ss := setSong("song-a", "", 0)
	rows = append(rows, mergedRow{Song: &ss})

	cache := map[string]scheduling.SongArrangementInfo{
		"song-a": {LengthSeconds: 240}, // 4 min
	}

	if got := serviceOrderLengthSeconds(rows, cache); got != 540 {
		t.Errorf("expected 540 (300+240), got %d", got)
	}
}

// TestServiceOrderLengthSecondsUncachedSongContributesZero confirms an
// uncached song undercounts rather than panicking on the missing map
// entry - same best-effort convention as ArrangementInfo elsewhere on this
// page.
func TestServiceOrderLengthSecondsUncachedSongContributesZero(t *testing.T) {
	ss := setSong("song-a", "", 0)
	rows := []mergedRow{{Song: &ss}}

	if got := serviceOrderLengthSeconds(rows, map[string]scheduling.SongArrangementInfo{}); got != 0 {
		t.Errorf("expected 0 for an uncached song, got %d", got)
	}
}

func TestServiceLengthWarningNoHistoryYieldsNoWarning(t *testing.T) {
	if got := serviceLengthWarning(3600, 0); got != "" {
		t.Errorf("expected no warning with no average to compare against, got %q", got)
	}
}

func TestServiceLengthWarningWithinNormalVarianceYieldsNoWarning(t *testing.T) {
	// 60 min average, 63 min actual - well within the 15%/3-minute band.
	if got := serviceLengthWarning(63*60, 60*60); got != "" {
		t.Errorf("expected no warning for a small variance, got %q", got)
	}
}

func TestServiceLengthWarningNoticeablyLonger(t *testing.T) {
	// 60 min average, 85 min actual - past both the 15% ratio and the
	// 3-minute floor.
	got := serviceLengthWarning(85*60, 60*60)
	if got == "" {
		t.Fatal("expected a warning for a noticeably longer plan, got none")
	}
	if !strings.Contains(got, "longer") {
		t.Errorf("expected the warning to say \"longer\", got %q", got)
	}
}

func TestServiceLengthWarningNoticeablyShorter(t *testing.T) {
	got := serviceLengthWarning(35*60, 60*60)
	if got == "" {
		t.Fatal("expected a warning for a noticeably shorter plan, got none")
	}
	if !strings.Contains(got, "shorter") {
		t.Errorf("expected the warning to say \"shorter\", got %q", got)
	}
}

// TestServiceLengthWarningFloorProtectsSmallAverages confirms the 3-minute
// floor, not just the 15% ratio, is actually in effect: a service type
// with a tiny recent average (20 min) shouldn't flag a 2-minute swing, even
// though 2 minutes is more than 15% of 20.
func TestServiceLengthWarningFloorProtectsSmallAverages(t *testing.T) {
	if got := serviceLengthWarning(22*60, 20*60); got != "" {
		t.Errorf("expected the 3-minute floor to absorb a 2-minute swing on a small average, got %q", got)
	}
}

// TestParseKeyPitchClass*/TestSemitoneDistanceWrapsAround relocated to
// internal/scheduling/key_engine_test.go - see
// docs/plans/in-progress/vocal-range-plan.md's "Phase 2 prerequisite: the
// key engine" section (pure relocation, no behavior change; the parser/
// distance functions they test now live as scheduling.ParseKeyPitchClass/
// scheduling.SemitoneDistance, called by keyTransitionWarnings below).

// TestKeyTransitionWarningsFlagsHalfStep also confirms the structured
// return value's SongRowID identifies the *second*, later song's own
// set_songs row id - not the first - per
// docs/plans/shipped/in-place-set-warnings-plan.md's resolved rendering
// decision (the badge renders on that later song's row).
func TestKeyTransitionWarningsFlagsHalfStep(t *testing.T) {
	e := setSong("song-e", "", 0)
	f := setSong("song-f", "", 1)
	f.ID = 42
	rows := []mergedRow{{Song: &e}, {Song: &f}}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-e": {Key: "E"},
		"song-f": {Key: "F"},
	}
	titles := map[string]string{"song-e": "Song E", "song-f": "Song F"}

	got := keyTransitionWarnings(rows, cache, titles)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 warning, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "Song E") || !strings.Contains(got[0].Message, "Song F") {
		t.Errorf("expected the warning message to name both songs, got %q", got[0].Message)
	}
	if got[0].Label == "" {
		t.Error("expected a non-empty short badge label")
	}
	if got[0].SongRowID != f.ID {
		t.Errorf("expected SongRowID to be the second song's row id (%d), got %d", f.ID, got[0].SongRowID)
	}
}

// TestKeyTransitionWarningsFlagsTritoneWithDistinctMessage confirms a
// tritone change (the other flagged interval, alongside the half-step case
// above) is worded distinctly from a half-step one.
func TestKeyTransitionWarningsFlagsTritoneWithDistinctMessage(t *testing.T) {
	c := setSong("song-c", "", 0)
	fs := setSong("song-fs", "", 1)
	rows := []mergedRow{{Song: &c}, {Song: &fs}}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-c":  {Key: "C"},
		"song-fs": {Key: "F#"},
	}

	got := keyTransitionWarnings(rows, cache, map[string]string{})
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 warning, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "tri-tone") {
		t.Errorf("expected the message to call this a tri-tone change, got %q", got[0].Message)
	}
	if !strings.Contains(got[0].Label, "tritone") {
		t.Errorf("expected the badge label to say tritone, got %q", got[0].Label)
	}
}

// TestKeyTransitionWarningsIgnoresSmoothInterval confirms a whole-step
// change (a common, intentional worship transition) doesn't get flagged
// alongside the half-step case above.
func TestKeyTransitionWarningsIgnoresSmoothInterval(t *testing.T) {
	g := setSong("song-g", "", 0)
	a := setSong("song-a", "", 1)
	rows := []mergedRow{{Song: &g}, {Song: &a}}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-g": {Key: "G"},
		"song-a": {Key: "A"},
	}

	got := keyTransitionWarnings(rows, cache, map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected no warning for a whole-step change, got %+v", got)
	}
}

// TestKeyTransitionWarningsSkipsWhenForeignItemBetween confirms two songs
// with a foreign item between them (a header, an announcement) aren't
// compared at all - a transition already exists there, so it's not "no
// transition" the way two truly back-to-back songs are.
func TestKeyTransitionWarningsSkipsWhenForeignItemBetween(t *testing.T) {
	e := setSong("song-e", "", 0)
	f := setSong("song-f", "i1", 0)
	rows := []mergedRow{
		{Song: &e},
		{Item: &pco.ItemData{ID: "i1", Attributes: pco.ItemAttributes{Title: "Offering"}}},
		{Song: &f},
	}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-e": {Key: "E"},
		"song-f": {Key: "F"},
	}

	got := keyTransitionWarnings(rows, cache, map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected no warning when a foreign item sits between the two songs, got %+v", got)
	}
}

// TestKeyTransitionWarningsSkipsUnparseableKey confirms a missing/odd key
// on either song just skips that pair rather than warning on a bogus
// distance.
func TestKeyTransitionWarningsSkipsUnparseableKey(t *testing.T) {
	e := setSong("song-e", "", 0)
	f := setSong("song-f", "", 1)
	rows := []mergedRow{{Song: &e}, {Song: &f}}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-e": {Key: ""},
		"song-f": {Key: "F"},
	}

	got := keyTransitionWarnings(rows, cache, map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected no warning when one song has no parseable key, got %+v", got)
	}
}

// TestSuggestedDominantSeventhCanonicalKeys covers all 17 canonical-key rows
// of docs/plans/shipped/key-change-tip-plan.md's worked table (all 12 pitch
// classes, both spellings where musicalKeyOptions' own datalist offers two)
// - the V7-of-destination chord suggestedKeyChangeMove/defaultKeyChangeMove
// suggest right before a flagged key change, independent of which interval
// (half-step or tritone) triggered it.
func TestSuggestedDominantSeventhCanonicalKeys(t *testing.T) {
	cases := []struct {
		destKey string
		want    string
	}{
		{"C", "G7"},
		{"C#", "G#7"},
		{"Db", "Ab7"},
		{"D", "A7"},
		{"D#", "A#7"},
		{"Eb", "Bb7"},
		{"E", "B7"},
		{"F", "C7"},
		{"F#", "C#7"},
		{"Gb", "Db7"},
		{"G", "D7"},
		{"G#", "D#7"},
		{"Ab", "Eb7"},
		{"A", "E7"},
		{"A#", "F7"},
		{"Bb", "F7"},
		{"B", "F#7"},
	}
	for _, tc := range cases {
		if got := suggestedDominantSeventh(tc.destKey); got != tc.want {
			t.Errorf("suggestedDominantSeventh(%q) = %q, want %q", tc.destKey, got, tc.want)
		}
	}
}

// TestSuggestedDominantSeventhNonCanonicalKeys confirms the formula degrades
// gracefully outside the 34 keys musicalKeyOptions' datalist actually lists
// - the key input is free-text, so an unlisted spelling can still reach
// this code. Both cases share a pitch class with a canonical key above (Cb
// == B, E# == F) but resolve differently: Cb's own flat spelling drives an
// ambiguous dominant to Gb7 (not the B-natural exception's F#7, since Cb
// isn't the bare-natural-letter case that exception exists for), and E#'s
// dominant (pitch class 0) isn't ambiguous at all, so it resolves the same
// as F's regardless of E#'s own sharp spelling.
func TestSuggestedDominantSeventhNonCanonicalKeys(t *testing.T) {
	if got := suggestedDominantSeventh("Cb"); got != "Gb7" {
		t.Errorf("suggestedDominantSeventh(%q) = %q, want %q", "Cb", got, "Gb7")
	}
	if got := suggestedDominantSeventh("E#"); got != "C7" {
		t.Errorf("suggestedDominantSeventh(%q) = %q, want %q", "E#", got, "C7")
	}
}

// TestSuggestedKeyChangeMoveFallsThroughToDefault confirms both intervals
// keyTransitionWarnings actually calls this with today (1: half-step, 6:
// tritone) fall through keyChangeTips (empty today) to
// defaultKeyChangeMove's V7-of-destination suggestion, and that the
// rendered sentence names both the chord and the destination key.
func TestSuggestedKeyChangeMoveFallsThroughToDefault(t *testing.T) {
	for _, distance := range []int{1, 6} {
		got := suggestedKeyChangeMove(distance, "Db")
		if !strings.Contains(got, "Ab7") {
			t.Errorf("suggestedKeyChangeMove(%d, %q) = %q, expected it to name the V7 chord Ab7", distance, "Db", got)
		}
		if !strings.Contains(got, "Db") {
			t.Errorf("suggestedKeyChangeMove(%d, %q) = %q, expected it to name the destination key Db", distance, "Db", got)
		}
	}
}

// TestKeyTransitionWarningsIncludesSuggestion confirms the suggestion text
// actually reaches keyTransitionWarnings' own returned Suggestion field
// (not just the standalone helpers above) for both flagged intervals.
func TestKeyTransitionWarningsIncludesSuggestion(t *testing.T) {
	g := setSong("song-g", "", 0)
	ab := setSong("song-ab", "", 1)
	rows := []mergedRow{{Song: &g}, {Song: &ab}}
	cache := map[string]scheduling.SongArrangementInfo{
		"song-g":  {Key: "G"},
		"song-ab": {Key: "Ab"},
	}

	got := keyTransitionWarnings(rows, cache, map[string]string{})
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 warning, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Suggestion, "Eb7") {
		t.Errorf("expected the suggestion to name Eb7 (the V7 of Ab), got %q", got[0].Suggestion)
	}
}

// --- Phase 3: vocal range mismatch/tip (docs/plans/in-progress/vocal-range-plan.md) ---

func TestVocalRangeWarningForSongNoVocalistsYieldsNoWarning(t *testing.T) {
	got := vocalRangeWarningForSong(50, 65, nil, map[string]scheduling.PersonVocalRange{}, map[string]string{})
	if got.Label != "" {
		t.Errorf("expected no warning with no roster vocalists at all, got %+v", got)
	}
}

func TestVocalRangeWarningForSongSkipsVocalistWithNoRangeOnFile(t *testing.T) {
	// "sam" is on the roster but has never had a range recorded - not a
	// vocalist to check at all, per the plan's own "who counts" rule.
	got := vocalRangeWarningForSong(50, 65, []string{"sam"}, map[string]scheduling.PersonVocalRange{}, map[string]string{})
	if got.Label != "" {
		t.Errorf("expected no warning for a roster member with no PersonVocalRange on file, got %+v", got)
	}
}

func TestVocalRangeWarningForSongFitsAsWrittenYieldsNoWarning(t *testing.T) {
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
	}
	// The song's own range (A3-D5, absolute 45-62) sits fully inside
	// Sarah's stored range - no mismatch.
	got := vocalRangeWarningForSong(45, 62, []string{"sarah"}, ranges, map[string]string{"sarah": "Sarah"})
	if got.Label != "" {
		t.Errorf("expected no warning when the song's range fits fully inside the vocalist's own, got %+v", got)
	}
}

func TestVocalRangeWarningForSongHardMismatchNamesTheReach(t *testing.T) {
	// Sarah's range is A3-D5 (absolute 45-62); the song needs up to F5
	// (absolute 65) - too high even shifted an octave either direction -
	// mirrors the plan's own worked example.
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5},
	}
	got := vocalRangeWarningForSong(50, 65, []string{"sarah"}, ranges, map[string]string{"sarah": "Sarah"})

	if got.Label != "range mismatch" {
		t.Errorf("expected Label %q, got %q", "range mismatch", got.Label)
	}
	if got.IsTip {
		t.Errorf("expected a hard mismatch (IsTip false), got IsTip true")
	}
	if !strings.Contains(got.Message, "Sarah") {
		t.Errorf("expected the message to name Sarah, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "A3-D5") {
		t.Errorf("expected the message to name Sarah's stored range A3-D5, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "F5") {
		t.Errorf("expected the message to name the specific violating note F5, got %q", got.Message)
	}
}

func TestVocalRangeWarningForSongOctaveShiftDowngradesToTip(t *testing.T) {
	ranges := map[string]scheduling.PersonVocalRange{
		"tom": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
	}
	// The song's own range is A4-D6 (absolute 57-74) - exactly Tom's own
	// range shifted a full octave up, so sung an octave lower it fits
	// perfectly.
	got := vocalRangeWarningForSong(57, 74, []string{"tom"}, ranges, map[string]string{"tom": "Tom"})

	if got.Label != "range tip" {
		t.Errorf("expected Label %q, got %q", "range tip", got.Label)
	}
	if !got.IsTip {
		t.Errorf("expected an octave-shift tip (IsTip true), got IsTip false")
	}
	if !strings.Contains(got.Message, "Tom") || !strings.Contains(got.Message, "lower") {
		t.Errorf("expected the message to name Tom and suggest singing it lower, got %q", got.Message)
	}
}

// TestVocalRangeWarningForSongHardMismatchWinsOverTip confirms that when
// one assigned vocalist has a real, unfixable mismatch and another's own
// issue is merely octave-shift-fixable, the whole row's badge renders as a
// warning (not a tip) - see vocalRangeWarningForSong's own doc comment on
// why a hard mismatch always wins - while both vocalists' own lines still
// appear in the combined message.
func TestVocalRangeWarningForSongHardMismatchWinsOverTip(t *testing.T) {
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5}, // A3-D5
		"tom":   {LowestPitchClass: 2, LowestOctave: 5, HighestPitchClass: 5, HighestOctave: 6}, // D5-F6
	}
	// Song range 50-65 (absolute): a hard mismatch against Sarah (needs up
	// to F5, no octave shift fixes it), but exactly Tom's own range shifted
	// up an octave (fits sung higher).
	got := vocalRangeWarningForSong(50, 65, []string{"sarah", "tom"}, ranges, map[string]string{"sarah": "Sarah", "tom": "Tom"})

	if got.Label != "range mismatch" || got.IsTip {
		t.Errorf("expected a hard mismatch verdict when any vocalist has one, got %+v", got)
	}
	if !strings.Contains(got.Message, "Sarah") {
		t.Errorf("expected the message to include Sarah's hard-mismatch line, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "Tom") {
		t.Errorf("expected the message to still include Tom's tip line, got %q", got.Message)
	}
}

// TestVocalRangeWarningForSongDedupesRepeatedPersonID confirms a person
// assigned to more than one roster position (so appearing twice in
// vocalistPersonIDs) is only ever checked, and only ever named in the
// message, once.
func TestVocalRangeWarningForSongDedupesRepeatedPersonID(t *testing.T) {
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {LowestPitchClass: 9, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5},
	}
	got := vocalRangeWarningForSong(50, 65, []string{"sarah", "sarah"}, ranges, map[string]string{"sarah": "Sarah"})

	if n := strings.Count(got.Message, "Sarah"); n != 2 {
		// Sarah's name is named twice within her own single sentence
		// ("Sarah's range is ...; ... needs Sarah to reach ...") - a
		// duplicated line (not deduped) would push this to 4.
		t.Errorf("expected Sarah's name to appear exactly twice (once per mention in her one line), got %d in %q", n, got.Message)
	}
}

func TestVocalRangeFitsShiftedDownOneOctave(t *testing.T) {
	fits, direction := vocalRangeFitsShifted(57, 74, 45, 62)
	if !fits || direction != "lower" {
		t.Errorf("expected fits=true direction=%q, got fits=%v direction=%q", "lower", fits, direction)
	}
}

func TestVocalRangeFitsShiftedUpOneOctave(t *testing.T) {
	fits, direction := vocalRangeFitsShifted(50, 65, 62, 77)
	if !fits || direction != "higher" {
		t.Errorf("expected fits=true direction=%q, got fits=%v direction=%q", "higher", fits, direction)
	}
}

func TestVocalRangeFitsShiftedNeitherDirectionFits(t *testing.T) {
	fits, _ := vocalRangeFitsShifted(50, 65, 45, 62)
	if fits {
		t.Errorf("expected fits=false when neither a one-octave-up nor one-octave-down shift fits")
	}
}

func TestVocalRangeViolationNotesLowOnly(t *testing.T) {
	got := vocalRangeViolationNotes(40, 60, 45, 62)
	want := []string{"down to E3"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestVocalRangeViolationNotesHighOnly(t *testing.T) {
	got := vocalRangeViolationNotes(50, 65, 45, 62)
	want := []string{"up to F5"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestVocalRangeViolationNotesBothBounds(t *testing.T) {
	got := vocalRangeViolationNotes(40, 70, 45, 62)
	want := []string{"down to E3", "up to A#5"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestSongEffectiveVocalRangeAbsMissingSongYieldsNotOK(t *testing.T) {
	_, _, ok := songEffectiveVocalRangeAbs(map[string]scheduling.SongVocalRange{}, "song-a", "G")
	if ok {
		t.Errorf("expected ok=false for a song with no stored vocal range")
	}
}

func TestVocalistsWithRangesOnFileFiltersAndDedupes(t *testing.T) {
	ranges := map[string]scheduling.PersonVocalRange{
		"sarah": {},
	}
	got := vocalistsWithRangesOnFile([]string{"sarah", "sarah", "tom", ""}, ranges)
	if len(got) != 1 || got[0] != "sarah" {
		t.Errorf("expected exactly [\"sarah\"] (deduped, and tom/\"\" excluded - no range on file), got %v", got)
	}
}
