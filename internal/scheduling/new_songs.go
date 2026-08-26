package scheduling

import "context"

// TrackNewSong marks songID as having been observed, at some point, with no
// Planning Center scheduling history at all - see tracked_new_songs
// (migration 00024). Idempotent: a song already tracked, or one that turns
// out to already have scheduling history by the time this is called, is a
// no-op either way (the caller only calls this for a song it already
// confirmed has no history - see buildSetBuilderData). Once tracked, a
// song stays tracked permanently; there's no untrack.
func (s *Store) TrackNewSong(ctx context.Context, organizationID, songID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tracked_new_songs (organization_id, song_id)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, song_id) DO NOTHING
	`, organizationID, songID)
	return err
}

// TrackedNewSongs returns the set of song ids this org has ever tracked as
// new (see TrackNewSong) - used alongside PublishedSongAppearanceCounts to
// decide which candidates are reinforcement-eligible: tracked, and a local
// count of 1 or 2.
func (s *Store) TrackedNewSongs(ctx context.Context, organizationID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT song_id FROM tracked_new_songs WHERE organization_id = $1
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tracked := map[string]bool{}
	for rows.Next() {
		var songID string
		if err := rows.Scan(&songID); err != nil {
			return nil, err
		}
		tracked[songID] = true
	}

	return tracked, rows.Err()
}

// PublishedSongAppearanceCounts returns, for every song this org has ever
// confirmed-published through this app, how many distinct plans it's
// appeared in - a local, free (no Planning Center calls) stand-in for "how
// many times has this song actually been sung," built automatically as a
// side effect of normal use. Counts distinct plan ids, not raw set_songs
// rows, so a song duplicated within one plan (see DuplicateSongInSet - a
// song reprised as the response, say) still counts as one appearance, not
// two.
//
// Deliberately not filtered by pending_delete: a pending-delete row still
// had a real pco_item_id at some point, meaning it really was sung -
// excluding it would undercount. Known, accepted gap: once a removal is
// fully published, DeleteSetSongRow deletes the local row entirely, so a
// song sung once and later fully un-published through this app loses that
// count - narrow enough (removal-after-confirmed-publish is a correction,
// not the common case) not to be worth solving for now.
func (s *Store) PublishedSongAppearanceCounts(ctx context.Context, organizationID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ss.song_id, COUNT(DISTINCT s.plan_id)
		FROM set_songs ss
		JOIN sets s ON s.id = ss.set_id
		WHERE s.organization_id = $1 AND ss.pco_item_id != ''
		GROUP BY ss.song_id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var songID string
		var count int
		if err := rows.Scan(&songID, &count); err != nil {
			return nil, err
		}
		counts[songID] = count
	}

	return counts, rows.Err()
}
