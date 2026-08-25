package scheduling

import "context"

// SongArrangementInfo is one song's cached Planning Center arrangement
// summary - see song_arrangement_cache (migration 00023). Zero values are
// legitimate ("PCO has nothing set for this field"), not "not cached yet" -
// a row's mere presence in the table is what "cached" means.
type SongArrangementInfo struct {
	Key           string
	BPM           float64
	Meter         string
	LengthSeconds int
}

// UpsertSongArrangementInfo writes (or overwrites) songID's cached
// arrangement summary for this org. Always overwrites rather than only
// ever recording the first observation, so a manual re-sync (see the Song
// Info page) reflects whatever's actually in Planning Center now, not
// whatever it was the first time this song was seen.
func (s *Store) UpsertSongArrangementInfo(ctx context.Context, organizationID, songID string, info SongArrangementInfo) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO song_arrangement_cache (organization_id, song_id, chord_chart_key, bpm, meter, length_seconds, cached_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (organization_id, song_id) DO UPDATE
			SET chord_chart_key = EXCLUDED.chord_chart_key,
				bpm = EXCLUDED.bpm,
				meter = EXCLUDED.meter,
				length_seconds = EXCLUDED.length_seconds,
				cached_at = now()
	`, organizationID, songID, info.Key, info.BPM, info.Meter, info.LengthSeconds)
	return err
}

// SongArrangementInfo returns every cached arrangement summary for this
// org, keyed by song id - a single bulk read for a whole page render
// (buildSetBuilderData) rather than one query per song, mirroring
// PublishedSongAppearanceCounts' bulk-map shape.
func (s *Store) SongArrangementInfo(ctx context.Context, organizationID string) (map[string]SongArrangementInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT song_id, chord_chart_key, bpm, meter, length_seconds
		FROM song_arrangement_cache
		WHERE organization_id = $1
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cache := map[string]SongArrangementInfo{}
	for rows.Next() {
		var songID string
		var info SongArrangementInfo
		if err := rows.Scan(&songID, &info.Key, &info.BPM, &info.Meter, &info.LengthSeconds); err != nil {
			return nil, err
		}
		cache[songID] = info
	}

	return cache, rows.Err()
}
