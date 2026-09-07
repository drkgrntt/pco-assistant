package scheduling

// DB-backed tests for the transition-notes store methods (migration
// 00040) - see person_data_cache_db_test.go's package doc comment for why
// these need a real Postgres (RUN_DB_TESTS=1) rather than the plain
// unit-test style transition_notes_test.go (controllers package) uses:
// they're exercising real NULL/NULLIF/IS NOT DISTINCT FROM handling and
// ordering via SQL, not just in-memory logic.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run TransitionNote -v

import (
	"context"
	"testing"
)

func cleanupTransitionNotes(t *testing.T, store *Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := store.db.ExecContext(context.Background(), `DELETE FROM song_transition_notes WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("cleanup: deleting song_transition_notes for org %s: %v", organizationID, err)
		}
	})
}

// TestCreateTransitionNoteDirectionalNotCanonicalized confirms the single
// most important divergence from song_relationships.go's
// CreateSongRelationship: entering a pair in one order and its reverse
// creates two independent rows, neither one visible as a match for the
// other's exact key - see this package's own doc comment on
// song_transition_notes.go for why.
func TestCreateTransitionNoteDirectionalNotCanonicalized(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	songB := testScopeID("song")
	cleanupTransitionNotes(t, store, orgID)

	if _, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteDebrief, SongIDA: songA, SongIDB: songB, Note: "A into B felt abrupt",
	}); err != nil {
		t.Fatalf("CreateTransitionNote(A->B): %v", err)
	}

	forward, err := store.ListTransitionNotesForKey(ctx, orgID, songA, songB, TransitionNoteDebrief)
	if err != nil {
		t.Fatalf("ListTransitionNotesForKey(A,B): %v", err)
	}
	if len(forward) != 1 {
		t.Fatalf("expected 1 note for the A->B key, got %d", len(forward))
	}

	reverse, err := store.ListTransitionNotesForKey(ctx, orgID, songB, songA, TransitionNoteDebrief)
	if err != nil {
		t.Fatalf("ListTransitionNotesForKey(B,A): %v", err)
	}
	if len(reverse) != 0 {
		t.Errorf("expected the reverse-order key to have no notes (direction matters), got %d", len(reverse))
	}
}

// TestTransitionNoteSingleSongUsesRealNull confirms a single-song note
// (SongIDB == "") round-trips as "" and is matched by
// ListTransitionNotesForKey/LatestTransitionNote's own "" lookup - proving
// it's stored as a real SQL NULL (via NULLIF), not the literal empty
// string, since the lookup's IS NOT DISTINCT FROM NULLIF($3,'') only
// matches a genuine NULL.
func TestTransitionNoteSingleSongUsesRealNull(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	cleanupTransitionNotes(t, store, orgID)

	created, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteTalkingPoint, SongIDA: songA, Note: "Say this before it starts",
	})
	if err != nil {
		t.Fatalf("CreateTransitionNote (single-song): %v", err)
	}
	if created.SongIDB != "" {
		t.Errorf("expected SongIDB to round-trip as \"\", got %q", created.SongIDB)
	}

	note, ok, err := store.LatestTransitionNote(ctx, orgID, songA, "", TransitionNoteTalkingPoint)
	if err != nil {
		t.Fatalf("LatestTransitionNote: %v", err)
	}
	if !ok {
		t.Fatal("expected LatestTransitionNote to find the single-song note")
	}
	if note.ID != created.ID {
		t.Errorf("expected to find the note just created (id %d), got id %d", created.ID, note.ID)
	}
}

// TestLatestTransitionNoteReturnsNewestOnly confirms the append-only
// table's "latest row only" read for talking points: two notes entered
// against the same key, LatestTransitionNote returns only the most recent
// one - see this package's own doc comment on why talking points stay
// append-only rather than upserting in place.
func TestLatestTransitionNoteReturnsNewestOnly(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	songB := testScopeID("song")
	cleanupTransitionNotes(t, store, orgID)

	if _, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteTalkingPoint, SongIDA: songA, SongIDB: songB, Note: "old wording",
	}); err != nil {
		t.Fatalf("CreateTransitionNote (first): %v", err)
	}
	newest, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteTalkingPoint, SongIDA: songA, SongIDB: songB, Note: "updated wording",
	})
	if err != nil {
		t.Fatalf("CreateTransitionNote (second): %v", err)
	}

	latest, ok, err := store.LatestTransitionNote(ctx, orgID, songA, songB, TransitionNoteTalkingPoint)
	if err != nil {
		t.Fatalf("LatestTransitionNote: %v", err)
	}
	if !ok {
		t.Fatal("expected a latest note")
	}
	if latest.ID != newest.ID || latest.Note != "updated wording" {
		t.Errorf("expected the newest row (%q), got %+v", "updated wording", latest)
	}

	log, err := store.ListTransitionNotesForKey(ctx, orgID, songA, songB, TransitionNoteTalkingPoint)
	if err != nil {
		t.Fatalf("ListTransitionNotesForKey: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("expected both notes to remain in the append-only log, got %d", len(log))
	}
}

// TestListTransitionNotesForSongMatchesEitherSide confirms a pair note is
// reachable from both songs' own ids - the "both cards" decision
// docs/plans/shipped/transition-notes-plan.md resolved, mirroring
// ListSongRelationships' own either-side query.
func TestListTransitionNotesForSongMatchesEitherSide(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	songB := testScopeID("song")
	cleanupTransitionNotes(t, store, orgID)

	if _, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteDebrief, SongIDA: songA, SongIDB: songB, Note: "into B felt abrupt",
	}); err != nil {
		t.Fatalf("CreateTransitionNote: %v", err)
	}

	forA, err := store.ListTransitionNotesForSong(ctx, orgID, songA)
	if err != nil {
		t.Fatalf("ListTransitionNotesForSong(songA): %v", err)
	}
	if len(forA) != 1 {
		t.Errorf("expected the pair note to show up on song A's own list, got %d", len(forA))
	}

	forB, err := store.ListTransitionNotesForSong(ctx, orgID, songB)
	if err != nil {
		t.Fatalf("ListTransitionNotesForSong(songB): %v", err)
	}
	if len(forB) != 1 {
		t.Errorf("expected the pair note to also show up on song B's own list, got %d", len(forB))
	}

	// A third, unrelated song must not see it.
	forOther, err := store.ListTransitionNotesForSong(ctx, orgID, testScopeID("song"))
	if err != nil {
		t.Fatalf("ListTransitionNotesForSong(unrelated): %v", err)
	}
	if len(forOther) != 0 {
		t.Errorf("expected an unrelated song to see no notes, got %d", len(forOther))
	}
}

// TestDeleteTransitionNoteScopedToOrganization confirms delete is
// org-scoped (a forged/guessed id from a different organization is a
// no-op, sql.ErrNoRows) - same convention as DeleteSongRelationship.
func TestDeleteTransitionNoteScopedToOrganization(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	otherOrgID := testScopeID("org")
	songA := testScopeID("song")
	cleanupTransitionNotes(t, store, orgID)
	cleanupTransitionNotes(t, store, otherOrgID)

	created, err := store.CreateTransitionNote(ctx, orgID, CreateTransitionNoteParams{
		Kind: TransitionNoteDebrief, SongIDA: songA, Note: "note",
	})
	if err != nil {
		t.Fatalf("CreateTransitionNote: %v", err)
	}

	if err := store.DeleteTransitionNote(ctx, otherOrgID, created.ID); err == nil {
		t.Error("expected deleting another organization's note to fail (sql.ErrNoRows), got nil")
	}

	if err := store.DeleteTransitionNote(ctx, orgID, created.ID); err != nil {
		t.Fatalf("DeleteTransitionNote (real owner): %v", err)
	}

	remaining, err := store.ListTransitionNotesForSong(ctx, orgID, songA)
	if err != nil {
		t.Fatalf("ListTransitionNotesForSong after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected the note to be gone after delete, got %d remaining", len(remaining))
	}
}
