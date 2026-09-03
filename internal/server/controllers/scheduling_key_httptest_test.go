package controllers

// Regression coverage for resolveKeyID's error-reporting contract
// (scheduling.go) - added alongside the fix for a real, previously-silent
// bug: a KeyOverride someone set here that couldn't actually be resolved
// to a real Planning Center key (no arrangement linked, or the
// GetKeys/CreateKey calls themselves failing) used to just fall back to
// the arrangement's default key with zero indication anything went wrong
// - the Build page kept showing the override the user set, while
// Publish quietly shipped a different key to Planning Center. Same
// httptest.Server-stands-in-for-Planning-Center seam as
// rosters_scoped_httptest_test.go/internal/scheduling's
// pco_songs_httptest_test.go - no real network call or credentials, and
// resolveKeyID is DB-free so it doesn't need this package's usual
// no-test-DB-harness workaround.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

// newFakeKeysServer stands in for Planning Center's
// .../arrangements/<id>/keys endpoint: GET returns existing (already an
// exact case-insensitive match for existingKey, unless existingKey is
// empty), POST (CreateKey) succeeds with a fresh id unless createFails is
// set, in which case it answers 500.
func newFakeKeysServer(t *testing.T, existingKey string, createFails bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			data := []map[string]any{}
			if existingKey != "" {
				data = append(data, map[string]any{
					"type":       "Key",
					"id":         "existing-key-id",
					"attributes": map[string]any{"starting_key": existingKey},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case http.MethodPost:
			if createFails {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"detail": "boom"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"type": "Key", "id": "created-key-id", "attributes": map[string]any{}},
			})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// --- No override requested --------------------------------------------

// TestResolveKeyIDNoOverrideReturnsDefaultWithoutACall proves the common
// case (nothing overridden for this occurrence) costs zero PCO calls and
// never reports an error - only a real override attempt should ever hit
// Planning Center or fail.
func TestResolveKeyIDNoOverrideReturnsDefaultWithoutACall(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	keyID, err := resolveKeyID(context.Background(), "song-1", "arr-1", "", "default-key-id")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if keyID != "default-key-id" {
		t.Fatalf("expected default-key-id, got %q", keyID)
	}
	if called {
		t.Fatal("expected no PCO call when there's no override")
	}
}

// --- Override requested, no arrangement to attach it under --------------

func TestResolveKeyIDOverrideWithoutArrangementReturnsErr(t *testing.T) {
	keyID, err := resolveKeyID(context.Background(), "song-1", "", "Bb", "default-key-id")

	if err == nil {
		t.Fatal("expected an error when there's no arrangement to attach the key to")
	}
	if keyID != "default-key-id" {
		t.Fatalf("expected the fallback default-key-id even on error, got %q", keyID)
	}
}

// --- Override matches an existing key ------------------------------------

func TestResolveKeyIDOverrideMatchesExistingKey(t *testing.T) {
	server := newFakeKeysServer(t, "Bb", false)
	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	keyID, err := resolveKeyID(context.Background(), "song-1", "arr-1", "Bb", "default-key-id")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if keyID != "existing-key-id" {
		t.Fatalf("expected the matched existing key id, got %q", keyID)
	}
}

// --- Override needs a new key, create succeeds ---------------------------

func TestResolveKeyIDOverrideCreatesNewKey(t *testing.T) {
	server := newFakeKeysServer(t, "", false)
	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	keyID, err := resolveKeyID(context.Background(), "song-1", "arr-1", "Bb", "default-key-id")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if keyID != "created-key-id" {
		t.Fatalf("expected the newly-created key id, got %q", keyID)
	}
}

// --- Override needs a new key, create fails: THE bug this fix covers ----

// TestResolveKeyIDCreateFailureReturnsErrAndFallback is the actual
// regression case: before this fix, a failed CreateKey (e.g. a 429 under
// load - this app has real production history with those) silently
// returned the arrangement's default key with no error at all, so
// PublishSet/RefreshSongInfo had no way to know the override never made
// it to Planning Center and reported a clean success anyway.
func TestResolveKeyIDCreateFailureReturnsErrAndFallback(t *testing.T) {
	server := newFakeKeysServer(t, "", true)
	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	keyID, err := resolveKeyID(context.Background(), "song-1", "arr-1", "Bb", "default-key-id")

	if err == nil {
		t.Fatal("expected an error when CreateKey fails - this must not look like success")
	}
	if keyID != "default-key-id" {
		t.Fatalf("expected the fallback default-key-id, got %q", keyID)
	}
}
