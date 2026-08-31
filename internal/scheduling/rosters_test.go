package scheduling

import "testing"

func TestComputeAvoidPairingConflictsEmptyAssignmentsReturnsNothing(t *testing.T) {
	candidates, existing := computeAvoidPairingConflicts(nil, map[string][]PersonRelationship{})
	if candidates != nil || existing != nil {
		t.Errorf("expected zero values for an empty roster, got candidates=%v existing=%v", candidates, existing)
	}
}

func TestComputeAvoidPairingConflictsCandidateWhenOtherPersonIsPersonIDA(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "on-roster"}}
	relsByPerson := map[string][]PersonRelationship{
		"on-roster": {{PersonIDA: "outside", PersonIDB: "on-roster", RelationshipType: PersonRelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairingConflicts(assignments, relsByPerson)

	if len(existing) != 0 {
		t.Errorf("expected no existing conflicts, got %v", existing)
	}
	if got := candidates["outside"]; len(got) != 1 || got[0] != "on-roster" {
		t.Errorf("expected candidates[outside] = [on-roster], got %v", candidates)
	}
}

func TestComputeAvoidPairingConflictsCandidateWhenOtherPersonIsPersonIDB(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "on-roster"}}
	relsByPerson := map[string][]PersonRelationship{
		"on-roster": {{PersonIDA: "on-roster", PersonIDB: "outside", RelationshipType: PersonRelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairingConflicts(assignments, relsByPerson)

	if len(existing) != 0 {
		t.Errorf("expected no existing conflicts, got %v", existing)
	}
	if got := candidates["outside"]; len(got) != 1 || got[0] != "on-roster" {
		t.Errorf("expected candidates[outside] = [on-roster], got %v", candidates)
	}
}

// TestComputeAvoidPairingConflictsExcludesNonAvoidPairingTypes confirms a
// works_well relationship never shows up as an avoid-pairing conflict.
func TestComputeAvoidPairingConflictsExcludesNonAvoidPairingTypes(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "a"}}
	relsByPerson := map[string][]PersonRelationship{
		"a": {{PersonIDA: "a", PersonIDB: "b", RelationshipType: PersonRelationshipWorksWell}},
	}

	candidates, existing := computeAvoidPairingConflicts(assignments, relsByPerson)

	if len(candidates) != 0 || len(existing) != 0 {
		t.Errorf("expected works_well to produce no conflicts, got candidates=%v existing=%v", candidates, existing)
	}
}

// TestComputeAvoidPairingConflictsExistingDedupsRegardlessOfStoredDirection
// confirms a pairing where both people are already on the roster is
// reported exactly once in existing, whether the relationship row stores
// the pair as (a, b) or (b, a).
func TestComputeAvoidPairingConflictsExistingDedupsRegardlessOfStoredDirection(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "person-a"}, {PersonID: "person-b"}}

	// Stored as person_id_a="person-b", person_id_b="person-a" - the
	// "wrong" (b, a) order - to exercise the a > b swap.
	relsByPerson := map[string][]PersonRelationship{
		"person-a": {{PersonIDA: "person-b", PersonIDB: "person-a", RelationshipType: PersonRelationshipAvoidPairing}},
		"person-b": {{PersonIDA: "person-b", PersonIDB: "person-a", RelationshipType: PersonRelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairingConflicts(assignments, relsByPerson)

	if len(candidates) != 0 {
		t.Errorf("expected no candidates when both people are already on the roster, got %v", candidates)
	}
	if len(existing) != 1 {
		t.Fatalf("expected exactly one deduped existing conflict, got %+v", existing)
	}
	if existing[0].PersonID != "person-a" || existing[0].ConflictsWith != "person-b" {
		t.Errorf("expected canonical order (person-a, person-b), got %+v", existing[0])
	}
}

// TestComputeAvoidPairingConflictsExistingDedupsOtherDirection is the same
// as above but with the relationship stored in the opposite (a, b) order,
// to confirm both stored orders converge on the same single, canonically
// ordered result.
func TestComputeAvoidPairingConflictsExistingDedupsOtherDirection(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "person-a"}, {PersonID: "person-b"}}

	relsByPerson := map[string][]PersonRelationship{
		"person-a": {{PersonIDA: "person-a", PersonIDB: "person-b", RelationshipType: PersonRelationshipAvoidPairing}},
		"person-b": {{PersonIDA: "person-a", PersonIDB: "person-b", RelationshipType: PersonRelationshipAvoidPairing}},
	}

	candidates, existing := computeAvoidPairingConflicts(assignments, relsByPerson)

	if len(candidates) != 0 {
		t.Errorf("expected no candidates when both people are already on the roster, got %v", candidates)
	}
	if len(existing) != 1 {
		t.Fatalf("expected exactly one deduped existing conflict, got %+v", existing)
	}
	if existing[0].PersonID != "person-a" || existing[0].ConflictsWith != "person-b" {
		t.Errorf("expected canonical order (person-a, person-b), got %+v", existing[0])
	}
}

func TestComputeRelationshipSuggestionsEmptyAssignmentsReturnsNoSuggestions(t *testing.T) {
	got := computeRelationshipSuggestions(nil, map[string][]PersonRelationship{})
	if len(got) != 0 {
		t.Errorf("expected no suggestions for an empty roster, got %+v", got)
	}
}

func TestComputeRelationshipSuggestionsMatchesWhenMemberIsPersonIDA(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "member"}}
	relsByPerson := map[string][]PersonRelationship{
		"member": {{PersonIDA: "member", PersonIDB: "candidate", RelationshipType: PersonRelationshipWorksWell}},
	}

	got := computeRelationshipSuggestions(assignments, relsByPerson)

	if len(got) != 1 || got[0].PersonID != "candidate" {
		t.Errorf("expected a suggestion for %q, got %+v", "candidate", got)
	}
}

func TestComputeRelationshipSuggestionsMatchesWhenMemberIsPersonIDB(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "member"}}
	relsByPerson := map[string][]PersonRelationship{
		"member": {{PersonIDA: "candidate", PersonIDB: "member", RelationshipType: PersonRelationshipMentorship}},
	}

	got := computeRelationshipSuggestions(assignments, relsByPerson)

	if len(got) != 1 || got[0].PersonID != "candidate" {
		t.Errorf("expected a suggestion for %q, got %+v", "candidate", got)
	}
}

// TestComputeRelationshipSuggestionsExcludesAvoidPairing confirms only
// works_well/mentorship relationships produce a suggestion - an
// avoid_pairing relationship must never show up as a recommended addition.
func TestComputeRelationshipSuggestionsExcludesAvoidPairing(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "member"}}
	relsByPerson := map[string][]PersonRelationship{
		"member": {{PersonIDA: "member", PersonIDB: "candidate", RelationshipType: PersonRelationshipAvoidPairing}},
	}

	got := computeRelationshipSuggestions(assignments, relsByPerson)

	if len(got) != 0 {
		t.Errorf("expected avoid_pairing to be excluded, got %+v", got)
	}
}

func TestComputeRelationshipSuggestionsExcludesPeopleAlreadyOnRoster(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "member-a"}, {PersonID: "member-b"}}
	relsByPerson := map[string][]PersonRelationship{
		"member-a": {{PersonIDA: "member-a", PersonIDB: "member-b", RelationshipType: PersonRelationshipWorksWell}},
		"member-b": {},
	}

	got := computeRelationshipSuggestions(assignments, relsByPerson)

	if len(got) != 0 {
		t.Errorf("expected people already on the roster to be excluded, got %+v", got)
	}
}

// TestComputeRelationshipSuggestionsAccumulatesRelationshipTypesAndNotes
// confirms multiple relationship rows pointing at the same candidate
// (from different roster members, or the same one) merge into one
// suggestion, and an empty Notes value is not appended.
func TestComputeRelationshipSuggestionsAccumulatesRelationshipTypesAndNotes(t *testing.T) {
	assignments := []RosterAssignment{{PersonID: "member-a"}, {PersonID: "member-b"}}
	relsByPerson := map[string][]PersonRelationship{
		"member-a": {{PersonIDA: "member-a", PersonIDB: "candidate", RelationshipType: PersonRelationshipWorksWell, Notes: "great team"}},
		"member-b": {{PersonIDA: "candidate", PersonIDB: "member-b", RelationshipType: PersonRelationshipMentorship, Notes: ""}},
	}

	got := computeRelationshipSuggestions(assignments, relsByPerson)

	if len(got) != 1 {
		t.Fatalf("expected exactly one suggestion (deduped across members), got %+v", got)
	}
	sug := got[0]
	if sug.PersonID != "candidate" {
		t.Errorf("expected suggestion for %q, got %+v", "candidate", sug)
	}
	if len(sug.RelationshipTypes) != 2 {
		t.Errorf("expected both relationship types accumulated, got %v", sug.RelationshipTypes)
	}
	if len(sug.Notes) != 1 || sug.Notes[0] != "great team" {
		t.Errorf("expected only the non-empty note to be appended, got %v", sug.Notes)
	}
}
