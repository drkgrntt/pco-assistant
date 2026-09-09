package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

// TestLeaderAssignmentErr covers leaderAssignmentErr's one special case (a
// 422 from CreateItemAssignment - PCO rejecting a leader who isn't yet
// scheduled on the plan, see this file's own doc comment) against
// friendlyPCOErr's plain fallback for everything else.
func TestLeaderAssignmentErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "422 becomes the not-scheduled-yet message",
			err:  &pco.RequestError{StatusCode: 422},
			want: "they're not published to this plan's team yet (see the Team tab) - publish the team, then Publish/Refresh song info again",
		},
		{
			name: "401 still becomes friendlyPCOErr's permission message",
			err:  &pco.RequestError{StatusCode: 401},
			want: wantFriendlyMsg,
		},
		{
			name: "500 passes through unchanged",
			err:  &pco.RequestError{StatusCode: 500},
			want: (&pco.RequestError{StatusCode: 500}).Error(),
		},
		{
			name: "plain error passes through unchanged",
			err:  errors.New("boom"),
			want: "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := leaderAssignmentErr(tt.err); got != tt.want {
				t.Errorf("leaderAssignmentErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// assignmentData builds a minimal pco.ItemAssignmentData for
// personAssignmentFor/syncLeaderAssignment tests - assignableType/personID
// left empty is a non-Person assignment (e.g. a TeamPosition placeholder),
// which personAssignmentFor must skip over.
func assignmentData(id, assignableType, personID string) pco.ItemAssignmentData {
	a := pco.ItemAssignmentData{
		Type:       "ItemAssignment",
		ID:         id,
		Attributes: pco.ItemAssignmentAttributes{AssignableType: assignableType},
	}
	if personID != "" {
		a.Relationships.Assignable.Data = &pco.General{Type: "Person", ID: personID}
	}
	return a
}

// TestPersonAssignmentFor covers personAssignmentFor's lookup - a pure
// function, no PCO call involved.
func TestPersonAssignmentFor(t *testing.T) {
	byItem := map[string][]pco.ItemAssignmentData{
		"item-1": {
			assignmentData("a-1", pco.ItemAssignmentAssignableTypeTeamPosition, ""),
			assignmentData("a-2", pco.ItemAssignmentAssignableTypePerson, "person-1"),
		},
	}

	got := personAssignmentFor(byItem, "item-1")
	if got == nil || got.ID != "a-2" {
		t.Fatalf("expected the Person-type assignment (a-2), got %+v", got)
	}

	if got := personAssignmentFor(byItem, "item-missing"); got != nil {
		t.Errorf("expected nil for an item with no assignments, got %+v", got)
	}

	byItemNoPersonRow := map[string][]pco.ItemAssignmentData{
		"item-2": {assignmentData("a-3", pco.ItemAssignmentAssignableTypeTeamPosition, "")},
	}
	if got := personAssignmentFor(byItemNoPersonRow, "item-2"); got != nil {
		t.Errorf("expected nil when only a TeamPosition assignment exists, got %+v", got)
	}
}

// itemAssignmentTestServer records every request it receives (method+path)
// into calls, and serves a bare "created" response for a POST or 204 for a
// DELETE - enough for syncLeaderAssignment to succeed without caring about
// response content beyond a create's new id.
func itemAssignmentTestServer(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"type":"ItemAssignment","id":"new-assignment"}}`))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
}

// TestSyncLeaderAssignment covers syncLeaderAssignment's decision table -
// see its own doc comment (leader_note.go) for the no-op/create/delete/
// reassign cases this exercises.
func TestSyncLeaderAssignment(t *testing.T) {
	personAssignment := assignmentData("existing-assignment", pco.ItemAssignmentAssignableTypePerson, "person-1")

	tests := []struct {
		name           string
		itemID         string
		current        *pco.ItemAssignmentData
		leaderPersonID string
		wantCalls      []string
	}{
		{
			name:           "unpublished song (no item id) is a no-op",
			itemID:         "",
			current:        nil,
			leaderPersonID: "person-1",
			wantCalls:      nil,
		},
		{
			name:           "no existing assignment, no leader set - no-op",
			itemID:         "item-1",
			current:        nil,
			leaderPersonID: "",
			wantCalls:      nil,
		},
		{
			name:           "no existing assignment, leader set - creates",
			itemID:         "item-1",
			current:        nil,
			leaderPersonID: "person-2",
			wantCalls:      []string{"POST /services/v2/service_types/st-1/plans/p-1/items/item-1/item_assignments"},
		},
		{
			name:           "existing assignment matches desired leader - no-op",
			itemID:         "item-1",
			current:        &personAssignment,
			leaderPersonID: "person-1",
			wantCalls:      nil,
		},
		{
			name:           "leader cleared - deletes the existing assignment",
			itemID:         "item-1",
			current:        &personAssignment,
			leaderPersonID: "",
			wantCalls:      []string{"DELETE /services/v2/service_types/st-1/plans/p-1/items/item-1/item_assignments/existing-assignment"},
		},
		{
			name:           "leader reassigned to someone else - deletes then creates",
			itemID:         "item-1",
			current:        &personAssignment,
			leaderPersonID: "person-2",
			wantCalls: []string{
				"DELETE /services/v2/service_types/st-1/plans/p-1/items/item-1/item_assignments/existing-assignment",
				"POST /services/v2/service_types/st-1/plans/p-1/items/item-1/item_assignments",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			server := itemAssignmentTestServer(t, &calls)
			defer server.Close()
			restore := pco.SetBaseURLForTesting(server.URL)
			defer restore()

			if err := syncLeaderAssignment(context.Background(), "st-1", "p-1", tt.itemID, tt.current, tt.leaderPersonID); err != nil {
				t.Fatalf("syncLeaderAssignment returned an error: %v", err)
			}

			if len(calls) != len(tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if calls[i] != want {
					t.Errorf("call %d = %q, want %q", i, calls[i], want)
				}
			}
		})
	}
}
