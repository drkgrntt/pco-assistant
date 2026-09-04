package controllers

// Pure matching/capping logic behind BuildCandidatesSearch (sets_builder.go)
// - the server-driven replacement for the "Browse all songs" list's old
// client-side filterCandidates() - plus buildBrowsableSongs, the same
// list's lightweight empty-state counterpart. candidateSongView/
// searchCandidates themselves need a live *scheduling.Store and PCO
// context, same as buildSetBuilderData, so - matching this package's
// existing test style (see sets_builder_warnings_test.go) - only the seams
// that don't need either are covered directly here.

import (
	"testing"

	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func TestCandidateMatchesSearchEmptyQueryMatchesEverything(t *testing.T) {
	if !candidateMatchesSearch("Amazing Grace", "grace hope", "") {
		t.Error("expected an empty query to match")
	}
}

func TestCandidateMatchesSearchMatchesTitleCaseInsensitive(t *testing.T) {
	if !candidateMatchesSearch("Amazing Grace", "", "amazing") {
		t.Error("expected a case-insensitive title match")
	}
}

func TestCandidateMatchesSearchMatchesTags(t *testing.T) {
	// tagsAttr is already lowercased by songTagViews - candidateMatchesSearch
	// doesn't need to lowercase it again, only the query and title.
	if !candidateMatchesSearch("Amazing Grace", "hope romans 8:1", "romans") {
		t.Error("expected a match against tag values, not just the title")
	}
}

func TestCandidateMatchesSearchNoMatch(t *testing.T) {
	if candidateMatchesSearch("Amazing Grace", "hope", "sermon") {
		t.Error("expected no match when neither title nor tags contain the query")
	}
}

func TestCandidateMatchesSearchTrimsWhitespace(t *testing.T) {
	if !candidateMatchesSearch("Amazing Grace", "", "  grace  ") {
		t.Error("expected leading/trailing whitespace in the query to be ignored")
	}
}

func TestCandidateMatchesRoleEmptyRoleMatchesEverything(t *testing.T) {
	if !candidateMatchesRole("opener", "") {
		t.Error("expected an empty role filter to match")
	}
	if !candidateMatchesRole("", "") {
		t.Error("expected an empty role filter to match a song with no roles at all")
	}
}

func TestCandidateMatchesRoleMatchesOneOfSeveral(t *testing.T) {
	if !candidateMatchesRole("opener response", "response") {
		t.Error("expected a match against one of several space-joined role codes")
	}
}

func TestCandidateMatchesRoleNoMatch(t *testing.T) {
	if candidateMatchesRole("opener", "response") {
		t.Error("expected no match when the role filter isn't among this song's roles")
	}
}

// TestCandidateMatchesRoleRejectsPrefixCollision guards the space-padded
// substring check itself: a naive strings.Contains(rolesAttr, role) would
// wrongly match "response" against a song only tagged "opener-response" or
// similar - the space padding on both sides is what prevents a role code
// from accidentally matching as a substring of a different one.
func TestCandidateMatchesRoleRejectsPrefixCollision(t *testing.T) {
	if candidateMatchesRole("responseteam", "response") {
		t.Error("expected role matching to require a whole space-separated code, not a substring")
	}
}

func TestCapCandidatesUnderLimitReturnsUnchanged(t *testing.T) {
	matches := []views.CandidateSongView{{SongID: "a"}, {SongID: "b"}}

	got, capped := capCandidates(matches, 5)

	if capped {
		t.Error("expected capped=false when matches is under the limit")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d", len(got))
	}
}

func TestCapCandidatesExactlyAtLimitIsNotCapped(t *testing.T) {
	matches := []views.CandidateSongView{{SongID: "a"}, {SongID: "b"}}

	got, capped := capCandidates(matches, 2)

	if capped {
		t.Error("expected capped=false when matches exactly equals the limit")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d", len(got))
	}
}

func songDataItem(id, title string) pco.SongData {
	return pco.SongData{ID: id, Attributes: pco.SongAttributes{Title: title}}
}

// TestBuildBrowsableSongsExcludesSongsAlreadyInSet mirrors searchCandidates'
// own inSet filter (sets_builder.go) - a song already in the draft set has
// nothing to add, so it shouldn't show up in the "browse titles" list
// either.
func TestBuildBrowsableSongsExcludesSongsAlreadyInSet(t *testing.T) {
	songs := []pco.SongData{
		songDataItem("1", "Amazing Grace"),
		songDataItem("2", "How Great Thou Art"),
	}
	inSet := map[string]bool{"2": true}

	got := buildBrowsableSongs(songs, inSet)

	if len(got) != 1 {
		t.Fatalf("expected 1 browsable song, got %d: %+v", len(got), got)
	}
	if got[0].SongID != "1" || got[0].Title != "Amazing Grace" {
		t.Errorf("expected the not-yet-added song, got %+v", got[0])
	}
}

// TestBuildBrowsableSongsIsTitleOnly guards against this ever growing back
// into the rich CandidateSongView shape - only an id and a title, the whole
// point of this being cheap enough to compute unconditionally (see its own
// doc comment).
func TestBuildBrowsableSongsIsTitleOnly(t *testing.T) {
	songs := []pco.SongData{songDataItem("1", "Amazing Grace")}

	got := buildBrowsableSongs(songs, nil)

	want := []views.SongOptionView{{SongID: "1", Title: "Amazing Grace"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// TestBuildBrowsableSongsPreservesOrder confirms this doesn't re-sort -
// fetchActiveSongs already returns songs title-ordered, so the browse list
// should reflect that order as-is.
func TestBuildBrowsableSongsPreservesOrder(t *testing.T) {
	songs := []pco.SongData{
		songDataItem("1", "Amazing Grace"),
		songDataItem("2", "Be Thou My Vision"),
		songDataItem("3", "Come Thou Fount"),
	}

	got := buildBrowsableSongs(songs, nil)

	if len(got) != 3 {
		t.Fatalf("expected 3 browsable songs, got %d", len(got))
	}
	for i, song := range songs {
		if got[i].SongID != song.ID || got[i].Title != song.Attributes.Title {
			t.Errorf("expected order to be preserved, got %+v at index %d", got[i], i)
		}
	}
}

func TestCapCandidatesOverLimitTrims(t *testing.T) {
	matches := []views.CandidateSongView{{SongID: "a"}, {SongID: "b"}, {SongID: "c"}}

	got, capped := capCandidates(matches, 2)

	if !capped {
		t.Error("expected capped=true when matches exceeds the limit")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].SongID != "a" || got[1].SongID != "b" {
		t.Errorf("expected the first 2 matches in order, got %+v", got)
	}
}
