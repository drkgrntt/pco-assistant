package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// RoleOpener/RoleResponse are the role codes the set builder (see sets.go's
// controller) filters candidate songs by. They're seeded into
// song_role_types by migration 00015, but that table is the source of
// truth for which codes actually exist - an admin can add more straight
// into the DB and they'll show up as a selectable, displayable tag with no
// special set-builder filtering, same as any code these constants don't
// name.
const (
	RoleOpener   = "opener"
	RoleResponse = "response"
)

type SongRoleType struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// ListSongRoleTypes returns every selectable role, ordered for display in
// the picker - add a row to song_role_types to make a new one available,
// no code change needed.
func (s *Store) ListSongRoleTypes(ctx context.Context) ([]SongRoleType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, label, sort_order
		FROM song_role_types
		ORDER BY sort_order, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []SongRoleType{}
	for rows.Next() {
		var t SongRoleType
		if err := rows.Scan(&t.Code, &t.Label, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
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

	var role SongRole
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_roles (song_id, role)
		VALUES ($1, $2)
		ON CONFLICT (song_id, role) DO UPDATE SET role = EXCLUDED.role
		RETURNING id, song_id, role, created_at
	`, params.SongID, params.Role).Scan(&role.ID, &role.SongID, &role.Role, &role.CreatedAt)
	// role references song_role_types (see migration 00015) - a code that
	// isn't a row there fails as a foreign key violation.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongRole{}, fmt.Errorf("unknown role %q", params.Role)
	}
	if err != nil {
		return SongRole{}, err
	}

	return role, nil
}

// UpdateSongRole overwrites an existing tag's song/role in place, keyed by
// id - CreateSongRole's ON CONFLICT DO UPDATE upserts by (song_id, role),
// which only coincidentally acts like an edit when the song doesn't
// change; moving a tag to a different song needs an id-keyed update so it
// doesn't leave the original row behind.
func (s *Store) UpdateSongRole(ctx context.Context, id int64, params CreateSongRoleParams) (SongRole, error) {
	if params.SongID == "" {
		return SongRole{}, errors.New("song_id is required")
	}

	var role SongRole
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_roles
		SET song_id = $1, role = $2
		WHERE id = $3
		RETURNING id, song_id, role, created_at
	`, params.SongID, params.Role, id).Scan(&role.ID, &role.SongID, &role.Role, &role.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SongRole{}, fmt.Errorf("that tag no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongRole{}, fmt.Errorf("that song is already tagged with that role")
	}
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongRole{}, fmt.Errorf("unknown role %q", params.Role)
	}
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
