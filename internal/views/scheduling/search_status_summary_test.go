package scheduling

import "testing"

// Finding 2 (4.1.3 Status Messages, docs/plans/shipped/wcag-aa-audit-plan.md)
// - the as-you-type search regions' separate summary live regions
// (candidateSearchStatusOOB/buildPlansSummary) announce a short sentence
// instead of the raw swap target's full result list. These tests cover the
// summary text logic without needing to render any HTML.

func TestCandidateSearchSummary(t *testing.T) {
	cand := []CandidateSongView{{SongID: "1", Title: "Song A"}}

	if got := candidateSearchSummary(nil, "", "", "", 0, false, ""); got != "" {
		t.Errorf("browse state (no query/filter) should announce nothing, got %q", got)
	}
	if got := candidateSearchSummary(nil, "", "", "", 0, false, "boom"); got != "" {
		t.Errorf("error state should announce nothing (the error paragraph announces itself), got %q", got)
	}
	if got := candidateSearchSummary(nil, "grace", "", "", 0, false, ""); got != "No matching songs found" {
		t.Errorf("no matches = %q, want %q", got, "No matching songs found")
	}
	if got := candidateSearchSummary(cand, "grace", "", "", 1, false, ""); got != "1 matching song(s) found" {
		t.Errorf("one match = %q, want %q", got, "1 matching song(s) found")
	}
	if got := candidateSearchSummary(cand, "grace", "", "", 40, true, ""); got != "Showing 1 of 40 matching songs - refine your search for more specific results" {
		t.Errorf("capped = %q", got)
	}
	// A role/context filter alone (no typed query) is still an active
	// search, same as query - should announce too.
	if got := candidateSearchSummary(cand, "", "opener", "", 1, false, ""); got == "" {
		t.Error("an active role filter with no query should still announce a summary")
	}
}

func TestBuildPlansSummary(t *testing.T) {
	if got := buildPlansSummary(nil, "upcoming"); got != "No upcoming plans found" {
		t.Errorf("empty upcoming = %q, want %q", got, "No upcoming plans found")
	}
	if got := buildPlansSummary(nil, "past"); got != "No past plans found" {
		t.Errorf("empty past = %q, want %q", got, "No past plans found")
	}
	plans := []PlanRow{{ID: "1"}, {ID: "2"}}
	if got := buildPlansSummary(plans, "upcoming"); got != "2 plan(s) found" {
		t.Errorf("two plans = %q, want %q", got, "2 plan(s) found")
	}
}
