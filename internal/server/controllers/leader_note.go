package controllers

// The song-leader sync: two independent write targets share the same local
// SetSong.LeaderPersonID.
//
//  1. syncLeaderNote/composeLeaderNoteContent - composing the reserved
//     "Leading: <name>" line into a Planning Center Item Note category's
//     free-text content without clobbering anything a human typed there by
//     hand. See docs/plans/shipped/song-leader-plan.md's "How the write is
//     composed" decision (option 1: a reserved prefix line this app owns,
//     not a second dedicated note category).
//  2. syncLeaderAssignment/personAssignmentFor - writing the same leader to
//     PCO's real "Song leader" resource, ItemAssignment (go-pco's
//     itemAssignments.go) - discovered and added after (1) shipped, when it
//     turned out PCO's plan-item edit panel already has a first-class
//     "Song leader" field/section backed by this resource, not merely a
//     free-text note; see docs/plans/shipped/worship-leader-pco-sync-plan.md
//     for the live verification trail. (1) is kept alongside (2) rather
//     than replaced - some orgs like seeing the leader spelled out right in
//     the Vocals note text they already read, and the two can't drift into
//     a real inconsistency (both are just fresh writes of the same local
//     field on every Publish/Refresh).
//
// Both are wired into the actual Publish/Refresh write loop in
// sets_publish.go.

import (
	"context"
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

// personAssignmentFor is noteInCategory's counterpart for ItemAssignment -
// finds itemID's existing Person-type assignment (if any) from a plan-wide
// assignmentsByItem map (see itemAssignmentsFromIncluded, sets_builder.go),
// a pure lookup with no PCO call of its own. Returns nil when there's no
// Person assignment on this item yet. A song item is only ever expected to
// carry at most one (PCO's own UI only ever showed a single "Add leader"
// slot when this was checked live), but this returns the first match
// rather than asserting that - see itemAssignments.go's own doc comment on
// why that's not treated as a documented constraint.
func personAssignmentFor(assignmentsByItem map[string][]pco.ItemAssignmentData, itemID string) *pco.ItemAssignmentData {
	for i, a := range assignmentsByItem[itemID] {
		if a.Attributes.AssignableType == pco.ItemAssignmentAssignableTypePerson {
			return &assignmentsByItem[itemID][i]
		}
	}
	return nil
}

// syncLeaderAssignment writes ss's local song leader (SetSong.LeaderPersonID)
// as a real Planning Center ItemAssignment on itemID - the resource behind
// what PCO's own Services UI calls "Song leader" on a plan item's edit
// panel (confirmed live - see docs/plans/shipped/
// worship-leader-pco-sync-plan.md). current is itemID's existing Person
// assignment right now, if any (nil meaning none) - always resolved by the
// caller from whatever it already sideloaded via personAssignmentFor/
// itemAssignmentsFromIncluded (an Include: []string{"item_assignments"}
// GetItems call already being made for another reason - see
// PublishSet/RefreshSongInfo's own call sites), never a fresh per-song
// GetItemAssignments call.
//
// No-op when itemID == "" (an unpublished song has no plan item to assign
// a leader on) or when leaderPersonID already matches current's assigned
// person (no pointless delete+recreate - see CreateItemAssignment's own
// doc comment on why reassigning is delete-and-recreate, not a PATCH: PCO
// documents no update endpoint for this resource at all).
func syncLeaderAssignment(ctx context.Context, serviceTypeID, planID, itemID string, current *pco.ItemAssignmentData, leaderPersonID string) error {
	if itemID == "" {
		return nil
	}

	var currentPersonID string
	if current != nil && current.Relationships.Assignable.Data != nil {
		currentPersonID = current.Relationships.Assignable.Data.ID
	}
	if currentPersonID == leaderPersonID {
		return nil
	}

	if current != nil {
		if err := pco.DeleteItemAssignment(ctx, serviceTypeID, planID, itemID, current.ID); err != nil {
			return err
		}
	}

	if leaderPersonID == "" {
		return nil
	}

	_, err := pco.CreateItemAssignment(ctx, serviceTypeID, planID, itemID, &pco.CreateItemAssignmentParams{PersonID: leaderPersonID})
	return err
}
