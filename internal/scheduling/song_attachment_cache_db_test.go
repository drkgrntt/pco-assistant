package scheduling

// DB-backed tests for song_attachment_cache (migration 00035). Gated behind
// RUN_DB_TESTS=1 - see person_data_cache_db_test.go's doc comment for why,
// and for the dbTestStore/testScopeID helpers this file reuses.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/scheduling/... -run SongAttachment -v

import (
	"context"
	"testing"
)

// cleanupSongAttachmentCache deletes every row song_attachment_cache could
// hold for organizationID - registered via t.Cleanup, same pattern as
// cleanupSongArrangementCache.
func cleanupSongAttachmentCache(t *testing.T, store *Store, organizationID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := store.db.ExecContext(context.Background(), `DELETE FROM song_attachment_cache WHERE organization_id = $1`, organizationID); err != nil {
			t.Errorf("cleanup: deleting from song_attachment_cache for org %s: %v", organizationID, err)
		}
	})
}

// TestUpsertSongAttachmentsReplacesAll confirms UpsertSongAttachments is a
// full replace-all for one song, not a merge - an attachment present on an
// earlier call but missing from a later one (removed in Planning Center)
// must not linger.
func TestUpsertSongAttachmentsReplacesAll(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songID := testScopeID("song")
	cleanupSongAttachmentCache(t, store, orgID)

	if err := store.UpsertSongAttachments(ctx, orgID, songID, []SongAttachmentInfo{
		{PCOAttachmentID: "att-1", Filename: "Lead Sheet.pdf", ContentType: "application/pdf"},
		{PCOAttachmentID: "att-2", Filename: "Chart.pdf", ContentType: "application/pdf"},
	}); err != nil {
		t.Fatalf("UpsertSongAttachments (first): %v", err)
	}

	cache, err := store.SongAttachments(ctx, orgID)
	if err != nil {
		t.Fatalf("SongAttachments (after first upsert): %v", err)
	}
	if got := len(cache[songID]); got != 2 {
		t.Fatalf("expected 2 cached attachments, got %d", got)
	}

	// Re-sync with only one of the two attachments still present - the
	// removed one must actually disappear, not just get overwritten.
	if err := store.UpsertSongAttachments(ctx, orgID, songID, []SongAttachmentInfo{
		{PCOAttachmentID: "att-2", Filename: "Chart.pdf", ContentType: "application/pdf"},
	}); err != nil {
		t.Fatalf("UpsertSongAttachments (second): %v", err)
	}

	cache, err = store.SongAttachments(ctx, orgID)
	if err != nil {
		t.Fatalf("SongAttachments (after second upsert): %v", err)
	}
	attachments := cache[songID]
	if len(attachments) != 1 {
		t.Fatalf("expected 1 cached attachment after replace-all, got %d: %+v", len(attachments), attachments)
	}
	if attachments[0].PCOAttachmentID != "att-2" {
		t.Errorf("expected the surviving attachment to be att-2, got %q", attachments[0].PCOAttachmentID)
	}
}

// TestUpsertSongAttachmentsEmptyClearsCache confirms syncing a song down to
// zero attachments (every attachment removed in Planning Center) actually
// empties its cache row, rather than erroring or leaving stale rows behind.
func TestUpsertSongAttachmentsEmptyClearsCache(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songID := testScopeID("song")
	cleanupSongAttachmentCache(t, store, orgID)

	if err := store.UpsertSongAttachments(ctx, orgID, songID, []SongAttachmentInfo{
		{PCOAttachmentID: "att-1", Filename: "Lead Sheet.pdf"},
	}); err != nil {
		t.Fatalf("UpsertSongAttachments (seed): %v", err)
	}

	if err := store.UpsertSongAttachments(ctx, orgID, songID, nil); err != nil {
		t.Fatalf("UpsertSongAttachments (empty): %v", err)
	}

	cache, err := store.SongAttachments(ctx, orgID)
	if err != nil {
		t.Fatalf("SongAttachments: %v", err)
	}
	if got := len(cache[songID]); got != 0 {
		t.Fatalf("expected 0 cached attachments after syncing to empty, got %d", got)
	}
}

// TestSongAttachmentsScopedByOrg confirms SongAttachments never returns
// another organization's cached attachments.
func TestSongAttachmentsScopedByOrg(t *testing.T) {
	store := dbTestStore(t)
	ctx := context.Background()
	orgA := testScopeID("org")
	orgB := testScopeID("org")
	songID := testScopeID("song")
	cleanupSongAttachmentCache(t, store, orgA)
	cleanupSongAttachmentCache(t, store, orgB)

	if err := store.UpsertSongAttachments(ctx, orgA, songID, []SongAttachmentInfo{
		{PCOAttachmentID: "att-1", Filename: "Org A file.pdf"},
	}); err != nil {
		t.Fatalf("UpsertSongAttachments (org A): %v", err)
	}

	cacheB, err := store.SongAttachments(ctx, orgB)
	if err != nil {
		t.Fatalf("SongAttachments (org B): %v", err)
	}
	if _, ok := cacheB[songID]; ok {
		t.Errorf("expected org B's cache to have no entry for a song only cached under org A, got %+v", cacheB[songID])
	}
}
