package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	PreferencePreferred   = "preferred"
	PreferenceDiscouraged = "discouraged"
)

var validPreferences = map[string]bool{
	PreferencePreferred:   true,
	PreferenceDiscouraged: true,
}

type SongPersonPreference struct {
	ID         int64     `json:"id"`
	SongID     string    `json:"song_id"`
	PersonID   string    `json:"person_id"`
	Preference string    `json:"preference"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// See CreateSongRelationshipParams for why both json and form tags are here.
type SetSongPersonPreferenceParams struct {
	SongID     string `json:"song_id" form:"song_id"`
	PersonID   string `json:"person_id" form:"person_id"`
	Preference string `json:"preference" form:"preference"`
	Notes      string `json:"notes" form:"notes"`
}

// SetSongPersonPreference creates or replaces the preference for a
// song/person pair - there's only ever one current preference per pair.
func (s *Store) SetSongPersonPreference(ctx context.Context, params SetSongPersonPreferenceParams) (SongPersonPreference, error) {
	if params.SongID == "" || params.PersonID == "" {
		return SongPersonPreference{}, errors.New("song_id and person_id are required")
	}
	if !validPreferences[params.Preference] {
		return SongPersonPreference{}, errors.New(`preference must be "preferred" or "discouraged"`)
	}

	var pref SongPersonPreference
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_person_preferences (song_id, person_id, preference, notes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (song_id, person_id) DO UPDATE
			SET preference = EXCLUDED.preference,
				notes = EXCLUDED.notes,
				updated_at = now()
		RETURNING id, song_id, person_id, preference, notes, created_at, updated_at
	`, params.SongID, params.PersonID, params.Preference, params.Notes).Scan(
		&pref.ID, &pref.SongID, &pref.PersonID, &pref.Preference, &pref.Notes, &pref.CreatedAt, &pref.UpdatedAt,
	)
	if err != nil {
		return SongPersonPreference{}, err
	}

	return pref, nil
}

// ListSongPersonPreferences filters by songID and/or personID; pass "" for
// either to leave it unfiltered.
func (s *Store) ListSongPersonPreferences(ctx context.Context, songID, personID string) ([]SongPersonPreference, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, song_id, person_id, preference, notes, created_at, updated_at
		FROM song_person_preferences
		WHERE ($1 = '' OR song_id = $1) AND ($2 = '' OR person_id = $2)
		ORDER BY created_at DESC
	`, songID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	preferences := []SongPersonPreference{}
	for rows.Next() {
		var pref SongPersonPreference
		if err := rows.Scan(&pref.ID, &pref.SongID, &pref.PersonID, &pref.Preference, &pref.Notes, &pref.CreatedAt, &pref.UpdatedAt); err != nil {
			return nil, err
		}
		preferences = append(preferences, pref)
	}

	return preferences, rows.Err()
}

func (s *Store) DeleteSongPersonPreference(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_person_preferences WHERE id = $1`, id)
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
