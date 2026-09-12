package scheduling

// Unit test for assignedWarningCount, RosterStatusSummary's own input for
// its "Warnings: N" chip - see that function's own doc comment
// (roster_builder.templ) for why it counts affected people, not total
// warning strings.

import "testing"

func TestAssignedWarningCountCountsPeopleNotWarnings(t *testing.T) {
	positions := []PositionGroupView{
		{
			Assigned: []AssignmentView{
				{PersonID: "1", FrequencyWarning: "prefers less often", BlockoutWarning: "unavailable"},
				{PersonID: "2"},
			},
		},
		{
			Assigned: []AssignmentView{
				{PersonID: "3", CrossTeamWarning: "already serving elsewhere this week"},
			},
		},
	}

	got := assignedWarningCount(positions)

	if got != 2 {
		t.Errorf("expected 2 (person 1 counted once despite 2 warnings, plus person 3), got %d", got)
	}
}

func TestAssignedWarningCountNoWarningsIsZero(t *testing.T) {
	positions := []PositionGroupView{
		{Assigned: []AssignmentView{{PersonID: "1"}}},
	}

	if got := assignedWarningCount(positions); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestAssignedWarningCountEmptyInput(t *testing.T) {
	if got := assignedWarningCount(nil); got != 0 {
		t.Errorf("expected 0 for no positions, got %d", got)
	}
}
