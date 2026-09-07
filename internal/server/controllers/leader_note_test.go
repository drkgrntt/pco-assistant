package controllers

import "testing"

// TestComposeLeaderNoteContent covers the edge cases the plan doc flagged
// as needing real, deliberate handling - see composeLeaderNoteContent's
// own doc comment (leader_note.go).
func TestComposeLeaderNoteContent(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		leaderName      string
		want            string
	}{
		{
			name:            "no existing content, leader set",
			existingContent: "",
			leaderName:      "Jane Smith",
			want:            "Leading: Jane Smith",
		},
		{
			name:            "existing content with no leader line, leader set",
			existingContent: "Bring extra mics for the choir",
			leaderName:      "Jane Smith",
			want:            "Leading: Jane Smith\nBring extra mics for the choir",
		},
		{
			name:            "existing content with a stale leader line, replaced",
			existingContent: "Leading: Old Person\nBring extra mics for the choir",
			leaderName:      "Jane Smith",
			want:            "Leading: Jane Smith\nBring extra mics for the choir",
		},
		{
			name:            "clearing a leader removes the line, keeps the rest",
			existingContent: "Leading: Jane Smith\nBring extra mics for the choir",
			leaderName:      "",
			want:            "Bring extra mics for the choir",
		},
		{
			name:            "clearing a leader when the leader line is the only content",
			existingContent: "Leading: Jane Smith",
			leaderName:      "",
			want:            "",
		},
		{
			name:            "clearing a leader with no existing content is a no-op",
			existingContent: "",
			leaderName:      "",
			want:            "",
		},
		{
			name:            "content coincidentally similar to the reserved prefix is left alone",
			existingContent: "Leading worship this week, go slow on the bridge",
			leaderName:      "Jane Smith",
			want:            "Leading: Jane Smith\nLeading worship this week, go slow on the bridge",
		},
		{
			name:            "replacing a leader when there's nothing else after it",
			existingContent: "Leading: Old Person",
			leaderName:      "New Person",
			want:            "Leading: New Person",
		},
		{
			name:            "multi-line trailing content after the leader line is preserved verbatim",
			existingContent: "Leading: Old Person\nline one\nline two",
			leaderName:      "New Person",
			want:            "Leading: New Person\nline one\nline two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := composeLeaderNoteContent(tt.existingContent, tt.leaderName)
			if got != tt.want {
				t.Errorf("composeLeaderNoteContent(%q, %q) = %q, want %q", tt.existingContent, tt.leaderName, got, tt.want)
			}
		})
	}
}
