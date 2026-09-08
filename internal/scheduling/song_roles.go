package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// RoleOpener/RoleResponse are just the two role codes seeded by migration
// 00015 - song_role_types is the source of truth for which codes actually
// exist, and every one of them (these two, migration 00020's
// communion/prayer/reflection/baptism, or anything added later) gets
// identical treatment: selectable, displayable, and filterable in the set
// builder with no further code change. Neither of these constants gets any
// special algorithmic treatment in Go - see songRoleViews in the
// controller, which resolves any role generically - they're kept purely
// for readability, the same way song_tags.go's TagCategory* constants are.
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
// no code change needed. Not organization-scoped: shared vocabulary, not
// any one org's data.
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
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	SongID         string    `json:"song_id"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

// See CreateSongRelationshipParams for why there's no OrganizationID field
// here.
type CreateSongRoleParams struct {
	SongID string `json:"song_id" form:"song_id"`
	Role   string `json:"role" form:"role"`
}

func (s *Store) CreateSongRole(ctx context.Context, organizationID string, params CreateSongRoleParams) (SongRole, error) {
	if params.SongID == "" {
		return SongRole{}, errors.New("song_id is required")
	}

	var role SongRole
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_roles (organization_id, song_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, song_id, role) DO UPDATE SET role = EXCLUDED.role
		RETURNING id, organization_id, song_id, role, created_at
	`, organizationID, params.SongID, params.Role).Scan(&role.ID, &role.OrganizationID, &role.SongID, &role.Role, &role.CreatedAt)
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
// id and organizationID (see UpdateSongRelationship's doc comment for why
// both) - CreateSongRole's ON CONFLICT DO UPDATE upserts by (organization_id,
// song_id, role), which only coincidentally acts like an edit when the song
// doesn't change; moving a tag to a different song needs an id-keyed
// update so it doesn't leave the original row behind.
func (s *Store) UpdateSongRole(ctx context.Context, organizationID string, id int64, params CreateSongRoleParams) (SongRole, error) {
	if params.SongID == "" {
		return SongRole{}, errors.New("song_id is required")
	}

	var role SongRole
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_roles
		SET song_id = $1, role = $2
		WHERE id = $3 AND organization_id = $4
		RETURNING id, organization_id, song_id, role, created_at
	`, params.SongID, params.Role, id, organizationID).Scan(&role.ID, &role.OrganizationID, &role.SongID, &role.Role, &role.CreatedAt)
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

// HasAnySongRoles reports whether the organization has tagged at least one
// song with a role - a cheap existence check for the home page's setup
// checklist (see HomeController.buildHomeData and
// docs/plans/in-progress/onboarding-and-ambient-framing-plan.md), same
// shape as HasAnySongRelationships.
func (s *Store) HasAnySongRoles(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM song_roles WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

// ListSongRoles filters to organizationID, and by songID and/or role; pass
// "" for either to leave it unfiltered. Used both to show a song's tags and
// to browse songs tagged with a given role (e.g. every "opener").
func (s *Store) ListSongRoles(ctx context.Context, organizationID, songID, role string) ([]SongRole, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, role, created_at
		FROM song_roles
		WHERE organization_id = $1 AND ($2 = '' OR song_id = $2) AND ($3 = '' OR role = $3)
		ORDER BY created_at DESC
	`, organizationID, songID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []SongRole{}
	for rows.Next() {
		var role SongRole
		if err := rows.Scan(&role.ID, &role.OrganizationID, &role.SongID, &role.Role, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}

	return roles, rows.Err()
}

func (s *Store) DeleteSongRole(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_roles WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
