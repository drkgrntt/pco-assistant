package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PreferencePreferred/PreferenceDiscouraged aren't given any special
// algorithmic meaning today (see ListSongPersonPreferences' callers - it's
// purely informational), but are kept as named constants since they're the
// two values every preference has always had. They're seeded into
// song_preference_types by migration 00015, which is the source of truth
// for which codes actually exist - add a row there for another option, no
// code change needed.
const (
	PreferencePreferred   = "preferred"
	PreferenceDiscouraged = "discouraged"
)

type SongPreferenceType struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// ListSongPreferenceTypes returns every selectable preference, ordered for
// display in the picker - add a row to song_preference_types to make a new
// one available, no code change needed. Not organization-scoped: shared
// vocabulary, not any one org's data.
func (s *Store) ListSongPreferenceTypes(ctx context.Context) ([]SongPreferenceType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, label, sort_order
		FROM song_preference_types
		ORDER BY sort_order, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []SongPreferenceType{}
	for rows.Next() {
		var t SongPreferenceType
		if err := rows.Scan(&t.Code, &t.Label, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

type SongPersonPreference struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	SongID         string    `json:"song_id"`
	PersonID       string    `json:"person_id"`
	Preference     string    `json:"preference"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// See CreateSongRelationshipParams for why both json and form tags are
// here, and why there's no OrganizationID field.
type SetSongPersonPreferenceParams struct {
	SongID     string `json:"song_id" form:"song_id"`
	PersonID   string `json:"person_id" form:"person_id"`
	Preference string `json:"preference" form:"preference"`
	Notes      string `json:"notes" form:"notes"`
}

// SetSongPersonPreference creates or replaces the preference for a
// song/person pair within organizationID - there's only ever one current
// preference per pair per organization.
func (s *Store) SetSongPersonPreference(ctx context.Context, organizationID string, params SetSongPersonPreferenceParams) (SongPersonPreference, error) {
	if params.SongID == "" || params.PersonID == "" {
		return SongPersonPreference{}, errors.New("song_id and person_id are required")
	}

	var pref SongPersonPreference
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_person_preferences (organization_id, song_id, person_id, preference, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, song_id, person_id) DO UPDATE
			SET preference = EXCLUDED.preference,
				notes = EXCLUDED.notes,
				updated_at = now()
		RETURNING id, organization_id, song_id, person_id, preference, notes, created_at, updated_at
	`, organizationID, params.SongID, params.PersonID, params.Preference, params.Notes).Scan(
		&pref.ID, &pref.OrganizationID, &pref.SongID, &pref.PersonID, &pref.Preference, &pref.Notes, &pref.CreatedAt, &pref.UpdatedAt,
	)
	// preference references song_preference_types (see migration 00015) - a
	// code that isn't a row there fails as a foreign key violation.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongPersonPreference{}, fmt.Errorf("unknown preference %q", params.Preference)
	}
	if err != nil {
		return SongPersonPreference{}, err
	}

	return pref, nil
}

// UpdateSongPersonPreference overwrites an existing preference's fields in
// place, keyed by id (and organizationID - see
// UpdateSongRelationship's doc comment for why) - unlike
// SetSongPersonPreference's upsert-by-pair (song_id, person_id), this is a
// true edit: changing which song or person a row is about updates that
// same row rather than upserting a different one and leaving the original
// behind.
func (s *Store) UpdateSongPersonPreference(ctx context.Context, organizationID string, id int64, params SetSongPersonPreferenceParams) (SongPersonPreference, error) {
	if params.SongID == "" || params.PersonID == "" {
		return SongPersonPreference{}, errors.New("song_id and person_id are required")
	}

	var pref SongPersonPreference
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_person_preferences
		SET song_id = $1, person_id = $2, preference = $3, notes = $4, updated_at = now()
		WHERE id = $5 AND organization_id = $6
		RETURNING id, organization_id, song_id, person_id, preference, notes, created_at, updated_at
	`, params.SongID, params.PersonID, params.Preference, params.Notes, id, organizationID).Scan(
		&pref.ID, &pref.OrganizationID, &pref.SongID, &pref.PersonID, &pref.Preference, &pref.Notes, &pref.CreatedAt, &pref.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SongPersonPreference{}, fmt.Errorf("that preference no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongPersonPreference{}, fmt.Errorf("a preference for that song and person already exists")
	}
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongPersonPreference{}, fmt.Errorf("unknown preference %q", params.Preference)
	}
	if err != nil {
		return SongPersonPreference{}, err
	}

	return pref, nil
}

// HasAnySongPersonPreferences reports whether the organization has set at
// least one song/person preference - a cheap existence check for the home
// page's setup checklist (see HomeController.buildHomeData and
// docs/plans/shipped/onboarding-and-ambient-framing-plan.md), same
// shape as HasAnySongRelationships.
func (s *Store) HasAnySongPersonPreferences(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM song_person_preferences WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

// ListSongPersonPreferences filters to organizationID, and by songID and/or
// personID; pass "" for either to leave it unfiltered.
func (s *Store) ListSongPersonPreferences(ctx context.Context, organizationID, songID, personID string) ([]SongPersonPreference, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, person_id, preference, notes, created_at, updated_at
		FROM song_person_preferences
		WHERE organization_id = $1 AND ($2 = '' OR song_id = $2) AND ($3 = '' OR person_id = $3)
		ORDER BY created_at DESC
	`, organizationID, songID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	preferences := []SongPersonPreference{}
	for rows.Next() {
		var pref SongPersonPreference
		if err := rows.Scan(&pref.ID, &pref.OrganizationID, &pref.SongID, &pref.PersonID, &pref.Preference, &pref.Notes, &pref.CreatedAt, &pref.UpdatedAt); err != nil {
			return nil, err
		}
		preferences = append(preferences, pref)
	}

	return preferences, rows.Err()
}

// DistinctSongPersonPreferencePersonIDs returns every distinct person id
// referenced by organizationID's song_person_preferences - the preference
// half of the People library index's local-data-derived person set (see
// DistinctPersonRelationshipPersonIDs's doc comment, person_relationships.go,
// for the full picture across all three source tables).
func (s *Store) DistinctSongPersonPreferencePersonIDs(ctx context.Context, organizationID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT person_id FROM song_person_preferences WHERE organization_id = $1
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

func (s *Store) DeleteSongPersonPreference(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_person_preferences WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
