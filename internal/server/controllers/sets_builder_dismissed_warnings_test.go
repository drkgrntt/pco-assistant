package controllers

// Non-DB unit tests covering the render-time half of
// docs/plans/shipped/dismissable-warnings-plan.md - given a pre-computed
// conflictBadgesBySong/recentRepeatDismissed map (exactly what
// buildSetBuilderData assembles from scheduling.Store.DismissedWarningKeys,
// sets_builder.go), renderServiceOrder must (a) carry a per-badge Dismissed
// flag through onto SetSongView.Conflicts rather than a single row-wide
// flag, and (b) respect recentRepeatSubjectKey's per-plan scoping - a
// dismissal recorded against one plan's occurrence of a song must never
// silently suppress the same song's own repeat warning on a different
// plan. The DB-backed half (Store.DismissWarning/UndismissWarning/
// DismissedWarningKeys themselves, including org-scoping) lives in
// internal/scheduling/dismissed_warnings_db_test.go
// (RUN_DB_TESTS=1) - this file only exercises the pure rendering logic, the
// same "renderServiceOrder with hand-built maps, no store/PCO needed" style
// sets_builder_test.go's own renderServiceOrderRow already uses for
// RecentlyRepeated/Conflicts/KeyTransitionLabel.

import (
	"context"
	"testing"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"
)

// renderServiceOrderRowWithDismissals is renderServiceOrderRow
// (sets_builder_test.go) plus the two dismissal-lookup maps that helper
// leaves nil - the tests below are specifically about those maps'
// effect, unlike the plain RecentlyRepeated/Conflicts-existence tests that
// helper already backs.
func renderServiceOrderRowWithDismissals(t *testing.T, rows []mergedRow, planID string, repeated map[string]bool, conflictBadgesBySong map[string][]views.ConflictBadgeView, recentRepeatDismissed map[string]bool, songID string) views.SetSongView {
	t.Helper()
	cache := map[string]scheduling.SongArrangementInfo{}
	out := renderServiceOrder(context.Background(), nil, "org1", planID, cache, nil, nil, nil, nil, rows, map[string]string{}, nil, nil, nil, nil, nil, repeated, conflictBadgesBySong, recentRepeatDismissed, nil, map[int64]keyTransitionWarning{}, nil, nil, nil, nil)
	for _, row := range out {
		if row.IsSong && row.Song.SongID == songID {
			return row.Song
		}
	}
	t.Fatalf("no rendered row found for song %q", songID)
	return views.SetSongView{}
}

// TestRenderServiceOrderConflictBadgeDismissedRendersMuted confirms a
// dismissed avoid-pairing badge carries Conflicts[i].Dismissed = true (the
// muted-styling/collapse-count-exclusion signal set_builder.templ's
// warningToggleClass/hasLiveConflict check), while an otherwise-identical
// undismissed badge still comes back Dismissed = false - the "renders
// muted, not loud" vs. "still renders loud" pair the dismissable-warnings
// plan's own verification section calls for.
func TestRenderServiceOrderConflictBadgeDismissedRendersMuted(t *testing.T) {
	a := setSong("song-a", "", 0)
	b := setSong("song-b", "", 1)
	rows := []mergedRow{{Song: &a}, {Song: &b}}
	key := avoidPairingSubjectKey("song-a", "song-b")

	dismissedBadges := map[string][]views.ConflictBadgeView{
		"song-a": {{Title: "Song B", SubjectKey: key, Dismissed: true}},
	}
	gotDismissed := renderServiceOrderRowWithDismissals(t, rows, "plan1", map[string]bool{}, dismissedBadges, map[string]bool{}, "song-a")
	if len(gotDismissed.Conflicts) != 1 || !gotDismissed.Conflicts[0].Dismissed {
		t.Errorf("expected song-a's dismissed conflict badge to carry Dismissed = true, got %+v", gotDismissed.Conflicts)
	}

	liveBadges := map[string][]views.ConflictBadgeView{
		"song-a": {{Title: "Song B", SubjectKey: key, Dismissed: false}},
	}
	gotLive := renderServiceOrderRowWithDismissals(t, rows, "plan1", map[string]bool{}, liveBadges, map[string]bool{}, "song-a")
	if len(gotLive.Conflicts) != 1 || gotLive.Conflicts[0].Dismissed {
		t.Errorf("expected song-a's undismissed conflict badge to still carry Dismissed = false, got %+v", gotLive.Conflicts)
	}
}

// TestRenderServiceOrderRecentRepeatDismissalScopedToPlan confirms
// recentRepeatSubjectKey's per-occurrence scoping actually holds at render
// time: dismissing song-a's recently-repeated warning on plan-a must not
// suppress the identical warning for song-a's own occurrence on a
// different plan-b - the resolved per-occurrence rule from
// docs/plans/shipped/dismissable-warnings-plan.md (as opposed to
// avoid-pairing's permanent, plan-independent scope).
func TestRenderServiceOrderRecentRepeatDismissalScopedToPlan(t *testing.T) {
	a := setSong("song-a", "", 0)
	rows := []mergedRow{{Song: &a}}
	repeated := map[string]bool{"song-a": true}
	recentRepeatDismissed := map[string]bool{
		recentRepeatSubjectKey("plan-a", "song-a"): true,
	}

	gotPlanA := renderServiceOrderRowWithDismissals(t, rows, "plan-a", repeated, map[string][]views.ConflictBadgeView{}, recentRepeatDismissed, "song-a")
	if !gotPlanA.RecentlyRepeated || !gotPlanA.RecentlyRepeatedDismissed {
		t.Errorf("expected plan-a's song-a to be RecentlyRepeated and dismissed, got %+v", gotPlanA)
	}

	gotPlanB := renderServiceOrderRowWithDismissals(t, rows, "plan-b", repeated, map[string][]views.ConflictBadgeView{}, recentRepeatDismissed, "song-a")
	if !gotPlanB.RecentlyRepeated {
		t.Error("expected plan-b's song-a to still carry the live RecentlyRepeated warning")
	}
	if gotPlanB.RecentlyRepeatedDismissed {
		t.Error("expected plan-a's dismissal to NOT carry over to plan-b's own occurrence of the same song")
	}
}

// TestAvoidPairingSubjectKeyCanonicalOrder confirms avoidPairingSubjectKey's
// order-independence - dismissing "avoid A near B" from either song's own
// row must resolve to the same stored key, matching
// scheduling.CreateSongRelationship's existing lexicographic-order
// convention this function's own doc comment cites.
func TestAvoidPairingSubjectKeyCanonicalOrder(t *testing.T) {
	if got, want := avoidPairingSubjectKey("song-a", "song-b"), avoidPairingSubjectKey("song-b", "song-a"); got != want {
		t.Errorf("expected avoidPairingSubjectKey to be order-independent, got %q and %q", got, want)
	}
}
