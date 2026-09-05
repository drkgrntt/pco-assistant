package controllers

// Unit tests for the set builder's plan-level/positional warning checks:
// total service length vs. this service type's recent average
// (SetBuilderData.ServiceLengthWarning), and a half-step/tritone key change
// between two back-to-back songs (keyTransitionWarnings - see
// docs/plans/shipped/in-place-set-warnings-plan.md for why this returns a
// structured []keyTransitionWarning rather than pre-formatted strings).
// Both checks are pure functions over already-in-hand data, same as
// mergeServiceOrder/songsInBoth in sets_builder_test.go, so they're tested
// directly here rather than through buildSetBuilderData.

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

func TestParseKeyPitchClassBareNote(t *testing.T) {
	got, ok := parseKeyPitchClass("G")
	if !ok || got != 7 {
		t.Errorf("expected G -> 7, true, got %d, %v", got, ok)
	}
}

func TestParseKeyPitchClassSharpAndFlat(t *testing.T) {
	if got, ok := parseKeyPitchClass("F#"); !ok || got != 6 {
		t.Errorf("expected F# -> 6, true, got %d, %v", got, ok)
	}
	if got, ok := parseKeyPitchClass("Bb"); !ok || got != 10 {
		t.Errorf("expected Bb -> 10, true, got %d, %v", got, ok)
	}
}

// TestParseKeyPitchClassIgnoresMinor confirms a trailing "m" (see
// resolveKeyID's comment on this convention) is stripped without changing
// the pitch class - this warning is about root movement, not mode.
func TestParseKeyPitchClassIgnoresMinor(t *testing.T) {
	got, ok := parseKeyPitchClass("Em")
	if !ok || got != 4 {
		t.Errorf("expected Em -> 4, true, got %d, %v", got, ok)
	}
}

func TestParseKeyPitchClassRejectsUnparseable(t *testing.T) {
	for _, key := range []string{"", "H", "Cx", "capo 2"} {
		if _, ok := parseKeyPitchClass(key); ok {
			t.Errorf("expected %q to fail to parse, but it did", key)
		}
	}
}

func TestSemitoneDistanceWrapsAround(t *testing.T) {
	// B (11) to C (0) is a half step going up through the octave boundary,
	// not an 11-semitone gap.
	if got := semitoneDistance(11, 0); got != 1 {
		t.Errorf("expected B->C to be a 1-semitone distance, got %d", got)
	}
}

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
