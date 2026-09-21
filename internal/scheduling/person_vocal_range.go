package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PersonVocalRange is one person's comfortable singing range on record for
// an organization - see migration 00036. LowestPitchClass/HighestPitchClass
// are 0-11 (C=0 .. B=11, matching keyPitchClasses in
// internal/server/controllers/sets_builder.go), and
// LowestOctave/HighestOctave are the octave each note sits in - together
// they combine into an absolute semitone value (octave*12 + pitch_class)
// for comparison, computed by callers rather than stored redundantly here.
// One row per person per organization (see SetPersonVocalRange).
type PersonVocalRange struct {
	ID                int64     `json:"id"`
	OrganizationID    string    `json:"organization_id"`
	PersonID          string    `json:"person_id"`
	LowestPitchClass  int       `json:"lowest_pitch_class"`
	LowestOctave      int       `json:"lowest_octave"`
	HighestPitchClass int       `json:"highest_pitch_class"`
	HighestOctave     int       `json:"highest_octave"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SetPersonVocalRangeParams is what a caller (the vocal-range setup page's
// POST handler) has already parsed a submitted lowest/highest note string
// into - see the note-parsing helper in
// internal/server/controllers/vocal_range.go. No OrganizationID or PersonID
// validation lives here beyond "not empty" - the caller owns note parsing
// and range-order validation (lowest <= highest), since those are
// user-input concerns, not storage concerns.
type SetPersonVocalRangeParams struct {
	PersonID          string
	LowestPitchClass  int
	LowestOctave      int
	HighestPitchClass int
	HighestOctave     int
}

// SetPersonVocalRange creates or replaces personID's vocal range within
// organizationID - there's only ever one current range per person per
// organization (mirrors SetSongPersonPreference's upsert shape).
func (s *Store) SetPersonVocalRange(ctx context.Context, organizationID string, params SetPersonVocalRangeParams) (PersonVocalRange, error) {
	if params.PersonID == "" {
		return PersonVocalRange{}, errors.New("person_id is required")
	}

	var r PersonVocalRange
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO person_vocal_ranges (organization_id, person_id, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (organization_id, person_id) DO UPDATE
			SET lowest_pitch_class = EXCLUDED.lowest_pitch_class,
				lowest_octave = EXCLUDED.lowest_octave,
				highest_pitch_class = EXCLUDED.highest_pitch_class,
				highest_octave = EXCLUDED.highest_octave,
				updated_at = now()
		RETURNING id, organization_id, person_id, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
	`, organizationID, params.PersonID, params.LowestPitchClass, params.LowestOctave, params.HighestPitchClass, params.HighestOctave).Scan(
		&r.ID, &r.OrganizationID, &r.PersonID, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return PersonVocalRange{}, err
	}

	return r, nil
}

// PersonVocalRange looks up personID's single stored range within
// organizationID - sql.ErrNoRows means no range is on file for that person.
func (s *Store) PersonVocalRange(ctx context.Context, organizationID, personID string) (PersonVocalRange, error) {
	var r PersonVocalRange
	err := s.db.QueryRowContext(ctx, `
		SELECT id, organization_id, person_id, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
		FROM person_vocal_ranges
		WHERE organization_id = $1 AND person_id = $2
	`, organizationID, personID).Scan(
		&r.ID, &r.OrganizationID, &r.PersonID, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return PersonVocalRange{}, err
	}

	return r, nil
}

// HasAnyPersonVocalRanges reports whether the organization has recorded at
// least one person's vocal range - a cheap existence check for the home
// page's setup checklist (see HomeController.buildHomeData and
// docs/plans/shipped/onboarding-and-ambient-framing-plan.md), same
// shape as HasAnySongRelationships.
func (s *Store) HasAnyPersonVocalRanges(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM person_vocal_ranges WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

// ListPersonVocalRanges returns every stored range for organizationID,
// keyed by person id - a single bulk read for a whole page render rather
// than one query per person, mirroring SongArrangementInfo's bulk-map
// shape.
func (s *Store) ListPersonVocalRanges(ctx context.Context, organizationID string) (map[string]PersonVocalRange, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, person_id, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave, created_at, updated_at
		FROM person_vocal_ranges
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ranges := map[string]PersonVocalRange{}
	for rows.Next() {
		var r PersonVocalRange
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.PersonID, &r.LowestPitchClass, &r.LowestOctave, &r.HighestPitchClass, &r.HighestOctave, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		ranges[r.PersonID] = r
	}

	return ranges, rows.Err()
}

// DistinctPersonVocalRangePersonIDs returns every distinct person id with a
// stored range in organizationID's person_vocal_ranges - the vocal-range
// half of the People library index's local-data-derived person set (see
// DistinctPersonRelationshipPersonIDs's doc comment,
// internal/scheduling/person_relationships.go, for the full picture across
// all three source tables).
func (s *Store) DistinctPersonVocalRangePersonIDs(ctx context.Context, organizationID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT person_id FROM person_vocal_ranges WHERE organization_id = $1
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// DeletePersonVocalRange deletes the range row identified by id within
// organizationID. Returns sql.ErrNoRows if no such row exists (already
// deleted, or belongs to a different organization).
func (s *Store) DeletePersonVocalRange(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM person_vocal_ranges WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
