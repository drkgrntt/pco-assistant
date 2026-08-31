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
