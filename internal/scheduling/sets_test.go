package scheduling

import "testing"

func TestComputeRecommendationsEmptyRelsReturnsNoSuggestions(t *testing.T) {
	got := computeRecommendations(nil, "last", map[string]bool{})
	if len(got) != 0 {
		t.Errorf("expected no suggestions for no relationships, got %+v", got)
	}
}

func TestComputeRecommendationsMatchesWhenLastSongIsSongIDA(t *testing.T) {
	rels := []SongRelationship{
		{SongIDA: "last", SongIDB: "other", RelationshipType: RelationshipFlowsWell},
	}

	got := computeRecommendations(rels, "last", map[string]bool{})

	if len(got) != 1 || got[0].SongID != "other" {
		t.Errorf("expected a suggestion for %q, got %+v", "other", got)
	}
}

func TestComputeRecommendationsMatchesWhenLastSongIsSongIDB(t *testing.T) {
	rels := []SongRelationship{
		{SongIDA: "other", SongIDB: "last", RelationshipType: RelationshipSameTheme},
	}

	got := computeRecommendations(rels, "last", map[string]bool{})

	if len(got) != 1 || got[0].SongID != "other" {
		t.Errorf("expected a suggestion for %q, got %+v", "other", got)
	}
}

// TestComputeRecommendationsExcludesAvoidPairing confirms only flows_well/
// same_theme relationships produce a suggestion - an avoid_pairing
// relationship with the last song must never show up as a "recommended
// next" pick.
func TestComputeRecommendationsExcludesAvoidPairing(t *testing.T) {
	rels := []SongRelationship{
		{SongIDA: "last", SongIDB: "other", RelationshipType: RelationshipAvoidPairing},
	}

	got := computeRecommendations(rels, "last", map[string]bool{})

	if len(got) != 0 {
		t.Errorf("expected avoid_pairing to be excluded, got %+v", got)
	}
}

func TestComputeRecommendationsExcludesSongsAlreadyInSet(t *testing.T) {
	rels := []SongRelationship{
		{SongIDA: "last", SongIDB: "other", RelationshipType: RelationshipFlowsWell},
	}
	inSet := map[string]bool{"other": true}

	got := computeRecommendations(rels, "last", inSet)

	if len(got) != 0 {
		t.Errorf("expected songs already in the set to be excluded, got %+v", got)
	}
}

// TestComputeRecommendationsAccumulatesRelationshipTypesAndNotes confirms
// multiple relationship rows pointing at the same other song merge into
// one suggestion, and an empty Notes value is not appended.
func TestComputeRecommendationsAccumulatesRelationshipTypesAndNotes(t *testing.T) {
	rels := []SongRelationship{
		{SongIDA: "last", SongIDB: "other", RelationshipType: RelationshipFlowsWell, Notes: "great transition"},
		{SongIDA: "other", SongIDB: "last", RelationshipType: RelationshipSameTheme, Notes: ""},
	}

	got := computeRecommendations(rels, "last", map[string]bool{})

	if len(got) != 1 {
		t.Fatalf("expected exactly one suggestion (deduped), got %+v", got)
	}
	sug := got[0]
	if len(sug.RelationshipTypes) != 2 {
		t.Errorf("expected both relationship types accumulated, got %v", sug.RelationshipTypes)
	}
	if len(sug.Notes) != 1 || sug.Notes[0] != "great transition" {
		t.Errorf("expected only the non-empty note to be appended, got %v", sug.Notes)
	}
}

func TestComputeAvoidPairingsEmptySetSongsReturnsNothing(t *testing.T) {
	candidates, existing := computeAvoidPairings(nil, map[string][]SongRelationship{})
	if candidates != nil || existing != nil {
		t.Errorf("expected zero values for an empty set, got candidates=%v existing=%v", candidates, existing)
	}
}

func TestComputeAvoidPairingsCandidateWhenOtherSongIsSongIDA(t *testing.T) {
	setSongs := []SetSong{{SongID: "in-set"}}
	relsBySong := map[string][]SongRelationship{
		"in-set": {{SongIDA: "outside", SongIDB: "in-set", RelationshipType: RelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairings(setSongs, relsBySong)

	if len(existing) != 0 {
		t.Errorf("expected no existing conflicts, got %v", existing)
	}
	if got := candidates["outside"]; len(got) != 1 || got[0] != "in-set" {
		t.Errorf("expected candidates[outside] = [in-set], got %v", candidates)
	}
}

func TestComputeAvoidPairingsCandidateWhenOtherSongIsSongIDB(t *testing.T) {
	setSongs := []SetSong{{SongID: "in-set"}}
	relsBySong := map[string][]SongRelationship{
		"in-set": {{SongIDA: "in-set", SongIDB: "outside", RelationshipType: RelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairings(setSongs, relsBySong)

	if len(existing) != 0 {
		t.Errorf("expected no existing conflicts, got %v", existing)
	}
	if got := candidates["outside"]; len(got) != 1 || got[0] != "in-set" {
		t.Errorf("expected candidates[outside] = [in-set], got %v", candidates)
	}
}

// TestComputeAvoidPairingsExcludesNonAvoidPairingTypes confirms a
// flows_well relationship never shows up as an avoid-pairing conflict.
func TestComputeAvoidPairingsExcludesNonAvoidPairingTypes(t *testing.T) {
	setSongs := []SetSong{{SongID: "a"}}
	relsBySong := map[string][]SongRelationship{
		"a": {{SongIDA: "a", SongIDB: "b", RelationshipType: RelationshipFlowsWell}},
	}

	candidates, existing := computeAvoidPairings(setSongs, relsBySong)

	if len(candidates) != 0 || len(existing) != 0 {
		t.Errorf("expected flows_well to produce no conflicts, got candidates=%v existing=%v", candidates, existing)
	}
}

// TestComputeAvoidPairingsExistingDedupsRegardlessOfStoredDirection
// confirms a pairing where both songs are already in the set is reported
// exactly once in existing, whether the relationship row stores the pair
// as (a, b) or (b, a).
func TestComputeAvoidPairingsExistingDedupsRegardlessOfStoredDirection(t *testing.T) {
	setSongs := []SetSong{{SongID: "song-a"}, {SongID: "song-b"}}

	// Stored as song_id_a="song-b", song_id_b="song-a" - the "wrong" (b, a)
	// order - to exercise the a > b swap.
	relsBySong := map[string][]SongRelationship{
		"song-a": {{SongIDA: "song-b", SongIDB: "song-a", RelationshipType: RelationshipAvoidPairing}},
		"song-b": {{SongIDA: "song-b", SongIDB: "song-a", RelationshipType: RelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairings(setSongs, relsBySong)

	if len(candidates) != 0 {
		t.Errorf("expected no candidates when both songs are already in the set, got %v", candidates)
	}
	if len(existing) != 1 {
		t.Fatalf("expected exactly one deduped existing conflict, got %+v", existing)
	}
	if existing[0].SongID != "song-a" || existing[0].ConflictsWith != "song-b" {
		t.Errorf("expected canonical order (song-a, song-b), got %+v", existing[0])
	}
}

// TestComputeAvoidPairingsExistingDedupsOtherDirection is the same as
// above but with the relationship stored in the opposite (a, b) order, to
// confirm both stored orders converge on the same single, canonically
// ordered result.
func TestComputeAvoidPairingsExistingDedupsOtherDirection(t *testing.T) {
	setSongs := []SetSong{{SongID: "song-a"}, {SongID: "song-b"}}

	relsBySong := map[string][]SongRelationship{
		"song-a": {{SongIDA: "song-a", SongIDB: "song-b", RelationshipType: RelationshipAvoidPairing}},
		"song-b": {{SongIDA: "song-a", SongIDB: "song-b", RelationshipType: RelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairings(setSongs, relsBySong)

	if len(candidates) != 0 {
		t.Errorf("expected no candidates when both songs are already in the set, got %v", candidates)
	}
	if len(existing) != 1 {
		t.Fatalf("expected exactly one deduped existing conflict, got %+v", existing)
	}
	if existing[0].SongID != "song-a" || existing[0].ConflictsWith != "song-b" {
		t.Errorf("expected canonical order (song-a, song-b), got %+v", existing[0])
	}
}
