package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Role types are free text (validated here, not via a DB CHECK), same
// reasoning as relationship types - see RelationshipFlowsWell etc.
const (
	RoleOpener   = "opener"
	RoleResponse = "response"
)

var validRoles = map[string]bool{
	RoleOpener:   true,
	RoleResponse: true,
}

type SongRole struct {
	ID        int64     `json:"id"`
	SongID    string    `json:"song_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateSongRoleParams struct {
	SongID string `json:"song_id" form:"song_id"`
	Role   string `json:"role" form:"role"`
}

func (s *Store) CreateSongRole(ctx context.Context, params CreateSongRoleParams) (SongRole, error) {
	if params.SongID == "" {
		return SongRole{}, errors.New("song_id is required")
	}
	if !validRoles[params.Role] {
		return SongRole{}, fmt.Errorf("unknown role %q", params.Role)
	}

	var role SongRole
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_roles (song_id, role)
		VALUES ($1, $2)
		ON CONFLICT (song_id, role) DO UPDATE SET role = EXCLUDED.role
		RETURNING id, song_id, role, created_at
	`, params.SongID, params.Role).Scan(&role.ID, &role.SongID, &role.Role, &role.CreatedAt)
	if err != nil {
		return SongRole{}, err
	}

	return role, nil
}

// ListSongRoles filters by songID and/or role; pass "" for either to leave
// it unfiltered. Used both to show a song's tags and to browse songs tagged
// with a given role (e.g. every "opener").
func (s *Store) ListSongRoles(ctx context.Context, songID, role string) ([]SongRole, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, song_id, role, created_at
		FROM song_roles
		WHERE ($1 = '' OR song_id = $1) AND ($2 = '' OR role = $2)
		ORDER BY created_at DESC
	`, songID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []SongRole{}
	for rows.Next() {
		var role SongRole
		if err := rows.Scan(&role.ID, &role.SongID, &role.Role, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}

	return roles, rows.Err()
}

func (s *Store) DeleteSongRole(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_roles WHERE id = $1`, id)
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
