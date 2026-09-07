package scheduling

import "context"

// SetVocalNoteCategory remembers, per organization + service type, which
// Planning Center Item Note category the song leader (set_songs.
// leader_person_id) should sync into on Publish/Refresh song info - see
// VocalNoteCategoryID below and docs/plans/shipped/song-leader-plan.md's
// "Which note category is 'the vocal one'?" decision (option 2: an
// explicit, one-time per-service-type designation picked from that
// service type's actual configured categories, not fuzzy name matching).
// categoryID "" clears the setting back to "not configured" (the leader
// sync becomes a no-op again) - same upsert-always-overwrites shape as
// RememberFirstSongAnchor (first_song_anchors.go), which this table
// otherwise mirrors.
func (s *Store) SetVocalNoteCategory(ctx context.Context, organizationID, serviceTypeID, categoryID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO service_type_vocal_note_categories (organization_id, service_type_id, category_id, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (organization_id, service_type_id) DO UPDATE
			SET category_id = EXCLUDED.category_id, updated_at = now()
	`, organizationID, serviceTypeID, categoryID)
	return err
}

// VocalNoteCategoryID returns the Planning Center Item Note category id
// this org's service type is currently configured to sync the song leader
// into - "" when nothing's been configured yet (no row, or an explicitly
// cleared one; both mean the same thing to a caller, so this doesn't
// distinguish them the way PreferredFirstSongAnchorTitle's ok return
// does - there's no meaningful "explicitly nothing" state here the way
// "top of service" is a meaningful anchor value). The caller
// (syncLeaderNote, sets_publish.go) treats "" as "feature not configured
// for this service type - do nothing," never an error.
func (s *Store) VocalNoteCategoryID(ctx context.Context, organizationID, serviceTypeID string) (string, error) {
	var categoryID string
	err := s.db.QueryRowContext(ctx, `
		SELECT category_id FROM service_type_vocal_note_categories
		WHERE organization_id = $1 AND service_type_id = $2
	`, organizationID, serviceTypeID).Scan(&categoryID)
	if err != nil {
		// sql.ErrNoRows means "nothing configured yet" - same "fold the
		// not-found case into the zero-value return" convention
		// GetPlanNote (plan_notes.go) already uses, not a real failure the
		// caller needs to handle separately.
		return "", nil
	}
	return categoryID, nil
}
