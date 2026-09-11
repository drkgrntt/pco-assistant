package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SongVocalRange is one song's sung range on record for an organization -
// see migration 00041 (and 00043, which fixed this shape - see that
// migration's own comment) and docs/plans/shipped/vocal-range-plan.md's
// Phase 2 section. The low/high notes are stored as their own exact pitch
// class + octave, the same shape PersonVocalRange already uses, alongside
// ReferenceKey (the key it was measured against - a bare (low, high) note
// pair alone would be meaningless without knowing what key it was measured
// in). Storing the exact notes rather than an offset from a re-anchored
// tonic (migration 00041's original shape) means EffectiveVocalRange can
// reconstruct the range exactly as entered whenever the target key equals
// the reference key, not just an approximation anchored to a fixed octave -
// see that function's own doc comment. One row per song per organization
// (see SetSongVocalRange).
type SongVocalRange struct {
	ID                int64     `json:"id"`
	OrganizationID    string    `json:"organization_id"`
	SongID            string    `json:"song_id"`
	ReferenceKey      string    `json:"reference_key"`
	LowestPitchClass  int       `json:"lowest_pitch_class"`
	LowestOctave      int       `json:"lowest_octave"`
	HighestPitchClass int       `json:"highest_pitch_class"`
	HighestOctave     int       `json:"highest_octave"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SetSongVocalRangeParams is what a caller (the song-vocal-range setup
// page's POST handler) has already parsed the submitted reference key/low
// note/high note strings into (scheduling.ParseKeyPitchClass/ParseNote). No
// validation lives here beyond "not empty song id" - the caller owns
// note/key parsing, the same division of labor SetPersonVocalRangeParams
// uses.
type SetSongVocalRangeParams struct {
	SongID            string
	ReferenceKey      string
	LowestPitchClass  int
	LowestOctave      int
	HighestPitchClass int
	HighestOctave     int
}

// SetSongVocalRange creates or replaces songID's vocal range within
// organizationID - there's only ever one current range per song per
// organization (mirrors SetPersonVocalRange/SetSongPersonPreference's
// upsert shape).
func (s *Store) SetSongVocalRange(ctx context.Context, organizationID string, params SetSongVocalRangeParams) (SongVocalRange, error) {
	if params.SongID == "" {
		return SongVocalRange{}, errors.New("song_id is required")
	}

	var r SongVocalRange
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_vocal_ranges (organization_id, song_id, reference_key, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (organization_id, song_id) DO UPDATE
			SET reference_key = EXCLUDED.reference_key,
				lowest_pitch_class = EXCLUDED.lowest_pitch_class,
				lowest_octave = EXCLUDED.lowest_octave,
				highest_pitch_class = EXCLUDED.highest_pitch_class,
				highest_octave = EXCLUDED.highest_octave,
				updated_at = now()
		RETURNING id, organization_id, song_id, reference_key, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
	`, organizationID, params.SongID, params.ReferenceKey, params.LowestPitchClass, params.LowestOctave, params.HighestPitchClass, params.HighestOctave).Scan(
		&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return SongVocalRange{}, err
	}

	return r, nil
}

// SongVocalRange looks up songID's single stored range within
// organizationID - sql.ErrNoRows means no range is on file for that song.
func (s *Store) SongVocalRange(ctx context.Context, organizationID, songID string) (SongVocalRange, error) {
	var r SongVocalRange
	err := s.db.QueryRowContext(ctx, `
		SELECT id, organization_id, song_id, reference_key, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
		FROM song_vocal_ranges
		WHERE organization_id = $1 AND song_id = $2
	`, organizationID, songID).Scan(
		&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return SongVocalRange{}, err
	}

	return r, nil
}

// HasAnySongVocalRanges reports whether the organization has recorded at
// least one song's vocal range - a cheap existence check for the home
// page's setup checklist (see HomeController.buildHomeData and
// docs/plans/shipped/onboarding-and-ambient-framing-plan.md), same
// shape as HasAnySongRelationships.
func (s *Store) HasAnySongVocalRanges(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM song_vocal_ranges WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

// ListSongVocalRanges returns every stored range for organizationID, keyed
// by song id - a single bulk read serving both the setup page's table
// render and the Build page's per-song transposition lookup
// (buildSetBuilderData/renderServiceOrder, sets_builder.go), mirroring
// SongArrangementInfo's bulk-map shape.
func (s *Store) ListSongVocalRanges(ctx context.Context, organizationID string) (map[string]SongVocalRange, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, reference_key, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
		FROM song_vocal_ranges
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ranges := map[string]SongVocalRange{}
	for rows.Next() {
		var r SongVocalRange
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		ranges[r.SongID] = r
	}

	return ranges, rows.Err()
}

// DeleteSongVocalRange deletes the range row identified by id within
// organizationID. Returns sql.ErrNoRows if no such row exists (already
// deleted, or belongs to a different organization).
func (s *Store) DeleteSongVocalRange(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_vocal_ranges WHERE id = $1 AND organization_id = $2`, id, organizationID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// EffectiveVocalRange transposes a stored song vocal range - the exact low/
// high notes as entered against referenceKey (SongVocalRange's own
// LowestPitchClass/LowestOctave/HighestPitchClass/HighestOctave) - into
// targetKey, the arrangement's actual effective key right now, which may
// differ from whatever referenceKey happened to be when the range was
// entered. Returns ok=false if either key string fails to parse.
//
// Because the low/high notes are stored as their own exact absolute
// semitone (not an offset from a re-anchored tonic - see migration 00043,
// which fixed a real display bug in the original 00041 shape: an offset
// anchored at a FIXED octave on display couldn't reproduce the octave a
// note was actually entered in, so e.g. E3-E4 entered against reference key
// A came back displayed as E4-E5), this is an exact round trip whenever
// targetKey == referenceKey - the real register a director had in mind,
// not an approximation. Transposing into any other key shifts both notes
// by the same signed delta (SignedSemitoneDelta), preserving the interval
// width exactly either way.
func EffectiveVocalRange(referenceKey string, lowPitchClass, lowOctave, highPitchClass, highOctave int, targetKey string) (effectiveLow, effectiveHigh int, ok bool) {
	refPC, ok1 := ParseKeyPitchClass(referenceKey)
	targetPC, ok2 := ParseKeyPitchClass(targetKey)
	if !ok1 || !ok2 {
		return 0, 0, false
	}

	delta := SignedSemitoneDelta(refPC, targetPC)

	return AbsoluteSemitone(lowPitchClass, lowOctave) + delta, AbsoluteSemitone(highPitchClass, highOctave) + delta, true
}
