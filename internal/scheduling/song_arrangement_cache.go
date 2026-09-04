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
	// ArrangementID is the arrangement's own PCO id (migration 00026) -
	// unlike Key/BPM/Meter/LengthSeconds (display-only summaries), this is
	// an actual relationship target: publishing a song passes it as
	// pco.CreateItemParams.ArrangementID so the resulting plan item is
	// actually linked to the arrangement, not just the bare song.
	ArrangementID string
	// Notes is the arrangement's own persistent PCO notes text, carried
	// through as a plan item's one-time Description default at publish
	// time - see PublishSet.
	Notes string
	// KeyID is the song's real Key sub-resource id (migration 00027) -
	// a genuinely separate relationship from ArrangementID, not implied by
	// it (confirmed live: an item with only its arrangement linked still
	// shows no key at all). Empty when the song has no Key sub-resource
	// (Key, the field above, then falls back to the arrangement's own
	// ChordChartKey - see fetchAndCacheArrangementInfo).
	KeyID string
	// Structure is the arrangement's section-by-section structure
	// (migration 00034) - PCO's ArrangementAttributes.Sequence/
	// SequenceShort ("Intro, Verse 1, Chorus...") joined into one display
	// string by formatSongStructure (scheduling.go), preferring
	// SequenceShort's abbreviated form and falling back to Sequence.
	// Display-only, like Key/BPM/Meter/LengthSeconds above - not a
	// relationship target, nothing publishes it anywhere.
	Structure string
	// HasChordChart is the arrangement's own HasChordChart attribute
	// (migration 00034) - true when a chord chart document is actually
	// attached in Planning Center, shown as a badge on the song detail
	// card so it's known before rehearsal whether one's even there.
	HasChordChart bool
}

// UpsertSongArrangementInfo writes (or overwrites) songID's cached
// arrangement summary for this org. Always overwrites rather than only
// ever recording the first observation, so a manual re-sync (see the Song
// Info page) reflects whatever's actually in Planning Center now, not
// whatever it was the first time this song was seen.
func (s *Store) UpsertSongArrangementInfo(ctx context.Context, organizationID, songID string, info SongArrangementInfo) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO song_arrangement_cache (organization_id, song_id, chord_chart_key, bpm, meter, length_seconds, arrangement_id, notes, key_id, structure, has_chord_chart, cached_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (organization_id, song_id) DO UPDATE
			SET chord_chart_key = EXCLUDED.chord_chart_key,
				bpm = EXCLUDED.bpm,
				meter = EXCLUDED.meter,
				length_seconds = EXCLUDED.length_seconds,
				arrangement_id = EXCLUDED.arrangement_id,
				notes = EXCLUDED.notes,
				key_id = EXCLUDED.key_id,
				structure = EXCLUDED.structure,
				has_chord_chart = EXCLUDED.has_chord_chart,
				cached_at = now()
	`, organizationID, songID, info.Key, info.BPM, info.Meter, info.LengthSeconds, info.ArrangementID, info.Notes, info.KeyID, info.Structure, info.HasChordChart)
	return err
}

// SongArrangementInfo returns every cached arrangement summary for this
// org, keyed by song id - a single bulk read for a whole page render
// (buildSetBuilderData) rather than one query per song, mirroring
// PublishedSongAppearanceCounts' bulk-map shape.
func (s *Store) SongArrangementInfo(ctx context.Context, organizationID string) (map[string]SongArrangementInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT song_id, chord_chart_key, bpm, meter, length_seconds, arrangement_id, notes, key_id, structure, has_chord_chart
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
		if err := rows.Scan(&songID, &info.Key, &info.BPM, &info.Meter, &info.LengthSeconds, &info.ArrangementID, &info.Notes, &info.KeyID, &info.Structure, &info.HasChordChart); err != nil {
			return nil, err
		}
		cache[songID] = info
	}

	return cache, rows.Err()
}

// ClearSongArrangementCache deletes every cached song arrangement row for
// organizationID - see AccountController.ClearSongInfoCache (internal/
// server/controllers/account.go), the Account page's "nuclear" cache-clear
// action. Non-destructive to real PCO data: the next read
// (resolveArrangementInfo's cache-miss path, or a Song Info sync) simply
// re-fetches and re-caches from Planning Center as needed, same as a
// brand-new org that's never synced yet.
func (s *Store) ClearSongArrangementCache(ctx context.Context, organizationID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM song_arrangement_cache WHERE organization_id = $1`, organizationID)
	return err
}
