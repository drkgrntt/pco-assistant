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

	"pco-assistant/internal/scheduling"
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

	got := buildBrowsableSongs(songs, inSet, nil, nil, nil, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 browsable song, got %d: %+v", len(got), got)
	}
	if got[0].SongID != "1" || got[0].Title != "Amazing Grace" {
		t.Errorf("expected the not-yet-added song, got %+v", got[0])
	}
}

// TestBuildBrowsableSongsSkipsExpensiveFields guards against this ever
// growing back into the *full* rich CandidateSongView shape - conflicts,
// repeat/reinforcement status, and arrangement info stay unset even when
// nothing filters them out, since those need real per-song computation
// this list is deliberately cheap enough to avoid (see its own doc
// comment). NotSungInLabel/Roles/Tags are the three fields this
// deliberately does populate - see
// TestBuildBrowsableSongsPopulatesRolesAndTags below.
func TestBuildBrowsableSongsSkipsExpensiveFields(t *testing.T) {
	songs := []pco.SongData{songDataItem("1", "Amazing Grace")}

	got := buildBrowsableSongs(songs, nil, nil, nil, nil, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 browsable song, got %d", len(got))
	}
	cand := got[0]
	if cand.SongID != "1" || cand.Title != "Amazing Grace" {
		t.Errorf("expected id/title to be set, got %+v", cand)
	}
	if cand.HasExtraMeta() {
		t.Errorf("expected no conflict/repeat/reinforcement/arrangement/context data, got %+v", cand)
	}
}

// TestBuildBrowsableSongsPopulatesRolesAndTags confirms the three fields
// this list does deliberately compute - not-sung-recently, roles, and tags
// (docs/issues.md: noticing these without clicking into a song) - actually
// get threaded through from the bulk lookup maps.
func TestBuildBrowsableSongsPopulatesRolesAndTags(t *testing.T) {
	songs := []pco.SongData{songDataItem("1", "Amazing Grace")}
	rolesBySong := map[string][]scheduling.SongRole{"1": {{Role: "opener"}}}
	roleTypeLabels := map[string]string{"opener": "Opener"}
	tagsBySong := map[string][]scheduling.SongTag{"1": {{Category: "theme", Value: "Hope"}}}
	tagCategoryLabels := map[string]string{"theme": "Theme"}

	got := buildBrowsableSongs(songs, nil, rolesBySong, roleTypeLabels, tagsBySong, tagCategoryLabels)

	if len(got) != 1 {
		t.Fatalf("expected 1 browsable song, got %d", len(got))
	}
	cand := got[0]
	if len(cand.Roles) != 1 || cand.Roles[0].Label != "Opener" {
		t.Errorf("expected the role to be resolved, got %+v", cand.Roles)
	}
	if len(cand.Tags) != 1 || cand.Tags[0].Value != "Hope" {
		t.Errorf("expected the tag to be resolved, got %+v", cand.Tags)
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

	got := buildBrowsableSongs(songs, nil, nil, nil, nil, nil)

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
