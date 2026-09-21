package controllers

// Unit tests for song_library.go's own pure helper functions
// (findSongByID, songsExcluding, songRolesBySongID) - all pure functions
// over already-in-hand data, tested directly here rather than through
// songLibraryData/songDetailData, same convention
// transition_notes_test.go's own package doc comment describes for
// transitionNotePrompts.

import (
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// songDataItem (pco.SongData constructor) is defined in
// sets_builder_candidates_test.go - reused here rather than duplicated.

func songRole(id int64, songID, role string) scheduling.SongRole {
	return scheduling.SongRole{ID: id, SongID: songID, Role: role}
}

func TestFindSongByIDFound(t *testing.T) {
	songs := []pco.SongData{songDataItem("song-a", "Song A"), songDataItem("song-b", "Song B")}

	got, ok := findSongByID(songs, "song-b")
	if !ok {
		t.Fatal("expected song-b to be found")
	}
	if got.Attributes.Title != "Song B" {
		t.Errorf("expected title %q, got %q", "Song B", got.Attributes.Title)
	}
}

func TestFindSongByIDNotFound(t *testing.T) {
	songs := []pco.SongData{songDataItem("song-a", "Song A")}

	_, ok := findSongByID(songs, "song-missing")
	if ok {
		t.Error("expected song-missing not to be found")
	}
}

func TestFindSongByIDEmptyList(t *testing.T) {
	_, ok := findSongByID(nil, "song-a")
	if ok {
		t.Error("expected no match against an empty song list")
	}
}

func TestSongsExcludingRemovesOnlyTheGivenID(t *testing.T) {
	songs := []pco.SongData{songDataItem("song-a", "Song A"), songDataItem("song-b", "Song B"), songDataItem("song-c", "Song C")}

	got := songsExcluding(songs, "song-b")
	if len(got) != 2 {
		t.Fatalf("expected 2 songs remaining, got %d", len(got))
	}
	for _, s := range got {
		if s.ID == "song-b" {
			t.Error("expected song-b to be excluded")
		}
	}
}

func TestSongsExcludingIDNotPresentReturnsAllSongs(t *testing.T) {
	songs := []pco.SongData{songDataItem("song-a", "Song A"), songDataItem("song-b", "Song B")}

	got := songsExcluding(songs, "song-missing")
	if len(got) != 2 {
		t.Errorf("expected both songs to remain when excludeID isn't present, got %d", len(got))
	}
}

func TestSongsExcludingEmptyInput(t *testing.T) {
	got := songsExcluding(nil, "song-a")
	if len(got) != 0 {
		t.Errorf("expected an empty result for an empty input, got %d", len(got))
	}
}

func TestSongRolesBySongIDGroupsBySong(t *testing.T) {
	roles := []scheduling.SongRole{
		songRole(1, "song-a", "opener"),
		songRole(2, "song-a", "response"),
		songRole(3, "song-b", "opener"),
	}

	bySong := songRolesBySongID(roles)
	if len(bySong["song-a"]) != 2 {
		t.Errorf("expected 2 roles for song-a, got %d", len(bySong["song-a"]))
	}
	if len(bySong["song-b"]) != 1 {
		t.Errorf("expected 1 role for song-b, got %d", len(bySong["song-b"]))
	}
	if len(bySong["song-c"]) != 0 {
		t.Errorf("expected no roles for an unrelated song, got %d", len(bySong["song-c"]))
	}
}

func TestSongRolesBySongIDEmptyInput(t *testing.T) {
	bySong := songRolesBySongID(nil)
	if len(bySong) != 0 {
		t.Errorf("expected an empty map for empty input, got %d entries", len(bySong))
	}
}
