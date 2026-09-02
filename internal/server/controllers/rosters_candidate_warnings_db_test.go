package controllers

// computeCandidateWarnings (rosters_candidate_warnings.go) is the on-demand
// counterpart to the eager per-candidate warning computation
// buildRosterBuilderData/buildRosterPositionGroupData used to do for every
// candidate in a position's whole PCO eligibility pool - see
// pco-api-risks.md's cold-start-eligibility-pool addendum. It force-syncs
// one person's cache (syncPersonBlockouts/syncPersonPlanHistory/
// syncPersonTeamPlanHistory, person_data_cache.go) before reading it, so -
// unlike the pure httptest-only version this replaced - it needs a real
// Postgres store, same as person_data_cache_db_test.go/
// rosters_person_data_refresh_db_test.go. This proves it produces the same
// values frequencyWarning/blockoutWarning/crossTeamWarning (rosters_
// warnings.go) would have computed against the same underlying PCO data the
// old eager path fetched, for one fixture per warning kind.
//
// Gated behind RUN_DB_TESTS=1, same as the other DB-backed tests in this
// package.
//
// Run with: RUN_DB_TESTS=1 go test ./internal/server/controllers/... -run ComputeCandidateWarnings -v

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// writeJSON is a small shared helper for the fake PCO servers below.
func writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// newCandidateWarningsFakeServer stands in for the three PCO endpoints
// computeCandidateWarnings' sync calls hit for one candidate: GetPlan (to
// resolve the plan's own sort_date), GetPersonPlanPeople (used by both
// syncPersonTeamPlanHistory and syncPersonPlanHistory), and GetBlockouts.
// planPeopleBody/blockoutsBody are handed back verbatim for every
// plan_people/blockouts request regardless of query string (both sync calls
// hit the identical URL with different `include`/`where[team_id]` params -
// see person_data_cache.go) - matching the real fixture data to each other
// (rather than serving different bodies per include) is what lets a single
// small fixture cover both call sites, same as production data would.
func newCandidateWarningsFakeServer(t *testing.T, planID string, planSortDate time.Time, planPeopleBody, blockoutsBody map[string]any) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/plans/"+planID):
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"type": "Plan",
					"id":   planID,
					"attributes": map[string]any{
						"sort_date": planSortDate.Format(time.RFC3339),
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/plan_people"):
			writeJSON(w, planPeopleBody)
		case strings.HasSuffix(r.URL.Path, "/blockouts"):
			writeJSON(w, blockoutsBody)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

var emptyListBody = map[string]any{"data": []map[string]any{}}

// TestComputeCandidateWarningsFrequencyWarning proves a candidate too close
// to their own "every week" preference on the exact (team, position) slot
// gets the same FrequencyWarning frequencyWarning itself would compute
// against the equivalent otherDates slice.
func TestComputeCandidateWarningsFrequencyWarning(t *testing.T) {
	sc, store := dbTestController(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	planDate := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC) // Sunday
	otherPlanID := "other-plan"
	otherDate := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) // 4 days earlier, same team/position

	planPeopleBody := map[string]any{
		"data": []map[string]any{
			{
				"type":       "PlanPerson",
				"id":         "pp-1",
				"attributes": map[string]any{"team_position_name": "Drums"},
				"relationships": map[string]any{
					"plan": map[string]any{"data": map[string]any{"type": "Plan", "id": otherPlanID}},
					"team": map[string]any{"data": map[string]any{"type": "Team", "id": "t1"}},
				},
			},
		},
		"included": []map[string]any{
			{
				"type":       "Plan",
				"id":         otherPlanID,
				"attributes": map[string]any{"sort_date": otherDate.Format(time.RFC3339)},
			},
		},
	}

	server := newCandidateWarningsFakeServer(t, "plan-1", planDate, planPeopleBody, emptyListBody)
	restore := pco.SetBaseURLForTesting(server.URL)
	got := sc.computeCandidateWarnings(context.Background(), orgID, "service-type-1", "plan-1", personID, "t1", "Drums", pco.SchedulePreferenceEveryWeek)
	restore()

	want := frequencyWarning(pco.SchedulePreferenceEveryWeek, planDate, []time.Time{otherDate})
	if want == "" {
		t.Fatalf("test fixture bug: expected the golden frequencyWarning computation to be non-empty")
	}
	if got.FrequencyWarning != want {
		t.Errorf("expected FrequencyWarning %q, got %q", want, got.FrequencyWarning)
	}
}

// TestComputeCandidateWarningsBlockoutWarning proves a candidate blocked out
// over the plan's own date gets the same BlockoutWarning blockoutWarning
// itself would compute against the equivalent blockout data.
func TestComputeCandidateWarningsBlockoutWarning(t *testing.T) {
	sc, store := dbTestController(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	planDate := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	blockoutsBody := map[string]any{
		"data": []map[string]any{
			{
				"type": "Blockout",
				"id":   "bo-1",
				"attributes": map[string]any{
					"reason":    "On vacation",
					"starts_at": planDate.Add(-24 * time.Hour).Format(time.RFC3339),
					"ends_at":   planDate.Add(24 * time.Hour).Format(time.RFC3339),
				},
			},
		},
	}

	server := newCandidateWarningsFakeServer(t, "plan-1", planDate, emptyListBody, blockoutsBody)
	restore := pco.SetBaseURLForTesting(server.URL)
	got := sc.computeCandidateWarnings(context.Background(), orgID, "service-type-1", "plan-1", personID, "t1", "Drums", "")

	bo, err := pco.GetBlockouts(context.Background(), personID, &pco.BlockoutsParams{Filter: "future"})
	restore()
	if err != nil {
		t.Fatalf("unexpected GetBlockouts error building the golden value: %v", err)
	}
	want := blockoutWarningIfKnown(planDate, bo.Data)
	if want == "" {
		t.Fatalf("test fixture bug: expected the golden blockoutWarning computation to be non-empty")
	}
	if got.BlockoutWarning != want {
		t.Errorf("expected BlockoutWarning %q, got %q", want, got.BlockoutWarning)
	}
}

// TestComputeCandidateWarningsCrossTeamWarning proves a candidate already
// serving another position the same calendar week gets the same
// CrossTeamWarning crossTeamWarning itself would compute against the
// equivalent cross-team labels.
func TestComputeCandidateWarningsCrossTeamWarning(t *testing.T) {
	sc, store := dbTestController(t)
	orgID := testScopeID("org")
	personID := testScopeID("person")
	cleanupPersonDataCache(t, store, orgID)

	planDate := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC) // Sunday
	otherPlanID := "other-plan"
	sameWeekDate := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC) // Thursday, same Mon-Sun week

	planPeopleBody := map[string]any{
		"data": []map[string]any{
			{
				"type":       "PlanPerson",
				"id":         "pp-1",
				"attributes": map[string]any{"team_position_name": "Photography"},
				"relationships": map[string]any{
					"plan": map[string]any{"data": map[string]any{"type": "Plan", "id": otherPlanID}},
					"team": map[string]any{"data": map[string]any{"type": "Team", "id": "t2"}},
				},
			},
		},
		"included": []map[string]any{
			{
				"type":       "Plan",
				"id":         otherPlanID,
				"attributes": map[string]any{"sort_date": sameWeekDate.Format(time.RFC3339)},
			},
			{
				"type":       "Team",
				"id":         "t2",
				"attributes": map[string]any{"name": "Photography Team"},
			},
		},
	}

	server := newCandidateWarningsFakeServer(t, "plan-1", planDate, planPeopleBody, emptyListBody)
	restore := pco.SetBaseURLForTesting(server.URL)
	got := sc.computeCandidateWarnings(context.Background(), orgID, "service-type-1", "plan-1", personID, "t1", "Drums", "")
	restore()

	want := crossTeamWarning([]string{"Photography Team (Thu AM)"})
	if got.CrossTeamWarning != want {
		t.Errorf("expected CrossTeamWarning %q, got %q", want, got.CrossTeamWarning)
	}
}
