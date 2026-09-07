package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SongVocalRange is one song's sung range on record for an organization -
// see migration 00041 and docs/plans/in-progress/vocal-range-plan.md's
// Phase 2 section. Stored as an interval (low/high offset, in semitones)
// above the tonic of ReferenceKey, the key it was measured against - a
// bare (low, high) note pair alone would be meaningless without knowing
// what key it was measured in. ReferenceKey is kept alongside the offsets
// purely so the entry can be re-derived/audited later - only the offsets
// are used at transposition time (see EffectiveVocalRange). One row per
// song per organization (see SetSongVocalRange).
type SongVocalRange struct {
	ID                  int64     `json:"id"`
	OrganizationID      string    `json:"organization_id"`
	SongID              string    `json:"song_id"`
	ReferenceKey        string    `json:"reference_key"`
	LowOffsetSemitones  int       `json:"low_offset_semitones"`
	HighOffsetSemitones int       `json:"high_offset_semitones"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// SetSongVocalRangeParams is what a caller (the song-vocal-range setup
// page's POST handler) has already computed the submitted reference
// key/low note/high note strings into via ComputeVocalRangeOffsets - see
// that function's own doc comment for the anchoring rule. No validation
// lives here beyond "not empty song id" - the caller owns note/key parsing
// and the anchoring computation, the same division of labor
// SetPersonVocalRangeParams uses.
type SetSongVocalRangeParams struct {
	SongID              string
	ReferenceKey        string
	LowOffsetSemitones  int
	HighOffsetSemitones int
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
		INSERT INTO song_vocal_ranges (organization_id, song_id, reference_key, low_offset_semitones, high_offset_semitones)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, song_id) DO UPDATE
			SET reference_key = EXCLUDED.reference_key,
				low_offset_semitones = EXCLUDED.low_offset_semitones,
				high_offset_semitones = EXCLUDED.high_offset_semitones,
				updated_at = now()
		RETURNING id, organization_id, song_id, reference_key, low_offset_semitones, high_offset_semitones, created_at, updated_at
	`, organizationID, params.SongID, params.ReferenceKey, params.LowOffsetSemitones, params.HighOffsetSemitones).Scan(
		&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowOffsetSemitones, &r.HighOffsetSemitones, &r.CreatedAt, &r.UpdatedAt,
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
		SELECT id, organization_id, song_id, reference_key, low_offset_semitones, high_offset_semitones, created_at, updated_at
		FROM song_vocal_ranges
		WHERE organization_id = $1 AND song_id = $2
	`, organizationID, songID).Scan(
		&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowOffsetSemitones, &r.HighOffsetSemitones, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return SongVocalRange{}, err
	}

	return r, nil
}

// ListSongVocalRanges returns every stored range for organizationID, keyed
// by song id - a single bulk read serving both the setup page's table
// render and the Build page's per-song transposition lookup
// (buildSetBuilderData/renderServiceOrder, sets_builder.go), mirroring
// SongArrangementInfo's bulk-map shape.
func (s *Store) ListSongVocalRanges(ctx context.Context, organizationID string) (map[string]SongVocalRange, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, reference_key, low_offset_semitones, high_offset_semitones, created_at, updated_at
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
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.SongID, &r.ReferenceKey, &r.LowOffsetSemitones, &r.HighOffsetSemitones, &r.CreatedAt, &r.UpdatedAt); err != nil {
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

// ComputeVocalRangeOffsets implements docs/plans/in-progress/
// vocal-range-plan.md's Phase 2 anchoring rule exactly: anchor
// referenceKey's tonic at the low note's own octave, stepping it down one
// octave if that natural placement sits above the low note (so the tonic
// always sits at or immediately below the low note), then express both
// notes as offsets above that tonic - lowOffset is guaranteed
// non-negative (and, by construction, less than an octave: the anchor is
// never more than 11 semitones below the low note), highOffset is
// guaranteed >= lowOffset. ok is false if referenceKey doesn't parse (see
// ParseKeyPitchClass) - the caller is expected to have already validated
// the note strings themselves via ParseNote before calling this.
func ComputeVocalRangeOffsets(referenceKey string, lowPitchClass, lowOctave, highPitchClass, highOctave int) (lowOffset, highOffset int, ok bool) {
	refPC, ok := ParseKeyPitchClass(referenceKey)
	if !ok {
		return 0, 0, false
	}

	lowAbs := AbsoluteSemitone(lowPitchClass, lowOctave)
	highAbs := AbsoluteSemitone(highPitchClass, highOctave)

	tonicAbs := AbsoluteSemitone(refPC, lowOctave)
	if tonicAbs > lowAbs {
		tonicAbs -= 12
	}

	return lowAbs - tonicAbs, highAbs - tonicAbs, true
}

// referenceTonicOctave is the fixed octave EffectiveVocalRange anchors a
// reference key's tonic to when reconstructing a real, displayable
// register at render time - see that function's own doc comment for why a
// fixed convention, not the original entry-time octave, is used here.
// Chosen to match Phase 1's own vocalNoteOctaves span
// (internal/views/scheduling/vocal_range.templ), a reasonable
// middle-of-typical-vocal-range placement.
const referenceTonicOctave = 3

// EffectiveVocalRange transposes a stored song vocal range (referenceKey +
// offsets, see ComputeVocalRangeOffsets) into targetKey - the arrangement's
// actual effective key right now, which may differ from whatever
// referenceKey happened to be when the range was entered. Returns
// ok=false if either key string fails to parse.
//
// Deliberate implementation note: the stored offsets alone don't retain
// the real octave the original low/high notes were entered in (see
// migration 00041 - only the reference key text and the two offsets
// survive), so this can't reconstruct the exact absolute register a
// director had in mind byte-for-byte. What it does preserve exactly is
// the relative structure that actually matters for a transposition - the
// interval width (high minus low) and the correct signed shift into
// targetKey (scheduling.SignedSemitoneDelta) - anchored at a fixed,
// reasonable octave (referenceTonicOctave) to produce a plausible,
// realistic-looking real-world display rather than attempting to invert
// data that was never persisted. Good enough for Phase 2's own scope (a
// visible, informational range on the song detail card); Phase 3's
// mismatch warning (not built here) is where exact register-matching
// against a person's stored range would need this revisited if the
// fixed-octave approximation proves too imprecise in practice.
func EffectiveVocalRange(referenceKey string, lowOffset, highOffset int, targetKey string) (effectiveLow, effectiveHigh int, ok bool) {
	refPC, ok1 := ParseKeyPitchClass(referenceKey)
	targetPC, ok2 := ParseKeyPitchClass(targetKey)
	if !ok1 || !ok2 {
		return 0, 0, false
	}

	tonicAbs := AbsoluteSemitone(refPC, referenceTonicOctave)
	delta := SignedSemitoneDelta(refPC, targetPC)

	return tonicAbs + delta + lowOffset, tonicAbs + delta + highOffset, true
}
