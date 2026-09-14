package controllers

// Regression test for the "one fully-staffed team splits into several
// same-named cards" bug: buildRosterBuilderData sorts data.Positions by
// positionTeamSequence before groupPositionsByTeam (roster_builder.templ)
// walks it as contiguous runs. Before this fix, resolveMissingTeamNames
// backfilled only teamNames for a team missing from shared.TeamInfo (every
// fully-staffed team - see that function's own doc comment), leaving
// teamSequence empty for it. positionTeamSequence then fell back to the
// same unknownTeamSequence constant for *every* such team, so two
// different fully-staffed teams (e.g. "Band" and "Vocal Team", both
// missing from shared.TeamInfo) compared equal and the stable sort just
// preserved their local-assignment insertion order instead of grouping
// them - splitting one team's positions across several separate cards
// wherever another team's position landed in between. Confirmed live
// against a real org (Team 62734, "Band").
//
// Same pco.SetBaseURLForTesting/httptest.Server seam as
// rosters_scoped_httptest_test.go's newRosterSharedFetchFakeServer, just
// faking GetTeams (the /teams endpoint resolveMissingTeamNames itself
// calls) instead of team_members/needed_positions.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

func TestResolveMissingTeamNamesBackfillsDistinctSequencePerTeam(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/teams") {
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "Team", "id": "band", "attributes": map[string]any{"name": "Band", "sequence": 1}},
				{"type": "Team", "id": "vocal", "attributes": map[string]any{"name": "Vocal Team", "sequence": 2}},
			},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	teamNames := map[string]string{}
	teamSequence := map[string]int{}
	resolveMissingTeamNames(context.Background(), "service-type-1", teamNames, teamSequence, []string{"band", "vocal"})

	if teamNames["band"] != "Band" || teamNames["vocal"] != "Vocal Team" {
		t.Fatalf("expected both team names backfilled, got %+v", teamNames)
	}

	bandSeq, bandOK := teamSequence["band"]
	vocalSeq, vocalOK := teamSequence["vocal"]
	if !bandOK || !vocalOK {
		t.Fatalf("expected both teams backfilled into teamSequence, got %+v", teamSequence)
	}
	if bandSeq == vocalSeq {
		t.Errorf("expected Band and Vocal Team to get distinct sequence values (both fully-staffed teams missing from shared.TeamInfo previously collided on unknownTeamSequence, splitting one team's positions across several cards), got both = %d", bandSeq)
	}

	// positionTeamSequence is the actual consumer this backfill protects -
	// prove neither team falls back to unknownTeamSequence now that both
	// were resolved.
	if positionTeamSequence(teamSequence, "band") == unknownTeamSequence {
		t.Errorf("expected Band's sequence to resolve, got unknownTeamSequence")
	}
	if positionTeamSequence(teamSequence, "vocal") == unknownTeamSequence {
		t.Errorf("expected Vocal Team's sequence to resolve, got unknownTeamSequence")
	}
}

// TestResolveMissingTeamNamesNilSequenceIsSafe proves the nil-teamSequence
// path (buildRosterPositionGroupData's call site, which renders one
// position group in isolation and has no team-sort to protect) doesn't
// panic on a write attempt - resolveMissingTeamNames must actually check
// for nil rather than assuming every caller passes a map.
func TestResolveMissingTeamNamesNilSequenceIsSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "Team", "id": "band", "attributes": map[string]any{"name": "Band", "sequence": 1}},
			},
		})
	}))
	defer server.Close()

	restore := pco.SetBaseURLForTesting(server.URL)
	defer restore()

	teamNames := map[string]string{}
	resolveMissingTeamNames(context.Background(), "service-type-1", teamNames, nil, []string{"band"})

	if teamNames["band"] != "Band" {
		t.Fatalf("expected team name backfilled even with nil teamSequence, got %+v", teamNames)
	}
}
