package controllers

import (
	"reflect"
	"testing"
)

// missingSongIDs backs resolveMissingTitles' backfill (sets_builder.go) -
// see its doc comment for why: SetSongs/pendingDeletions can reference a
// song that's since been archived in PCO and so was excluded from the
// active-only titles map fetchActiveSongs built.

func TestMissingSongIDsReturnsOnlyAbsentIDs(t *testing.T) {
	titles := map[string]string{"1": "Alpha", "2": "Beta"}

	got := missingSongIDs(titles, []string{"1", "3", "2", "4"})
	want := []string{"3", "4"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestMissingSongIDsDeduplicates(t *testing.T) {
	titles := map[string]string{}

	got := missingSongIDs(titles, []string{"1", "1", "2", "1"})
	want := []string{"1", "2"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestMissingSongIDsSkipsEmptyString(t *testing.T) {
	titles := map[string]string{}

	got := missingSongIDs(titles, []string{"", "1", ""})
	want := []string{"1"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestMissingSongIDsAllPresentReturnsNil(t *testing.T) {
	titles := map[string]string{"1": "Alpha", "2": "Beta"}

	got := missingSongIDs(titles, []string{"1", "2"})
	if got != nil {
		t.Errorf("expected nil (nothing missing), got %v", got)
	}
}

func TestMissingSongIDsEmptyInput(t *testing.T) {
	got := missingSongIDs(map[string]string{}, nil)
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
