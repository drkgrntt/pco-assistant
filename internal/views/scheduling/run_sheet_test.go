package scheduling

import "testing"

// noteView is a small helper constructor, same style as sets_test.go's
// foreignItem/songItem/setSong - just enough of a SongNoteView to exercise
// hasAnyNoteContent without spelling out every field at every call site.
func noteView(categoryName, content string) SongNoteView {
	return SongNoteView{CategoryName: categoryName, Content: content}
}

func TestHasAnyNoteContentAllEmpty(t *testing.T) {
	notes := []SongNoteView{noteView("Audio/Visual", ""), noteView("Band", "")}

	if hasAnyNoteContent(notes) {
		t.Error("expected no content among all-empty notes")
	}
}

func TestHasAnyNoteContentNone(t *testing.T) {
	if hasAnyNoteContent(nil) {
		t.Error("expected no content for a nil notes slice")
	}
}

func TestHasAnyNoteContentSomeFilled(t *testing.T) {
	notes := []SongNoteView{noteView("Audio/Visual", ""), noteView("Band", "Keys down an octave")}

	if !hasAnyNoteContent(notes) {
		t.Error("expected content since Band has a note")
	}
}

// assignedPositionGroupView is positionGroupView (roster_builder_test.go)
// plus an assigned-count knob, since teamGroupHasAssignments/
// anyPositionAssigned care only about Assigned's length, not who's in it.
func assignedPositionGroupView(teamID, teamName, positionName string, assignedCount int) PositionGroupView {
	pos := positionGroupView(teamID, teamName, positionName)
	pos.Assigned = make([]AssignmentView, assignedCount)
	return pos
}

func TestTeamGroupHasAssignmentsAllUnassigned(t *testing.T) {
	positions := []PositionGroupView{
		assignedPositionGroupView("band", "Band", "Drums", 0),
		assignedPositionGroupView("band", "Band", "Bass", 0),
	}
	group := groupPositionsByTeam(positions)[0]

	if teamGroupHasAssignments(group) {
		t.Error("expected no assignments when every position in the group is unassigned")
	}
}

func TestTeamGroupHasAssignmentsSomeAssigned(t *testing.T) {
	positions := []PositionGroupView{
		assignedPositionGroupView("band", "Band", "Drums", 0),
		assignedPositionGroupView("band", "Band", "Bass", 1),
	}
	group := groupPositionsByTeam(positions)[0]

	if !teamGroupHasAssignments(group) {
		t.Error("expected assignments since Bass has someone assigned")
	}
}

func TestAnyPositionAssignedNone(t *testing.T) {
	positions := []PositionGroupView{
		assignedPositionGroupView("band", "Band", "Drums", 0),
		assignedPositionGroupView("vocals", "Vocals", "Soprano", 0),
	}

	if anyPositionAssigned(positions) {
		t.Error("expected no assignments across an entirely-unassigned roster")
	}
}

func TestAnyPositionAssignedSome(t *testing.T) {
	positions := []PositionGroupView{
		assignedPositionGroupView("band", "Band", "Drums", 0),
		assignedPositionGroupView("vocals", "Vocals", "Soprano", 2),
	}

	if !anyPositionAssigned(positions) {
		t.Error("expected an assignment since Soprano has 2 assigned")
	}
}
