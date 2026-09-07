package controllers

// Unit tests for the transition-notes resurfacing matching logic
// (transitionNotePrompts, latestTransitionNotesByKey, transitionNotesBySong
// - transition_notes.go). All pure functions over already-in-hand data,
// same as keyTransitionWarnings/mergeServiceOrder, so they're tested
// directly here rather than through buildSetBuilderData - see
// sets_builder_warnings_test.go's own package doc comment for the same
// convention applied to keyTransitionWarnings.

import (
	"strings"
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func transitionNote(kind, songIDA, songIDB, note string) scheduling.TransitionNote {
	return scheduling.TransitionNote{Kind: kind, SongIDA: songIDA, SongIDB: songIDB, Note: note}
}

func TestLatestTransitionNotesByKeyKeepsOnlyNewestPerKey(t *testing.T) {
	// Ordered newest-first, as ListTransitionNotesForSong actually returns
	// them - the first "updated wording" row must win over the older one
	// for the same key.
	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteTalkingPoint, "song-a", "song-b", "updated wording"),
		transitionNote(scheduling.TransitionNoteTalkingPoint, "song-a", "song-b", "old wording"),
	}

	latest := latestTransitionNotesByKey(notes)
	key := transitionNoteKey(scheduling.TransitionNoteTalkingPoint, "song-a", "song-b")
	got, ok := latest[key]
	if !ok {
		t.Fatal("expected a latest entry for the song-a/song-b talking_point key")
	}
	if got.Note != "updated wording" {
		t.Errorf("expected the newest row to win, got %q", got.Note)
	}
	if len(latest) != 1 {
		t.Errorf("expected exactly 1 key in the map, got %d", len(latest))
	}
}

func TestTransitionNotesBySongIndexesBothSidesOfAPair(t *testing.T) {
	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteDebrief, "song-a", "song-b", "abrupt into B"),
	}

	bySong := transitionNotesBySong(notes)
	if len(bySong["song-a"]) != 1 {
		t.Errorf("expected song-a's own list to contain the pair note, got %d", len(bySong["song-a"]))
	}
	if len(bySong["song-b"]) != 1 {
		t.Errorf("expected song-b's own list to also contain the pair note, got %d", len(bySong["song-b"]))
	}
	if len(bySong["song-c"]) != 0 {
		t.Errorf("expected an unrelated song to have no notes, got %d", len(bySong["song-c"]))
	}
}

func TestTransitionNotesBySongSingleSongOnlyIndexesOneSide(t *testing.T) {
	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteTalkingPoint, "song-a", "", "say this before it starts"),
	}

	bySong := transitionNotesBySong(notes)
	if len(bySong["song-a"]) != 1 {
		t.Errorf("expected song-a to have the single-song note, got %d", len(bySong["song-a"]))
	}
	if _, ok := bySong[""]; ok {
		t.Error("expected no entry keyed by the empty string")
	}
}

// TestTransitionNotePromptsSurfacesSingleSongNoteAnywhereInSet confirms a
// single-song note surfaces as soon as that song is anywhere in the
// order - no adjacency required, unlike a pair note.
func TestTransitionNotePromptsSurfacesSingleSongNoteAnywhereInSet(t *testing.T) {
	other := setSong("song-other", "", 0)
	other.ID = 1
	target := setSong("song-a", "", 1)
	target.ID = 2
	rows := []mergedRow{{Song: &other}, {Song: &target}}

	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteDebrief, "song-a", "", "ran long last time"),
	}
	latest := latestTransitionNotesByKey(notes)

	got := transitionNotePrompts(rows, latest, map[string]string{})
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 prompt, got %d: %+v", len(got), got)
	}
	if got[0].SongRowID != target.ID {
		t.Errorf("expected the prompt to attach to song-a's own row (%d), got %d", target.ID, got[0].SongRowID)
	}
	if got[0].Kind != scheduling.TransitionNoteDebrief {
		t.Errorf("expected kind %q, got %q", scheduling.TransitionNoteDebrief, got[0].Kind)
	}
	if !strings.Contains(got[0].Message, "ran long last time") {
		t.Errorf("expected the message to carry the note text, got %q", got[0].Message)
	}
}

// TestTransitionNotePromptsSurfacesPairOnlyWhenTrulyAdjacent confirms a
// pair note surfaces only once the two songs are truly back-to-back, and
// attaches to the *second* song's row - mirroring
// TestKeyTransitionWarningsFlagsHalfStep's own placement assertion
// (sets_builder_warnings_test.go).
func TestTransitionNotePromptsSurfacesPairOnlyWhenTrulyAdjacent(t *testing.T) {
	a := setSong("song-a", "", 0)
	a.ID = 1
	b := setSong("song-b", "", 1)
	b.ID = 2
	rows := []mergedRow{{Song: &a}, {Song: &b}}

	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteTalkingPoint, "song-a", "song-b", "hit the V7 here"),
	}
	latest := latestTransitionNotesByKey(notes)
	titles := map[string]string{"song-a": "Song A", "song-b": "Song B"}

	got := transitionNotePrompts(rows, latest, titles)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 prompt, got %d: %+v", len(got), got)
	}
	if got[0].SongRowID != b.ID {
		t.Errorf("expected the prompt to attach to the second song's row (%d), got %d", b.ID, got[0].SongRowID)
	}
	if !strings.Contains(got[0].Message, "Song A") || !strings.Contains(got[0].Message, "Song B") {
		t.Errorf("expected the message to name both songs, got %q", got[0].Message)
	}
}

// TestTransitionNotePromptsReverseOrderDoesNotMatch confirms direction is
// respected: a note entered A-into-B must not surface when B happens to
// precede A instead - the opposite of song_relationships' undirected
// pairing (see this package's own doc comment on why).
func TestTransitionNotePromptsReverseOrderDoesNotMatch(t *testing.T) {
	b := setSong("song-b", "", 0)
	b.ID = 1
	a := setSong("song-a", "", 1)
	a.ID = 2
	// b, then a - the reverse of the note's own A-into-B direction.
	rows := []mergedRow{{Song: &b}, {Song: &a}}

	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteDebrief, "song-a", "song-b", "A into B felt abrupt"),
	}
	latest := latestTransitionNotesByKey(notes)

	got := transitionNotePrompts(rows, latest, map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected no prompt when the pair appears in reverse order, got %+v", got)
	}
}

// TestTransitionNotePromptsSkipsWhenForeignItemBetween confirms two songs
// with a foreign item between them (mirroring
// TestKeyTransitionWarningsSkipsWhenForeignItemBetween) aren't treated as
// adjacent for a pair note either.
func TestTransitionNotePromptsSkipsWhenForeignItemBetween(t *testing.T) {
	a := setSong("song-a", "", 0)
	a.ID = 1
	b := setSong("song-b", "i1", 0)
	b.ID = 2
	rows := []mergedRow{
		{Song: &a},
		{Item: &pco.ItemData{ID: "i1", Attributes: pco.ItemAttributes{Title: "Offering"}}},
		{Song: &b},
	}

	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteDebrief, "song-a", "song-b", "should not surface"),
	}
	latest := latestTransitionNotesByKey(notes)

	got := transitionNotePrompts(rows, latest, map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected no prompt when a foreign item sits between the two songs, got %+v", got)
	}
}

// TestTransitionNotePromptsBothKindsCanSurfaceTogether confirms a debrief
// and a talking point on the exact same key both surface as independent
// prompts - the plan's own "kind so debrief and talking-point don't
// collide" framing.
func TestTransitionNotePromptsBothKindsCanSurfaceTogether(t *testing.T) {
	target := setSong("song-a", "", 0)
	target.ID = 1
	rows := []mergedRow{{Song: &target}}

	notes := []scheduling.TransitionNote{
		transitionNote(scheduling.TransitionNoteDebrief, "song-a", "", "debrief text"),
		transitionNote(scheduling.TransitionNoteTalkingPoint, "song-a", "", "talking point text"),
	}
	latest := latestTransitionNotesByKey(notes)

	got := transitionNotePrompts(rows, latest, map[string]string{})
	if len(got) != 2 {
		t.Fatalf("expected both kinds to surface as independent prompts, got %d: %+v", len(got), got)
	}
}
