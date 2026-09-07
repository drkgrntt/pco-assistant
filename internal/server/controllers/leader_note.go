package controllers

// The song-leader -> vocal-notes-category sync: composing the reserved
// "Leading: <name>" line into a Planning Center Item Note category's
// free-text content without clobbering anything a human typed there by
// hand. See docs/plans/shipped/song-leader-plan.md's "How the write is
// composed" decision (option 1: a reserved prefix line this app owns, not
// a second dedicated note category) and syncLeaderNote (sets_publish.go),
// which wires this into the actual Publish/Refresh write loop.

import (
	"strings"

	pco "github.com/drkgrntt/go-pco"
)

// leaderNoteLinePrefix is the exact, reserved first-line prefix this app
// owns inside the target category's content - "Leading: Jane Smith", for
// example. Only a line that starts with this *exact* string (colon and
// trailing space included) is ever treated as "this app's own leader
// line" and replaced/removed - see composeLeaderNoteContent's own doc
// comment for why that matters (content that merely starts with similar-
// looking text must never be silently clobbered).
const leaderNoteLinePrefix = "Leading: "

// composeLeaderNoteContent returns existingContent (the vocal notes
// category's current free-text content on one song's plan item, exactly
// as Planning Center has it right now) with leaderName's reserved first
// line applied:
//
//   - If existingContent's first line is already a reserved leader line
//     (starts with leaderNoteLinePrefix), it's replaced in place -
//     everything after that first line is left completely untouched.
//   - Otherwise, a fresh reserved line is prepended ahead of whatever was
//     already there (which is left untouched, on its own line(s) below).
//   - leaderName == "" (the local leader was cleared) removes the
//     reserved line entirely rather than leaving a stale "Leading: "
//     line with nothing after the colon - if that leaves nothing else in
//     the content, the result is "", not a dangling blank line.
//
// Only ever inspects/replaces the *first* line - existingContent that
// happens to start with unrelated text merely resembling the prefix
// (e.g. "Leading worship this week..." - no colon) does not match
// leaderNoteLinePrefix exactly, so it's left alone and a real reserved
// line is prepended ahead of it, never overwriting it.
func composeLeaderNoteContent(existingContent, leaderName string) string {
	lines := strings.Split(existingContent, "\n")
	rest := lines
	if len(lines) > 0 && strings.HasPrefix(lines[0], leaderNoteLinePrefix) {
		rest = lines[1:]
	}
	restContent := strings.Join(rest, "\n")

	if leaderName == "" {
		return restContent
	}

	leaderLine := leaderNoteLinePrefix + leaderName
	if restContent == "" {
		return leaderLine
	}
	return leaderLine + "\n" + restContent
}

// noteInCategory finds itemID's existing Planning Center note (if any) in
// categoryID, from a plan-wide notesByItem map (see itemNotesFromIncluded,
// sets_builder.go) - a pure lookup, no PCO call of its own. Returns
// ""/"" (noteID/content) when there's no note in that category on this
// item yet, the same "nothing there yet" shape buildSongNoteViews already
// uses for SongNoteView.
func noteInCategory(notesByItem map[string][]pco.ItemNoteData, itemID, categoryID string) (noteID, content string) {
	for _, n := range notesByItem[itemID] {
		if n.Relationships.ItemNoteCategory.Data != nil && n.Relationships.ItemNoteCategory.Data.ID == categoryID {
			return n.ID, n.Attributes.Content
		}
	}
	return "", ""
}
