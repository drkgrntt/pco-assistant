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
