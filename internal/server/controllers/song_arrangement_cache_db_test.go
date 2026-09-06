package controllers

// DB-backed test for RefreshSetSongArrangementInfo's actual write path -
// fetchAndCacheArrangementInfo (scheduling.go), called directly the same
// way the handler does, against a fake PCO server (pco.SetBaseURLForTesting,
// same seam as song_arrangement_cache_httptest_test.go) and a real Postgres
// store (dbTestController, defined in person_data_cache_db_test.go in this
// same package). RefreshSetSongArrangementInfo itself is a thin *fiber.Ctx
// wrapper around a set-song lookup plus this one call - this repo has no
// harness anywhere for driving a full fiber handler end-to-end (see
// rosters_scoped_httptest_test.go's own doc comment), so this proves the
// one piece of the handler that's both DB-writing and worth a regression
// test: that calling through actually overwrites a stale cached row with
// whatever Planning Center returns right now, which is the entire point of
// this action (see fetchAndCacheArrangementInfo's own doc comment - it has
// no "skip if already cached" guard, unlike the batch sync it's shared
// with).
//
// Gated behind RUN_DB_TESTS=1, same as person_data_cache_db_test.go.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/server/controllers/... -run RefreshSongArrangement -v

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

// newArrangementRefreshFakeServer stands in for Planning Center's
// arrangements and attachments endpoints - the two calls
// fetchAndCacheArrangementInfo now makes per song (see its own doc comment:
// the arrangement fetch, with keys sideloaded via include=keys, plus
// fetchAndCacheSongAttachments' own GetAttachments call) - replying to the
// arrangements request with the given key as a sideloaded Key resource in
// "included", and to the attachments request with an empty list (this
// test's own attachment content isn't the point; song_attachment_cache_test.go/
// song_attachment_cache_db_test.go in internal/scheduling cover that).
// Used twice in the test below with two different keys to prove a second
// call overwrites the first. A request to /keys would mean
// fetchAndCacheArrangementInfo regressed back to its old two-call shape for
// the arrangement fetch itself, so one arriving here fails the test loudly
// instead of silently passing.
func newArrangementRefreshFakeServer(t *testing.T, arrangementID, key string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/arrangements"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"type": "Arrangement",
						"id":   arrangementID,
						"attributes": map[string]any{
							"bpm":             120.0,
							"meter":           "4/4",
							"length":          240,
							"notes":           "some notes",
							"sequence_short":  []string{"Intro", "V1", "C"},
							"has_chord_chart": true,
						},
					},
				},
				"included": []map[string]any{
					{
						"type": "Key",
						"id":   "key-" + key,
						"attributes": map[string]any{
							"starting_key": key,
						},
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
		default:
			t.Errorf("unexpected request to %s (fetchAndCacheArrangementInfo should only ever hit /arrangements and /attachments, with keys sideloaded via include=keys)", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestFetchAndCacheArrangementInfoOverwritesStaleCacheRow proves
// RefreshSetSongArrangementInfo's actual write path (fetchAndCacheArrangementInfo)
// force-overwrites an already-cached row with fresh data - the whole reason
// this manual refresh action exists (the background queue-on-miss sync,
// runSongInfoQueueSync, deliberately never re-fetches a song that's already
// cached; this one call must).
func TestFetchAndCacheArrangementInfoOverwritesStaleCacheRow(t *testing.T) {
	_, store := dbTestController(t)
	ctx := context.Background()
	orgID := testScopeID("org")
	songID := testScopeID("song")
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(ctx, `DELETE FROM song_arrangement_cache WHERE organization_id = $1`, orgID)
	})

	// First fetch: seed a cached row with key "G".
	server := newArrangementRefreshFakeServer(t, "arr-1", "G")
	restore := pco.SetBaseURLForTesting(server.URL)
	first, err := fetchAndCacheArrangementInfo(ctx, store, orgID, songID)
	restore()
	if err != nil {
		t.Fatalf("fetchAndCacheArrangementInfo (first fetch): %v", err)
	}
	if first.Key != "G" {
		t.Fatalf("fetchAndCacheArrangementInfo (first fetch) key = %q, want %q", first.Key, "G")
	}
	if first.Structure != "Intro, V1, C" {
		t.Errorf("fetchAndCacheArrangementInfo (first fetch) structure = %q, want %q", first.Structure, "Intro, V1, C")
	}
	if !first.HasChordChart {
		t.Error("fetchAndCacheArrangementInfo (first fetch) HasChordChart = false, want true")
	}

	cache, err := store.SongArrangementInfo(ctx, orgID)
	if err != nil {
		t.Fatalf("SongArrangementInfo (after first fetch): %v", err)
	}
	if got := cache[songID].Key; got != "G" {
		t.Fatalf("cached key after first fetch = %q, want %q", got, "G")
	}
	if got := cache[songID].Structure; got != "Intro, V1, C" {
		t.Errorf("cached structure after first fetch = %q, want %q", got, "Intro, V1, C")
	}
	if !cache[songID].HasChordChart {
		t.Error("cached HasChordChart after first fetch = false, want true")
	}

	// Second fetch, simulating the song's key having changed in Planning
	// Center since the first cache - this is the manual-refresh scenario
	// RefreshSetSongArrangementInfo exists for. A plain re-render (which
	// only ever reads the cache on a hit - see resolveArrangementCacheInfo)
	// would never call Planning Center again on its own; only this forced
	// call does.
	server2 := newArrangementRefreshFakeServer(t, "arr-1", "D")
	restore2 := pco.SetBaseURLForTesting(server2.URL)
	second, err := fetchAndCacheArrangementInfo(ctx, store, orgID, songID)
	restore2()
	if err != nil {
		t.Fatalf("fetchAndCacheArrangementInfo (second fetch): %v", err)
	}
	if second.Key != "D" {
		t.Fatalf("fetchAndCacheArrangementInfo (second fetch) key = %q, want %q (forced re-fetch must overwrite, not skip)", second.Key, "D")
	}

	cache, err = store.SongArrangementInfo(ctx, orgID)
	if err != nil {
		t.Fatalf("SongArrangementInfo (after second fetch): %v", err)
	}
	if got := cache[songID].Key; got != "D" {
		t.Fatalf("cached key after second fetch = %q, want %q - stale row was not overwritten", got, "D")
	}

	var rowCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM song_arrangement_cache WHERE organization_id = $1 AND song_id = $2`, orgID, songID).Scan(&rowCount); err != nil {
		t.Fatalf("counting song_arrangement_cache rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("song_arrangement_cache has %d rows for this song after two fetches, want exactly 1 (upsert, not a second insert)", rowCount)
	}
}
