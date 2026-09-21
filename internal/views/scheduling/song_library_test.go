package scheduling

// Unit tests for song_library.go's own pure helper functions
// (songTagCountLabel, songLibraryCardSearchText) - the song library index
// page's card-text helpers.

import (
	"strings"
	"testing"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func TestSongTagCountLabelSingular(t *testing.T) {
	if got := songTagCountLabel(1); got != "1 label" {
		t.Errorf("expected %q, got %q", "1 label", got)
	}
}

func TestSongTagCountLabelPlural(t *testing.T) {
	if got := songTagCountLabel(3); got != "3 labels" {
		t.Errorf("expected %q, got %q", "3 labels", got)
	}
}

func TestSongTagCountLabelZero(t *testing.T) {
	if got := songTagCountLabel(0); got != "0 labels" {
		t.Errorf("expected %q, got %q", "0 labels", got)
	}
}

func TestSongLibraryCardSearchTextCombinesAllFacets(t *testing.T) {
	song := pco.SongData{ID: "song-a", Attributes: pco.SongAttributes{Title: "Amazing Grace"}}
	tags := []scheduling.SongTag{{Value: "Hope"}}
	contexts := []scheduling.SongContext{{ContextTypeID: 1}}
	contextLabels := map[int64]string{1: "Communion"}
	roles := []scheduling.SongRole{{Role: "opener"}}
	roleLabels := map[string]string{"opener": "Opener"}

	got := songLibraryCardSearchText(song, tags, contexts, contextLabels, roles, roleLabels)

	for _, want := range []string{"amazing grace", "hope", "communion", "opener"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected search text %q to contain %q", got, want)
		}
	}
}

func TestSongLibraryCardSearchTextIsLowercased(t *testing.T) {
	song := pco.SongData{ID: "song-a", Attributes: pco.SongAttributes{Title: "GREAT IS THY FAITHFULNESS"}}

	got := songLibraryCardSearchText(song, nil, nil, nil, nil, nil)
	if got != strings.ToLower(got) {
		t.Errorf("expected search text to be fully lowercased, got %q", got)
	}
}

func TestSongLibraryCardSearchTextNoFacetsIsJustTitle(t *testing.T) {
	song := pco.SongData{ID: "song-a", Attributes: pco.SongAttributes{Title: "Doxology"}}

	got := songLibraryCardSearchText(song, nil, nil, nil, nil, nil)
	if got != "doxology" {
		t.Errorf("expected %q, got %q", "doxology", got)
	}
}
