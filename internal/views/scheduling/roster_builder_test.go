package scheduling

// Unit test for groupPositionsByTeam, the roster builder's team-grouping
// helper - see its own doc comment for why it assumes an already
// team-sorted input rather than sorting itself.

import "testing"

func positionGroupView(teamID, teamName, positionName string) PositionGroupView {
	return PositionGroupView{TeamID: teamID, TeamName: teamName, TeamPositionName: positionName}
}

func TestGroupPositionsByTeamGroupsContiguousRuns(t *testing.T) {
	positions := []PositionGroupView{
		positionGroupView("band", "Band", "Drums"),
		positionGroupView("band", "Band", "Bass"),
		positionGroupView("vocals", "Vocals", "Soprano"),
	}

	got := groupPositionsByTeam(positions)

	if len(got) != 2 {
		t.Fatalf("expected 2 team groups, got %d: %+v", len(got), got)
	}
	if got[0].TeamName != "Band" || len(got[0].Positions) != 2 {
		t.Errorf("expected Band group with 2 positions, got %+v", got[0])
	}
	if got[1].TeamName != "Vocals" || len(got[1].Positions) != 1 {
		t.Errorf("expected Vocals group with 1 position, got %+v", got[1])
	}
}

// TestGroupPositionsByTeamPreservesOriginalIndex confirms each position
// keeps its index into the original slice - positionGroupItem.Index, kept
// for callers that care about original ordering (each position's own card
// id is content-derived instead, see PositionGroupElementID, so it no
// longer depends on this index staying stable across a re-render).
func TestGroupPositionsByTeamPreservesOriginalIndex(t *testing.T) {
	positions := []PositionGroupView{
		positionGroupView("band", "Band", "Drums"),
		positionGroupView("vocals", "Vocals", "Soprano"),
	}

	got := groupPositionsByTeam(positions)

	if got[0].Positions[0].Index != 0 {
		t.Errorf("expected Drums to keep index 0, got %d", got[0].Positions[0].Index)
	}
	if got[1].Positions[0].Index != 1 {
		t.Errorf("expected Soprano to keep index 1, got %d", got[1].Positions[0].Index)
	}
}

// TestGroupPositionsByTeamNonContiguousSameTeamSplits confirms this
// doesn't re-sort: if the same team's positions somehow aren't contiguous
// in the input, they land in separate groups rather than being merged -
// grouping the input is buildRosterBuilderData's job (it sorts by team
// Sequence before this runs), not this function's.
func TestGroupPositionsByTeamNonContiguousSameTeamSplits(t *testing.T) {
	positions := []PositionGroupView{
		positionGroupView("band", "Band", "Drums"),
		positionGroupView("vocals", "Vocals", "Soprano"),
		positionGroupView("band", "Band", "Bass"),
	}

	got := groupPositionsByTeam(positions)

	if len(got) != 3 {
		t.Fatalf("expected 3 groups (no merging across the non-contiguous Band run), got %d: %+v", len(got), got)
	}
}

func TestGroupPositionsByTeamEmptyInput(t *testing.T) {
	if got := groupPositionsByTeam(nil); len(got) != 0 {
		t.Errorf("expected no groups for empty input, got %+v", got)
	}
}
