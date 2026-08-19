package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// A Roster is a draft position-fill plan for one PCO plan - the people
// equivalent of a Set (see sets.go). Local to this app only; nothing here is
// written back to Planning Center except via the controller's Publish flow.
// There's at most one per (service_type_id, plan_id) pair.
type Roster struct {
	ID            int64     `json:"id"`
	ServiceTypeID string    `json:"service_type_id"`
	PlanID        string    `json:"plan_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RosterAssignment is one person filling one named position slot. Unlike
// SetSong, there's no position/anchor ordering here - filling a slot has no
// sequence to it, so this is a flatter row.
type RosterAssignment struct {
	ID               int64     `json:"id"`
	RosterID         int64     `json:"roster_id"`
	PersonID         string    `json:"person_id"`
	TeamID           string    `json:"team_id"`
	TeamPositionName string    `json:"team_position_name"`
	CreatedAt        time.Time `json:"created_at"`
	// PCOPlanPersonID is set once this assignment has been pushed to
	// Planning Center as a real PlanPerson (see PublishRoster) - empty means
	// not published yet.
	PCOPlanPersonID string `json:"pco_plan_person_id"`
	Status          string `json:"status"`
	// PendingDelete marks a published assignment that was removed locally -
	// its PCO PlanPerson still needs deleting on the next publish.
	// ListRosterAssignments never returns these; ListPendingRosterDeletions does.
	PendingDelete bool   `json:"pending_delete"`
	Notes         string `json:"notes"`
}

// StatusUnconfirmed is what a newly-created (never-published) assignment
// defaults to - the honest state before anyone's actually confirmed it,
// matching PlanPerson's own default meaning.
const StatusUnconfirmed = "Unconfirmed"

// FindRoster looks up an existing roster without creating one - see
// sets.FindSet for why this exists alongside GetOrCreateRoster.
func (s *Store) FindRoster(ctx context.Context, serviceTypeID, planID string) (roster Roster, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT id, service_type_id, plan_id, created_at, updated_at
		FROM rosters WHERE service_type_id = $1 AND plan_id = $2
	`, serviceTypeID, planID).Scan(&roster.ID, &roster.ServiceTypeID, &roster.PlanID, &roster.CreatedAt, &roster.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Roster{}, false, nil
	}
	if err != nil {
		return Roster{}, false, err
	}
	return roster, true, nil
}

// GetOrCreateRoster returns the draft roster for a plan, creating an empty
// one on first use - see sets.GetOrCreateSet.
func (s *Store) GetOrCreateRoster(ctx context.Context, serviceTypeID, planID string) (Roster, error) {
	if serviceTypeID == "" || planID == "" {
		return Roster{}, errors.New("service_type_id and plan_id are required")
	}

	var roster Roster
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO rosters (service_type_id, plan_id)
		VALUES ($1, $2)
		ON CONFLICT (service_type_id, plan_id) DO UPDATE SET updated_at = rosters.updated_at
		RETURNING id, service_type_id, plan_id, created_at, updated_at
	`, serviceTypeID, planID).Scan(&roster.ID, &roster.ServiceTypeID, &roster.PlanID, &roster.CreatedAt, &roster.UpdatedAt)
	if err != nil {
		return Roster{}, err
	}

	return roster, nil
}

// ListRosterAssignments returns the roster's active (non-tombstoned)
// assignments - see ListPendingRosterDeletions for ones awaiting removal
// from Planning Center.
func (s *Store) ListRosterAssignments(ctx context.Context, rosterID int64) ([]RosterAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, roster_id, person_id, team_id, team_position_name, created_at, pco_plan_person_id, status, notes
		FROM roster_assignments
		WHERE roster_id = $1 AND pending_delete = false
		ORDER BY created_at ASC
	`, rosterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assignments := []RosterAssignment{}
	for rows.Next() {
		var ra RosterAssignment
		var pcoPlanPersonID sql.NullString
		if err := rows.Scan(&ra.ID, &ra.RosterID, &ra.PersonID, &ra.TeamID, &ra.TeamPositionName, &ra.CreatedAt, &pcoPlanPersonID, &ra.Status, &ra.Notes); err != nil {
			return nil, err
		}
		ra.PCOPlanPersonID = pcoPlanPersonID.String
		assignments = append(assignments, ra)
	}

	return assignments, rows.Err()
}

// ListPendingRosterDeletions returns assignments that were removed locally
// after already being published - their pco_plan_person_id still needs
// deleting in Planning Center on the next PublishRoster run.
func (s *Store) ListPendingRosterDeletions(ctx context.Context, rosterID int64) ([]RosterAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, roster_id, person_id, team_id, team_position_name, created_at, pco_plan_person_id, status, notes
		FROM roster_assignments
		WHERE roster_id = $1 AND pending_delete = true
	`, rosterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assignments := []RosterAssignment{}
	for rows.Next() {
		var ra RosterAssignment
		var pcoPlanPersonID sql.NullString
		if err := rows.Scan(&ra.ID, &ra.RosterID, &ra.PersonID, &ra.TeamID, &ra.TeamPositionName, &ra.CreatedAt, &pcoPlanPersonID, &ra.Status, &ra.Notes); err != nil {
			return nil, err
		}
		ra.PCOPlanPersonID = pcoPlanPersonID.String
		ra.PendingDelete = true
		assignments = append(assignments, ra)
	}

	return assignments, rows.Err()
}

// AddPersonToPosition assigns personID to fill (team_id, team_position_name)
// on this roster. Refuses a duplicate active assignment of the same person
// to the same slot - unlike songs, there's no "duplicate on purpose" case
// for a position assignment.
func (s *Store) AddPersonToPosition(ctx context.Context, rosterID int64, personID, teamID, teamPositionName string) (RosterAssignment, error) {
	if personID == "" || teamID == "" || teamPositionName == "" {
		return RosterAssignment{}, errors.New("person_id, team_id, and team_position_name are required")
	}

	var alreadyPresent bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM roster_assignments
			WHERE roster_id = $1 AND person_id = $2 AND team_id = $3 AND team_position_name = $4 AND pending_delete = false
		)
	`, rosterID, personID, teamID, teamPositionName).Scan(&alreadyPresent); err != nil {
		return RosterAssignment{}, err
	}
	if alreadyPresent {
		return RosterAssignment{}, fmt.Errorf("that person is already assigned to this position")
	}

	var ra RosterAssignment
	var pcoPlanPersonID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO roster_assignments (roster_id, person_id, team_id, team_position_name, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, roster_id, person_id, team_id, team_position_name, created_at, pco_plan_person_id, status, notes
	`, rosterID, personID, teamID, teamPositionName, StatusUnconfirmed).Scan(
		&ra.ID, &ra.RosterID, &ra.PersonID, &ra.TeamID, &ra.TeamPositionName, &ra.CreatedAt, &pcoPlanPersonID, &ra.Status, &ra.Notes,
	)
	if err != nil {
		return RosterAssignment{}, err
	}
	ra.PCOPlanPersonID = pcoPlanPersonID.String

	return ra, nil
}

// RemovePersonFromPosition removes rowID from the active roster. If it was
// already published, the row is kept as a tombstone (pending_delete) so the
// next PublishRoster run knows to delete its PCO PlanPerson too - see
// sets.RemoveSongFromSet, which this mirrors (minus position renumbering,
// since assignments aren't ordered).
func (s *Store) RemovePersonFromPosition(ctx context.Context, rosterID, rowID int64) error {
	var pcoPlanPersonID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT pco_plan_person_id FROM roster_assignments WHERE id = $1 AND roster_id = $2 AND pending_delete = false
	`, rowID, rosterID).Scan(&pcoPlanPersonID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}

	if pcoPlanPersonID.Valid && pcoPlanPersonID.String != "" {
		_, err = s.db.ExecContext(ctx, `UPDATE roster_assignments SET pending_delete = true WHERE id = $1`, rowID)
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM roster_assignments WHERE id = $1`, rowID)
	return err
}

// MarkAssignmentPublished records that rowID was pushed to Planning Center
// as PlanPerson planPersonID, so a later PublishRoster run treats it as
// already-published.
func (s *Store) MarkAssignmentPublished(ctx context.Context, rosterID, rowID int64, planPersonID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE roster_assignments SET pco_plan_person_id = $1 WHERE id = $2 AND roster_id = $3 AND pending_delete = false
	`, planPersonID, rowID, rosterID)
	return err
}

// DeleteAssignmentRow permanently removes a tombstoned row after its PCO
// PlanPerson has actually been deleted (see PublishRoster).
func (s *Store) DeleteAssignmentRow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM roster_assignments WHERE id = $1`, id)
	return err
}

// RevertRoster discards every local assignment (including tombstoned
// pending deletions) and replaces them with importRows - used to reset a
// roster back to exactly what Planning Center currently has. See
// sets.RevertSet, which this mirrors; the caller derives importRows from a
// live GetTeamMembers call.
func (s *Store) RevertRoster(ctx context.Context, rosterID int64, importRows []RosterAssignment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM roster_assignments WHERE roster_id = $1`, rosterID); err != nil {
		return err
	}

	for _, row := range importRows {
		status := row.Status
		if status == "" {
			status = StatusUnconfirmed
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO roster_assignments (roster_id, person_id, team_id, team_position_name, pco_plan_person_id, status)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, rosterID, row.PersonID, row.TeamID, row.TeamPositionName, row.PCOPlanPersonID, status); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RosterConflict is one avoid_pairing pair already present on a roster.
type RosterConflict struct {
	PersonID      string `json:"person_id"`
	ConflictsWith string `json:"conflicts_with"`
}

// AvoidPairingConflicts is the person-relationship equivalent of
// sets.AvoidPairings: candidates maps a not-yet-assigned person id to the
// roster member id(s) they're flagged against; existing lists pairs where
// both people are already assigned (each pair reported once). Returns zero
// values (not an error) for an empty roster.
func (s *Store) AvoidPairingConflicts(ctx context.Context, rosterID int64) (candidates map[string][]string, existing []RosterConflict, err error) {
	assignments, err := s.ListRosterAssignments(ctx, rosterID)
	if err != nil {
		return nil, nil, err
	}
	if len(assignments) == 0 {
		return nil, nil, nil
	}

	inRoster := make(map[string]bool, len(assignments))
	for _, ra := range assignments {
		inRoster[ra.PersonID] = true
	}

	candidates = map[string][]string{}
	seenExisting := map[string]bool{}

	for _, ra := range assignments {
		rels, err := s.ListPersonRelationships(ctx, ra.PersonID)
		if err != nil {
			return nil, nil, err
		}

		for _, rel := range rels {
			if rel.RelationshipType != PersonRelationshipAvoidPairing {
				continue
			}

			other := rel.PersonIDB
			if rel.PersonIDA != ra.PersonID {
				other = rel.PersonIDA
			}

			if inRoster[other] {
				a, b := ra.PersonID, other
				if a > b {
					a, b = b, a
				}
				key := a + "|" + b
				if !seenExisting[key] {
					seenExisting[key] = true
					existing = append(existing, RosterConflict{PersonID: a, ConflictsWith: b})
				}
				continue
			}

			candidates[other] = append(candidates[other], ra.PersonID)
		}
	}

	return candidates, existing, nil
}

// PersonSuggestion is one recommended person, with every reason they're
// being recommended.
type PersonSuggestion struct {
	PersonID          string   `json:"person_id"`
	RelationshipTypes []string `json:"relationship_types"`
	Notes             []string `json:"notes"`
}

// RelationshipSuggestions suggests people to add next, based on
// works_well/mentorship relationships touching anyone already on the
// roster. This deliberately differs from sets.Recommendations, which bases
// suggestions on "the last song added" because song order matters -
// position assignments have no order, so this aggregates across *every*
// current roster member instead, excluding people already assigned. Returns
// nil (not an error) for an empty roster.
func (s *Store) RelationshipSuggestions(ctx context.Context, rosterID int64) ([]PersonSuggestion, error) {
	assignments, err := s.ListRosterAssignments(ctx, rosterID)
	if err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return nil, nil
	}

	inRoster := make(map[string]bool, len(assignments))
	for _, ra := range assignments {
		inRoster[ra.PersonID] = true
	}

	bySuggestion := map[string]*PersonSuggestion{}
	var order []string

	for _, ra := range assignments {
		rels, err := s.ListPersonRelationships(ctx, ra.PersonID)
		if err != nil {
			return nil, err
		}

		for _, rel := range rels {
			if rel.RelationshipType != PersonRelationshipWorksWell && rel.RelationshipType != PersonRelationshipMentorship {
				continue
			}

			other := rel.PersonIDB
			if rel.PersonIDA != ra.PersonID {
				other = rel.PersonIDA
			}
			if inRoster[other] {
				continue
			}

			sug, ok := bySuggestion[other]
			if !ok {
				sug = &PersonSuggestion{PersonID: other}
				bySuggestion[other] = sug
				order = append(order, other)
			}
			sug.RelationshipTypes = append(sug.RelationshipTypes, rel.RelationshipType)
			if rel.Notes != "" {
				sug.Notes = append(sug.Notes, rel.Notes)
			}
		}
	}

	suggestions := make([]PersonSuggestion, 0, len(order))
	for _, personID := range order {
		suggestions = append(suggestions, *bySuggestion[personID])
	}

	return suggestions, nil
}
