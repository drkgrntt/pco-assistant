package controllers

// The song-leader sync: writing the local SetSong.LeaderPersonID to PCO's
// real "Song leader" resource, ItemAssignment (go-pco's itemAssignments.go).
// See docs/plans/shipped/worship-leader-pco-sync-plan.md for the live
// verification trail that found this resource - PCO's plan-item edit panel
// has a first-class "Song leader" field/section backed by it, not a
// free-text note.
//
// This app originally (docs/plans/shipped/song-leader-plan.md) synced the
// leader as a reserved "Leading: <name>" line merged into an Item Note
// category the org designated as "the vocal one" - a stopgap for when no
// real structured PCO field was known to exist. Once ItemAssignment was
// found and wired in, that note-line sync was removed outright (not kept
// alongside it) - the real assignment is strictly better (it's what
// actually drives PCO's own notifications) and keeping both meant a
// "Song leader sync setting" (which note category to write into) that
// added setup friction for no remaining benefit.
//
// syncLeaderAssignment is wired into the actual Publish/Refresh write loop
// in sets_publish.go.

import (
	"context"

	pco "github.com/drkgrntt/go-pco"
)

// personAssignmentFor finds itemID's existing Person-type assignment (if
// any) from a plan-wide assignmentsByItem map (see
// itemAssignmentsFromIncluded, sets_builder.go), a pure lookup with no PCO
// call of its own. Returns nil when there's no Person assignment on this
// item yet. A song item is only ever expected to carry at most one (PCO's
// own UI only ever showed a single "Add leader" slot when this was checked
// live), but this returns the first match rather than asserting that - see
// itemAssignments.go's own doc comment on why that's not treated as a
// documented constraint.
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
