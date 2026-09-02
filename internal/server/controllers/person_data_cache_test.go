package controllers

import (
	"reflect"
	"testing"
	"time"
)

// --- personSyncBatches ------------------------------------------------------
// Mirrors song_arrangement_cache_test.go's songSyncBatches coverage - see
// personSyncBatches' own doc comment for why this isn't just a generic
// helper shared with that function.

func personReq(personID string) personSyncRequest {
	return personSyncRequest{organizationID: "org-1", personID: personID, kind: personSyncKindBlockouts}
}

func TestPersonSyncBatchesSplitsEvenly(t *testing.T) {
	reqs := []personSyncRequest{personReq("1"), personReq("2"), personReq("3"), personReq("4")}

	got := personSyncBatches(reqs, 2)

	want := [][]personSyncRequest{{personReq("1"), personReq("2")}, {personReq("3"), personReq("4")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("personSyncBatches(4 reqs, 2) = %v, want %v", got, want)
	}
}

func TestPersonSyncBatchesLastBatchSmaller(t *testing.T) {
	reqs := []personSyncRequest{personReq("1"), personReq("2"), personReq("3")}

	got := personSyncBatches(reqs, 2)

	if len(got) != 2 {
		t.Fatalf("personSyncBatches(3 reqs, 2) produced %d batches, want 2", len(got))
	}
	if len(got[0]) != 2 || len(got[1]) != 1 {
		t.Errorf("personSyncBatches(3 reqs, 2) batch sizes = %d, %d, want 2, 1", len(got[0]), len(got[1]))
	}
}

func TestPersonSyncBatchesFewerReqsThanBatchSize(t *testing.T) {
	reqs := []personSyncRequest{personReq("1"), personReq("2")}

	got := personSyncBatches(reqs, 10)

	want := [][]personSyncRequest{{personReq("1"), personReq("2")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("personSyncBatches(2 reqs, 10) = %v, want %v", got, want)
	}
}

func TestPersonSyncBatchesEmptyInput(t *testing.T) {
	got := personSyncBatches(nil, 10)
	if got != nil {
		t.Errorf("personSyncBatches(nil, 10) = %v, want nil", got)
	}
}

func TestPersonSyncBatchesZeroSizeFallsBackToOneBatch(t *testing.T) {
	reqs := []personSyncRequest{personReq("1"), personReq("2"), personReq("3")}

	got := personSyncBatches(reqs, 0)

	if len(got) != 1 || len(got[0]) != 3 {
		t.Errorf("personSyncBatches(3 reqs, 0) = %v, want a single batch of 3 (non-positive size shouldn't panic or loop forever)", got)
	}
}

func TestPersonSyncBatchesRealBatchSizeCallCount(t *testing.T) {
	// Same sanity check as TestSongSyncBatchesRealBatchSizeCallCount, over
	// personDataSyncBatchSize instead - a 237-request backlog (bigger than
	// any real church's, but plausible as a stress case) should need
	// ceil(237/10) = 24 batches.
	reqs := make([]personSyncRequest, 237)
	for i := range reqs {
		reqs[i] = personReq("person")
	}

	got := personSyncBatches(reqs, personDataSyncBatchSize)

	wantBatches := 24
	if len(got) != wantBatches {
		t.Fatalf("personSyncBatches(237 reqs, %d) produced %d batches, want %d", personDataSyncBatchSize, len(got), wantBatches)
	}
	if len(got[len(got)-1]) != 7 {
		t.Errorf("last batch has %d reqs, want 7 (237 %% %d)", len(got[len(got)-1]), personDataSyncBatchSize)
	}
}

// --- personSyncTryStart ------------------------------------------------------
// Mirrors song_arrangement_cache_test.go's songSyncTryStart coverage.

func TestPersonSyncTryStartFirstCallSucceeds(t *testing.T) {
	inProgress := map[string]bool{}

	if !personSyncTryStart(inProgress, "org-1") {
		t.Error("personSyncTryStart on an org with nothing running = false, want true")
	}
	if !inProgress["org-1"] {
		t.Error("personSyncTryStart should have marked org-1 as running")
	}
}

func TestPersonSyncTryStartSecondCallFails(t *testing.T) {
	inProgress := map[string]bool{}

	if !personSyncTryStart(inProgress, "org-1") {
		t.Fatal("first personSyncTryStart call should succeed")
	}
	if personSyncTryStart(inProgress, "org-1") {
		t.Error("second personSyncTryStart while org-1 is already running = true, want false")
	}
}

func TestPersonSyncTryStartIndependentPerOrg(t *testing.T) {
	inProgress := map[string]bool{}

	if !personSyncTryStart(inProgress, "org-1") {
		t.Fatal("personSyncTryStart for org-1 should succeed")
	}
	if !personSyncTryStart(inProgress, "org-2") {
		t.Error("personSyncTryStart for org-2 while only org-1 is running = false, want true (orgs are independent)")
	}
}

func TestPersonSyncTryStartAfterClearingCanStartAgain(t *testing.T) {
	inProgress := map[string]bool{}

	if !personSyncTryStart(inProgress, "org-1") {
		t.Fatal("first personSyncTryStart call should succeed")
	}
	delete(inProgress, "org-1") // what runPersonDataSync's deferred cleanup does

	if !personSyncTryStart(inProgress, "org-1") {
		t.Error("personSyncTryStart after the previous sync finished (cleared) = false, want true")
	}
}

// --- personCacheStale --------------------------------------------------------

func TestPersonCacheStaleFreshRowNotStale(t *testing.T) {
	if personCacheStale(time.Now().Add(-1 * time.Minute)) {
		t.Error("a row synced 1 minute ago should not be considered stale (personCacheTTL is hours, not minutes)")
	}
}

func TestPersonCacheStaleOldRowIsStale(t *testing.T) {
	if !personCacheStale(time.Now().Add(-24 * time.Hour)) {
		t.Error("a row synced 24 hours ago should be considered stale (personCacheTTL is a few hours)")
	}
}
