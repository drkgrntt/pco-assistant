package scheduling

// DB-backed test for DistinctSongTagValues - see
// person_data_cache_db_test.go's package doc comment for why this needs a
// real Postgres (RUN_DB_TESTS=1) rather than the plain unit-test style
// song_tag_suggestions_test.go uses: it's grouping real rows via SQL
// GROUP-by-scan, not just exercising in-memory logic.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run DistinctSongTagValues -v

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

func TestDistinctSongTagValues(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songA := testScopeID("song")
	songB := testScopeID("song")

	t.Cleanup(func() {
		if _, err := store.db.ExecContext(ctx, `DELETE FROM song_tags WHERE organization_id = $1`, orgID); err != nil {
			t.Errorf("cleanup: deleting song_tags for org %s: %v", orgID, err)
		}
	})

	// No tags yet - the miss case, same shape ListSongTags/other lookups
	// return (an empty, non-nil map), not an error.
	values, err := store.DistinctSongTagValues(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctSongTagValues (no tags): unexpected error: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("DistinctSongTagValues (no tags) = %v, want empty", values)
	}

	// Two songs tagged "King" under theme (a duplicate value across songs
	// must collapse to one distinct entry), plus one scripture reference,
	// to prove grouping is per-category and values are deduped+sorted.
	for _, params := range []CreateSongTagParams{
		{SongID: songA, Category: TagCategoryTheme, Value: "King"},
		{SongID: songB, Category: TagCategoryTheme, Value: "King"},
		{SongID: songA, Category: TagCategoryTheme, Value: "Grace"},
		{SongID: songA, Category: TagCategoryScripture, Value: "Psalm 23"},
	} {
		if _, err := store.CreateSongTag(ctx, orgID, params); err != nil {
			t.Fatalf("CreateSongTag(%+v): %v", params, err)
		}
	}

	values, err = store.DistinctSongTagValues(ctx, orgID)
	if err != nil {
		t.Fatalf("DistinctSongTagValues (after tagging): unexpected error: %v", err)
	}

	wantTheme := []string{"Grace", "King"}
	gotTheme := values[TagCategoryTheme]
	sort.Strings(gotTheme)
	if !reflect.DeepEqual(gotTheme, wantTheme) {
		t.Errorf("DistinctSongTagValues[%q] = %v, want %v (duplicate \"King\" across songA/songB should collapse to one)", TagCategoryTheme, gotTheme, wantTheme)
	}

	wantScripture := []string{"Psalm 23"}
	if gotScripture := values[TagCategoryScripture]; !reflect.DeepEqual(gotScripture, wantScripture) {
		t.Errorf("DistinctSongTagValues[%q] = %v, want %v", TagCategoryScripture, gotScripture, wantScripture)
	}

	// A different org's tags must never leak in.
	otherOrgID := testScopeID("org")
	if _, err := store.CreateSongTag(ctx, otherOrgID, CreateSongTagParams{SongID: songA, Category: TagCategoryIdea, Value: "Other org's idea"}); err != nil {
		t.Fatalf("CreateSongTag (other org): %v", err)
	}
	t.Cleanup(func() {
		if _, err := store.db.ExecContext(ctx, `DELETE FROM song_tags WHERE organization_id = $1`, otherOrgID); err != nil {
			t.Errorf("cleanup: deleting song_tags for org %s: %v", otherOrgID, err)
		}
	})

	if _, ok := values[TagCategoryIdea]; ok {
		t.Errorf("DistinctSongTagValues(orgID) leaked a value from a different organization: %v", values[TagCategoryIdea])
	}
}
