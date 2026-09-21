package scheduling

import (
	"testing"

	"pco-assistant/internal/scheduling"
)

// Unit tests for otherPersonID (person_detail.templ) - resolving a
// personID-scoped relationship row to whichever side isn't the page's own
// subject, so PersonDetailRelationshipsSection can show "who this is with"
// without A/B labels.
func TestOtherPersonIDPersonIsSideA(t *testing.T) {
	rel := scheduling.PersonRelationship{PersonIDA: "me", PersonIDB: "them"}
	if got := otherPersonID(rel, "me"); got != "them" {
		t.Errorf("otherPersonID = %q, want %q", got, "them")
	}
}

func TestOtherPersonIDPersonIsSideB(t *testing.T) {
	rel := scheduling.PersonRelationship{PersonIDA: "them", PersonIDB: "me"}
	if got := otherPersonID(rel, "me"); got != "them" {
		t.Errorf("otherPersonID = %q, want %q", got, "them")
	}
}
