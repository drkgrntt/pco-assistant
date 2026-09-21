package scheduling

// Unit tests for song_detail.templ's own pure helper (songDetailOtherSongID)
// - which side of a canonicalized SongRelationship pair isn't the song
// whose own detail page is rendering it. See CreateSongRelationship's own
// doc comment (internal/scheduling/song_relationships.go) on why the pair
// is stored in lexicographic order rather than "entered" order, so a
// relationship can have either song on either side.

import (
	"testing"

	"pco-assistant/internal/scheduling"
)

func TestSongDetailOtherSongIDSongIsSideA(t *testing.T) {
	rel := scheduling.SongRelationship{SongIDA: "song-a", SongIDB: "song-b"}

	if got := songDetailOtherSongID(rel, "song-a"); got != "song-b" {
		t.Errorf("expected %q, got %q", "song-b", got)
	}
}

func TestSongDetailOtherSongIDSongIsSideB(t *testing.T) {
	rel := scheduling.SongRelationship{SongIDA: "song-a", SongIDB: "song-b"}

	if got := songDetailOtherSongID(rel, "song-b"); got != "song-a" {
		t.Errorf("expected %q, got %q", "song-a", got)
	}
}
