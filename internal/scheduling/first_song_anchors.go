package scheduling

import "context"

// RememberFirstSongAnchor upserts, for this org's service type, which item
// title the first song is currently placed after (""  for top of
// service) - see service_type_first_song_anchors (migration 00021) and
// PreferredFirstSongAnchorTitle for how it's read back. Always overwrites
// with the latest state rather than only ever recording the first
// observation, so if a service type's template structure changes over
// time, the memory follows it.
func (s *Store) RememberFirstSongAnchor(ctx context.Context, organizationID, serviceTypeID, anchorItemTitle string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO service_type_first_song_anchors (organization_id, service_type_id, anchor_item_title, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (organization_id, service_type_id) DO UPDATE
			SET anchor_item_title = EXCLUDED.anchor_item_title, updated_at = now()
	`, organizationID, serviceTypeID, anchorItemTitle)
	return err
}

// PreferredFirstSongAnchorTitle returns the remembered item title this
// org's service type's first song is usually placed after ("" meaning top
// of service), and whether anything's been remembered for it at all - ok
// is false only when no row exists yet, not when the remembered title is
// itself "". The caller (AddSetSong, in the controllers package) resolves
// this title against whichever plan is actually being built right now,
// since a title alone doesn't carry the specific PCO item id a new plan
// would need.
func (s *Store) PreferredFirstSongAnchorTitle(ctx context.Context, organizationID, serviceTypeID string) (title string, ok bool) {
	err := s.db.QueryRowContext(ctx, `
		SELECT anchor_item_title FROM service_type_first_song_anchors
		WHERE organization_id = $1 AND service_type_id = $2
	`, organizationID, serviceTypeID).Scan(&title)
	if err != nil {
		return "", false
	}
	return title, true
}
