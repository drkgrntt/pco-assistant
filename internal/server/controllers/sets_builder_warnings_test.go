package controllers

// Unit tests for the set builder's plan-level/positional warning checks:
// total service length vs. this service type's recent average
// (SetBuilderData.ServiceLengthWarning), a half-step/tritone key change
// between two back-to-back songs (keyTransitionWarnings - see
// docs/plans/shipped/in-place-set-warnings-plan.md for why this returns a
// structured []keyTransitionWarning rather than pre-formatted strings), and
// the suggested-move tip appended to that same warning
// (suggestedDominantSeventh/suggestedKeyChangeMove - see
// docs/plans/shipped/key-change-tip-plan.md). All are pure functions over
// already-in-hand data, same as mergeServiceOrder/songsInBoth in
// sets_builder_test.go, so they're tested directly here rather than through
// buildSetBuilderData.

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
