package scheduling

import "context"

// GetPlanNote returns this org's at-a-glance note for one plan ("" when
// nothing's been saved yet, same "no row is just an empty note" convention
// as PreferredFirstSongAnchorTitle) - see plan_notes (migration 00032).
// Deliberately its own table, not a column on Set: a Set row only exists
// once someone's actually started a draft for this plan, but the point of
// this note is spotting a plan *before* ever opening it in the Build
// page's plan picker list (see the controller's combinedPlansForServiceType),
// so it has to be readable independently of whether a draft exists.
func (s *Store) GetPlanNote(ctx context.Context, organizationID, serviceTypeID, planID string) (string, error) {
	var note string
	err := s.db.QueryRowContext(ctx, `
		SELECT note FROM plan_notes
		WHERE organization_id = $1 AND service_type_id = $2 AND plan_id = $3
	`, organizationID, serviceTypeID, planID).Scan(&note)
	if err != nil {
		// sql.ErrNoRows means "nothing saved yet" here, same as
		// PreferredFirstSongAnchorTitle's ok=false case - not a real
		// failure the caller needs to surface, so it's folded into the
		// zero-value "" return rather than propagated.
		return "", nil
	}
	return note, nil
}

// ListPlanNotesForServiceType returns every saved note for this org's
// service type, keyed by plan id - used by the Build page's plan picker
// (combinedPlansForServiceType) to annotate a whole page of plans and to
// search note text, without a query per plan.
func (s *Store) ListPlanNotesForServiceType(ctx context.Context, organizationID, serviceTypeID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plan_id, note FROM plan_notes
		WHERE organization_id = $1 AND service_type_id = $2
	`, organizationID, serviceTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := map[string]string{}
	for rows.Next() {
		var planID, note string
		if err := rows.Scan(&planID, &note); err != nil {
			return nil, err
		}
		notes[planID] = note
	}
	return notes, rows.Err()
}

// UpsertPlanNote saves this org's at-a-glance note for one plan, creating
// the row on first use - same upsert shape as RememberFirstSongAnchor.
// note == "" is a valid save (clearing a previously-set note), not treated
// specially.
func (s *Store) UpsertPlanNote(ctx context.Context, organizationID, serviceTypeID, planID, note string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plan_notes (organization_id, service_type_id, plan_id, note, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (organization_id, service_type_id, plan_id) DO UPDATE
			SET note = EXCLUDED.note, updated_at = now()
	`, organizationID, serviceTypeID, planID, note)
	return err
}
