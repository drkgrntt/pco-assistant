package controllers

import (
	"testing"
	"time"

	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func neededPosition(teamID, positionName string, quantity int) pco.NeededPositionData {
	np := pco.NeededPositionData{Attributes: pco.NeededPositionAttributes{
		Quantity:         quantity,
		TeamPositionName: positionName,
	}}
	np.Relationships.Team.Data = &pco.General{Type: "Team", ID: teamID}
	return np
}

func rosterAssignment(id int64, personID, teamID, positionName string) scheduling.RosterAssignment {
	return scheduling.RosterAssignment{ID: id, PersonID: personID, TeamID: teamID, TeamPositionName: positionName}
}

// publishedRosterAssignment mirrors rosterAssignment for an assignment
// that's already been pushed to Planning Center as a real PlanPerson (or
// was imported from one) - see PCOPlanPersonID's own doc comment
// (scheduling/rosters.go).
func publishedRosterAssignment(id int64, personID, teamID, positionName, pcoPlanPersonID string) scheduling.RosterAssignment {
	a := rosterAssignment(id, personID, teamID, positionName)
	a.PCOPlanPersonID = pcoPlanPersonID
	return a
}

func TestGroupNeededPositionsMergesQuantityAndAssignments(t *testing.T) {
	needed := []pco.NeededPositionData{
		neededPosition("t1", "Drums", 2),
		neededPosition("t1", "Vocals", 1),
	}
	assignments := []scheduling.RosterAssignment{
		rosterAssignment(1, "p1", "t1", "Drums"),
	}

	got := groupNeededPositions(needed, assignments)

	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(got), got)
	}
	if got[0].TeamPositionName != "Drums" || got[0].Quantity != 2 || len(got[0].Assignments) != 1 {
		t.Errorf("Drums group wrong: %+v", got[0])
	}
	if got[1].TeamPositionName != "Vocals" || got[1].Quantity != 1 || len(got[1].Assignments) != 0 {
		t.Errorf("Vocals group wrong: %+v", got[1])
	}
}

// TestGroupNeededPositionsKeepsAssignmentWithNoNeededEntry confirms an
// assignment for a position Planning Center never configured a "needed"
// count for still shows up (quantity 0), rather than being silently
// dropped - mirrors mergeServiceOrder's "never drop data" principle.
func TestGroupNeededPositionsKeepsAssignmentWithNoNeededEntry(t *testing.T) {
	assignments := []scheduling.RosterAssignment{
		rosterAssignment(1, "p1", "t1", "Lights"),
	}

	got := groupNeededPositions(nil, assignments)

	if len(got) != 1 || got[0].TeamPositionName != "Lights" || got[0].Quantity != 0 || len(got[0].Assignments) != 1 {
		t.Errorf("expected a 0-quantity Lights group with 1 assignment, got %+v", got)
	}
}

// TestGroupNeededPositionsAddsBackAlreadyPublishedFills confirms the fix
// for the "N of (N-1) filled" bug: Planning Center's own
// NeededPosition.Quantity counts down as a position gets filled (dropping
// out of the list entirely once fully staffed, not settling at an
// explicit 0), so a position PCO reports as needing 0 more - because its
// one assigned person is already published there - must still show as
// needing 1 total, not 0, once that published fill is added back in.
func TestGroupNeededPositionsAddsBackAlreadyPublishedFills(t *testing.T) {
	// Fully staffed: PCO has dropped it from needed_positions entirely
	// (no matching entry at all), same as a real fully-filled plan.
	assignments := []scheduling.RosterAssignment{
		publishedRosterAssignment(1, "p1", "t1", "Drums", "pp-1"),
	}

	got := groupNeededPositions(nil, assignments)

	if len(got) != 1 || got[0].Quantity != 1 || len(got[0].Assignments) != 1 {
		t.Fatalf("expected Drums to need 1 total (1 already published), got %+v", got)
	}

	// Partially staffed: PCO still lists it, with Quantity already
	// decremented for the one person already published to it.
	needed := []pco.NeededPositionData{neededPosition("t1", "Vocals", 1)}
	assignments = []scheduling.RosterAssignment{
		publishedRosterAssignment(2, "p2", "t1", "Vocals", "pp-2"),
	}

	got = groupNeededPositions(needed, assignments)

	if len(got) != 1 || got[0].Quantity != 2 || len(got[0].Assignments) != 1 {
		t.Fatalf("expected Vocals to need 2 total (1 still open + 1 already published), got %+v", got)
	}

	// An unpublished (local-draft-only) assignment doesn't get added back
	// - Planning Center has no idea about it yet, so its own Quantity
	// hasn't moved to account for it.
	assignments = []scheduling.RosterAssignment{
		rosterAssignment(3, "p3", "t1", "Vocals"),
	}

	got = groupNeededPositions(needed, assignments)

	if len(got) != 1 || got[0].Quantity != 1 || len(got[0].Assignments) != 1 {
		t.Fatalf("expected an unpublished assignment to leave Quantity untouched, got %+v", got)
	}
}

func TestTeamNamesFromIncludedParsesTeamEntries(t *testing.T) {
	included := []any{
		map[string]any{
			"type": "Team",
			"id":   "t1",
			"attributes": map[string]any{
				"name": "Photography",
			},
		},
		map[string]any{
			"type": "Plan",
			"id":   "p1",
		},
	}

	got := teamNamesFromIncluded(included)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 team name (the Plan entry should be skipped), got %+v", got)
	}
	if got["t1"] != "Photography" {
		t.Errorf("expected t1 -> Photography, got %q", got["t1"])
	}
}

func TestPlanSortDatesFromIncludedParsesPlanEntries(t *testing.T) {
	included := []any{
		map[string]any{
			"type": "Plan",
			"id":   "p1",
			"attributes": map[string]any{
				"sort_date": "2026-09-20T00:00:00Z",
			},
		},
		map[string]any{
			"type": "Team",
			"id":   "t1",
		},
	}

	got := planSortDatesFromIncluded(included)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 plan date (the Team entry should be skipped), got %+v", got)
	}
	want := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if !got["p1"].Equal(want) {
		t.Errorf("expected p1 -> %v, got %v", want, got["p1"])
	}
}

func TestPositionTeamSequenceKnownTeam(t *testing.T) {
	seq := map[string]int{"t1": 2}

	if got := positionTeamSequence(seq, "t1"); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// TestPositionTeamSequenceUnknownTeamSortsLast confirms a team id with no
// entry (its Planning Center lookup failed) gets a sequence larger than
// any real team's, not the zero value - a zero-value default would sort a
// lookup failure to the *front* of the roster instead of the back.
func TestPositionTeamSequenceUnknownTeamSortsLast(t *testing.T) {
	seq := map[string]int{"t1": 0, "t2": 5}

	got := positionTeamSequence(seq, "unknown")
	if got <= seq["t1"] || got <= seq["t2"] {
		t.Errorf("expected the unknown team's sequence (%d) to sort after every known team (%v)", got, seq)
	}
}

// TestCandidatePersonViewLeavesWarningsEmpty confirms candidatePersonView -
// the single choke point buildRosterBuilderData and
// buildRosterPositionGroupData (rosters_scoped.go) both route every
// candidate row through - never populates FrequencyWarning/BlockoutWarning/
// CrossTeamWarning, regardless of what's passed in. Neither build function
// can be exercised end-to-end here (both need a real *scheduling.Store -
// see rosters_scoped_httptest_test.go's own doc comment on why this
// package has no DB test harness), so this is the practical way to pin the
// "candidates never get eager warnings" invariant described in
// pco-api-risks.md's cold-start-eligibility-pool addendum: as long as both
// build functions only ever construct a CandidatePersonView via this one
// function (confirmed by reading rosters_builder.go/rosters_scoped.go), a
// regression that made this helper populate a warning field would be
// caught here.
func TestCandidatePersonViewLeavesWarningsEmpty(t *testing.T) {
	got := candidatePersonView("p1", "Alice", pco.SchedulePreferenceEveryWeek)

	if got.FrequencyWarning != "" {
		t.Errorf("expected FrequencyWarning to stay empty, got %q", got.FrequencyWarning)
	}
	if got.BlockoutWarning != "" {
		t.Errorf("expected BlockoutWarning to stay empty, got %q", got.BlockoutWarning)
	}
	if got.CrossTeamWarning != "" {
		t.Errorf("expected CrossTeamWarning to stay empty, got %q", got.CrossTeamWarning)
	}
	if got.PersonID != "p1" || got.Name != "Alice" || got.SchedulePreference != pco.SchedulePreferenceEveryWeek {
		t.Errorf("expected PersonID/Name/SchedulePreference to still be set, got %+v", got)
	}
}
