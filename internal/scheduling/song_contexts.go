package scheduling

// Song contexts ("where this song gets sung" - big church, youth, other
// campus, or whatever an org actually calls its own contexts) - see
// docs/plans/shipped/song-context-tags-plan.md for the resolved design and
// migration 00045 for the schema. Two halves in this file: type management
// (song_context_types - this app's first per-org *editable* lookup table,
// unlike song_role_types' fixed, developer-curated vocabulary) and song
// assignments (song_contexts - a straight structural mirror of
// song_roles.go's Create/Update/List/Delete/HasAny shape, just keyed by an
// int64 context_type_id instead of a string role code).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// SongContextType is one org-defined context ("Big church," "Youth," "Other
// campus," ...) - see ListSongContextTypes.
type SongContextType struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Label          string    `json:"label"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListSongContextTypes returns organizationID's context types, ordered for
// display in the picker - append-order (sort_order), then label as a
// tiebreaker. Org-scoped, unlike ListSongRoleTypes: this is a per-org
// editable list, not shared vocabulary.
func (s *Store) ListSongContextTypes(ctx context.Context, organizationID string) ([]SongContextType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, label, sort_order, created_at
		FROM song_context_types
		WHERE organization_id = $1
		ORDER BY sort_order, label
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []SongContextType{}
	for rows.Next() {
		var t SongContextType
		if err := rows.Scan(&t.ID, &t.OrganizationID, &t.Label, &t.SortOrder, &t.CreatedAt); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

// CreateSongContextType adds a new context type to organizationID's own
// list, trimming label and appending it after whatever sort_order already
// exists (COALESCE(MAX(sort_order),0)+1) - there's no manual reorder UI in
// v1, so append-order is the only order there is. A duplicate label (per
// song_context_types_unique) is reported as a friendly error rather than
// the raw constraint-violation message.
func (s *Store) CreateSongContextType(ctx context.Context, organizationID, label string) (SongContextType, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return SongContextType{}, errors.New("label is required")
	}

	var t SongContextType
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_context_types (organization_id, label, sort_order)
		VALUES ($1, $2, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM song_context_types WHERE organization_id = $1))
		RETURNING id, organization_id, label, sort_order, created_at
	`, organizationID, label).Scan(&t.ID, &t.OrganizationID, &t.Label, &t.SortOrder, &t.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongContextType{}, fmt.Errorf("you already have a context named %q", label)
	}
	if err != nil {
		return SongContextType{}, err
	}

	return t, nil
}

// UpdateSongContextType renames an existing context type in place - a
// rename only, no sort_order change (there's no manual reorder UI in v1).
func (s *Store) UpdateSongContextType(ctx context.Context, organizationID string, id int64, label string) (SongContextType, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return SongContextType{}, errors.New("label is required")
	}

	var t SongContextType
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_context_types
		SET label = $1
		WHERE id = $2 AND organization_id = $3
		RETURNING id, organization_id, label, sort_order, created_at
	`, label, id, organizationID).Scan(&t.ID, &t.OrganizationID, &t.Label, &t.SortOrder, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SongContextType{}, fmt.Errorf("that context no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongContextType{}, fmt.Errorf("you already have a context named %q", label)
	}
	if err != nil {
		return SongContextType{}, err
	}

	return t, nil
}

// DeleteSongContextType removes a context type - song_contexts' ON DELETE
// CASCADE (migration 00045) takes every song's assignment to it along with
// it, so the caller's own confirm wording needs to say so explicitly (see
// the song_contexts.templ delete button).
func (s *Store) DeleteSongContextType(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_context_types WHERE id = $1 AND organization_id = $2`, id, organizationID)
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

// SongContext is one song's assignment to a context type - see
// CreateSongContext.
type SongContext struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	SongID         string    `json:"song_id"`
	ContextTypeID  int64     `json:"context_type_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// See CreateSongRelationshipParams for why there's no OrganizationID field
// here.
type CreateSongContextParams struct {
	SongID        string `json:"song_id" form:"song_id"`
	ContextTypeID int64  `json:"context_type_id" form:"context_type_id"`
}

// CreateSongContext tags a song with a context type - straight mirror of
// CreateSongRole, just upserting on (organization_id, song_id,
// context_type_id) instead of (organization_id, song_id, role).
func (s *Store) CreateSongContext(ctx context.Context, organizationID string, params CreateSongContextParams) (SongContext, error) {
	if params.SongID == "" {
		return SongContext{}, errors.New("song_id is required")
	}

	var sc SongContext
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_contexts (organization_id, song_id, context_type_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, song_id, context_type_id) DO UPDATE SET context_type_id = EXCLUDED.context_type_id
		RETURNING id, organization_id, song_id, context_type_id, created_at
	`, organizationID, params.SongID, params.ContextTypeID).Scan(&sc.ID, &sc.OrganizationID, &sc.SongID, &sc.ContextTypeID, &sc.CreatedAt)
	// context_type_id references song_context_types (migration 00045) - an
	// id that isn't a row there (or belongs to a different org) fails as a
	// foreign key violation.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongContext{}, errors.New("unknown context")
	}
	if err != nil {
		return SongContext{}, err
	}

	return sc, nil
}

// UpdateSongContext overwrites an existing assignment's song/context in
// place, keyed by id and organizationID - see UpdateSongRole's doc comment
// for why (CreateSongContext's ON CONFLICT DO UPDATE only coincidentally
// acts like an edit when the song doesn't change).
func (s *Store) UpdateSongContext(ctx context.Context, organizationID string, id int64, params CreateSongContextParams) (SongContext, error) {
	if params.SongID == "" {
		return SongContext{}, errors.New("song_id is required")
	}

	var sc SongContext
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_contexts
		SET song_id = $1, context_type_id = $2
		WHERE id = $3 AND organization_id = $4
		RETURNING id, organization_id, song_id, context_type_id, created_at
	`, params.SongID, params.ContextTypeID, id, organizationID).Scan(&sc.ID, &sc.OrganizationID, &sc.SongID, &sc.ContextTypeID, &sc.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SongContext{}, fmt.Errorf("that context tag no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongContext{}, fmt.Errorf("that song is already tagged with that context")
	}
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongContext{}, errors.New("unknown context")
	}
	if err != nil {
		return SongContext{}, err
	}

	return sc, nil
}

// HasAnySongContexts reports whether the organization has tagged at least
// one song with a context - the home page setup checklist's existence
// check (see HasAnySongRoles), "tagged at least one song," not "created the
// lookup."
func (s *Store) HasAnySongContexts(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM song_contexts WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

// ListSongContext filters to organizationID, and by songID; pass "" to
// leave it unfiltered - see ListSongRoles' own doc comment (this is that
// function's exact shape, minus the second role-code filter, since nothing
// yet needs "browse songs tagged with this context" the way role filtering
// does at the store layer - that filtering happens candidate-list-side
// instead, see sets_builder.go's candidateMatchesContext).
func (s *Store) ListSongContext(ctx context.Context, organizationID, songID string) ([]SongContext, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, context_type_id, created_at
		FROM song_contexts
		WHERE organization_id = $1 AND ($2 = '' OR song_id = $2)
		ORDER BY created_at DESC
	`, organizationID, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contexts := []SongContext{}
	for rows.Next() {
		var sc SongContext
		if err := rows.Scan(&sc.ID, &sc.OrganizationID, &sc.SongID, &sc.ContextTypeID, &sc.CreatedAt); err != nil {
			return nil, err
		}
		contexts = append(contexts, sc)
	}

	return contexts, rows.Err()
}

func (s *Store) DeleteSongContext(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_contexts WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
