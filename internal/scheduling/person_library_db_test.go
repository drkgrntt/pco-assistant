package scheduling

// DB-backed tests for the three distinct-person-id queries the People
// library index (internal/server/controllers/person_library.go) derives
// its person set from - DistinctPersonRelationshipPersonIDs,
// DistinctSongPersonPreferencePersonIDs, DistinctPersonVocalRangePersonIDs.
// Same "real Postgres, gated behind RUN_DB_TESTS=1" reasoning as
// song_tags_db_test.go's DistinctSongTagValues test (it's a SQL DISTINCT
// scan of real rows, not in-memory logic).
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run DistinctPerson -v

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

func TestDistinctPersonRelationshipPersonIDs(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personA := testScopeID("person")
	personB := testScopeID("person")
	personC := testScopeID("person")

	t.Cleanup(func() {
		if _, err := store.db.ExecContext(ctx, `DELETE FROM person_relationships WHERE organization_id = $1`, orgID); err != nil {
			t.Errorf("cleanup: deleting person_relationships for org %s: %v", orgID, err)
		}
	})

	ids, err := store.DistinctPersonRelationshipPersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctPersonRelationshipPersonIDs (no relationships): unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("DistinctPersonRelationshipPersonIDs (no relationships) = %v, want empty", ids)
	}

	// personA appears on the "A" side of one relationship and the "B" side
	// of another, proving both columns are unioned rather than just one.
	if _, err := store.CreatePersonRelationship(ctx, orgID, CreatePersonRelationshipParams{
		PersonIDA: personA, PersonIDB: personB, RelationshipType: PersonRelationshipWorksWell,
	}); err != nil {
		t.Fatalf("CreatePersonRelationship(A,B): %v", err)
	}
	if _, err := store.CreatePersonRelationship(ctx, orgID, CreatePersonRelationshipParams{
		PersonIDA: personC, PersonIDB: personA, RelationshipType: PersonRelationshipMentorship,
	}); err != nil {
		t.Fatalf("CreatePersonRelationship(C,A): %v", err)
	}

	ids, err = store.DistinctPersonRelationshipPersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctPersonRelationshipPersonIDs (after create): unexpected error: %v", err)
	}
	sort.Strings(ids)
	want := []string{personA, personB, personC}
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("DistinctPersonRelationshipPersonIDs = %v, want %v", ids, want)
	}
}

func TestDistinctSongPersonPreferencePersonIDs(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personA := testScopeID("person")
	personB := testScopeID("person")
	songA := testScopeID("song")
	songB := testScopeID("song")

	t.Cleanup(func() {
		if _, err := store.db.ExecContext(ctx, `DELETE FROM song_person_preferences WHERE organization_id = $1`, orgID); err != nil {
			t.Errorf("cleanup: deleting song_person_preferences for org %s: %v", orgID, err)
		}
	})

	ids, err := store.DistinctSongPersonPreferencePersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctSongPersonPreferencePersonIDs (no preferences): unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("DistinctSongPersonPreferencePersonIDs (no preferences) = %v, want empty", ids)
	}

	// personA is preferred for two different songs - must collapse to one
	// distinct id, not two.
	if _, err := store.SetSongPersonPreference(ctx, orgID, SetSongPersonPreferenceParams{SongID: songA, PersonID: personA, Preference: PreferencePreferred}); err != nil {
		t.Fatalf("SetSongPersonPreference(songA, personA): %v", err)
	}
	if _, err := store.SetSongPersonPreference(ctx, orgID, SetSongPersonPreferenceParams{SongID: songB, PersonID: personA, Preference: PreferenceDiscouraged}); err != nil {
		t.Fatalf("SetSongPersonPreference(songB, personA): %v", err)
	}
	if _, err := store.SetSongPersonPreference(ctx, orgID, SetSongPersonPreferenceParams{SongID: songA, PersonID: personB, Preference: PreferencePreferred}); err != nil {
		t.Fatalf("SetSongPersonPreference(songA, personB): %v", err)
	}

	ids, err = store.DistinctSongPersonPreferencePersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctSongPersonPreferencePersonIDs (after set): unexpected error: %v", err)
	}
	sort.Strings(ids)
	want := []string{personA, personB}
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("DistinctSongPersonPreferencePersonIDs = %v, want %v", ids, want)
	}
}

func TestDistinctPersonVocalRangePersonIDs(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	personA := testScopeID("person")
	personB := testScopeID("person")

	t.Cleanup(func() {
		if _, err := store.db.ExecContext(ctx, `DELETE FROM person_vocal_ranges WHERE organization_id = $1`, orgID); err != nil {
			t.Errorf("cleanup: deleting person_vocal_ranges for org %s: %v", orgID, err)
		}
	})

	ids, err := store.DistinctPersonVocalRangePersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctPersonVocalRangePersonIDs (no ranges): unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("DistinctPersonVocalRangePersonIDs (no ranges) = %v, want empty", ids)
	}

	for _, personID := range []string{personA, personB} {
		if _, err := store.SetPersonVocalRange(ctx, orgID, SetPersonVocalRangeParams{
			PersonID: personID, LowestPitchClass: 7, LowestOctave: 3, HighestPitchClass: 2, HighestOctave: 5,
		}); err != nil {
			t.Fatalf("SetPersonVocalRange(%s): %v", personID, err)
		}
	}

	ids, err = store.DistinctPersonVocalRangePersonIDs(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctPersonVocalRangePersonIDs (after set): unexpected error: %v", err)
	}
	sort.Strings(ids)
	want := []string{personA, personB}
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("DistinctPersonVocalRangePersonIDs = %v, want %v", ids, want)
	}
}
