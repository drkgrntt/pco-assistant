package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// These are the relationship_type codes the candidate-suggestion algorithm
// (see sets.go) gives special meaning to - flows_well/same_theme boost a
// song as a suggested next pick, avoid_pairing flags a conflict. They're
// seeded into song_relationship_types by migration 00015, but that table is
// the source of truth for which codes actually exist: an admin can add
// more (e.g. "same_key") straight into the DB and they'll show up as a
// selectable, displayable relationship type with no special algorithmic
// behavior, same as any code the algorithm doesn't recognize.
const (
	RelationshipFlowsWell    = "flows_well"
	RelationshipSameTheme    = "same_theme"
	RelationshipAvoidPairing = "avoid_pairing"
)

type SongRelationshipType struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// ListSongRelationshipTypes returns every selectable relationship type,
// ordered for display in the picker - add a row to song_relationship_types
// to make a new one available, no code change needed. Not organization-
// scoped: these are shared vocabulary (codes/labels), not any one org's
// data - see migration 00015.
func (s *Store) ListSongRelationshipTypes(ctx context.Context) ([]SongRelationshipType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, label, sort_order
		FROM song_relationship_types
		ORDER BY sort_order, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []SongRelationshipType{}
	for rows.Next() {
		var t SongRelationshipType
		if err := rows.Scan(&t.Code, &t.Label, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

type SongRelationship struct {
	ID               int64     `json:"id"`
	OrganizationID   string    `json:"organization_id"`
	SongIDA          string    `json:"song_id_a"`
	SongIDB          string    `json:"song_id_b"`
	RelationshipType string    `json:"relationship_type"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Struct tags cover both JSON bodies (SchedulingAPIController, /api/scheduling)
// and form-encoded bodies (SchedulingController, /scheduling) - Fiber's
// BodyParser keys off `json` for JSON requests and `form` for
// application/x-www-form-urlencoded/multipart ones. There's deliberately no
// OrganizationID field here - it must never come from request input (a
// caller could just claim any org), only from the signed-in session (see
// middleware.CurrentOrganizationID), so every method below takes it as its
// own explicit parameter instead.
type CreateSongRelationshipParams struct {
	SongIDA          string `json:"song_id_a" form:"song_id_a"`
	SongIDB          string `json:"song_id_b" form:"song_id_b"`
	RelationshipType string `json:"relationship_type" form:"relationship_type"`
	Notes            string `json:"notes" form:"notes"`
}

func (s *Store) CreateSongRelationship(ctx context.Context, organizationID string, params CreateSongRelationshipParams) (SongRelationship, error) {
	if params.SongIDA == "" || params.SongIDB == "" {
		return SongRelationship{}, errors.New("song_id_a and song_id_b are required")
	}
	if params.SongIDA == params.SongIDB {
		return SongRelationship{}, errors.New("song_id_a and song_id_b must be different songs")
	}

	// Store the pair in a canonical order (lexicographically smaller ID
	// first). A relationship has no inherent direction - "A flows well
	// with B" and "B flows well with A" mean the same thing - so without
	// this, entering the same pair in the opposite order silently creates
	// a second, redundant row instead of being recognized as the same
	// relationship.
	songIDA, songIDB := params.SongIDA, params.SongIDB
	if songIDA > songIDB {
		songIDA, songIDB = songIDB, songIDA
	}

	var rel SongRelationship
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_relationships (organization_id, song_id_a, song_id_b, relationship_type, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, song_id_a, song_id_b, relationship_type) DO NOTHING
		RETURNING id, organization_id, song_id_a, song_id_b, relationship_type, notes, created_at, updated_at
	`, organizationID, songIDA, songIDB, params.RelationshipType, params.Notes).Scan(
		&rel.ID, &rel.OrganizationID, &rel.SongIDA, &rel.SongIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SongRelationship{}, fmt.Errorf("that relationship already exists")
	}
	// relationship_type references song_relationship_types (see migration
	// 00015) - a code that isn't a row there fails as a foreign key
	// violation rather than a Go-side map lookup now.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}
	if err != nil {
		return SongRelationship{}, err
	}

	return rel, nil
}

// UpdateSongRelationship overwrites an existing relationship's fields in
// place - same validation and canonical-order handling as
// CreateSongRelationship, but keyed by id rather than by the song
// pair/type, so changing which songs or type a row represents (an actual
// edit, not "add a new pair") doesn't leave the original row behind as an
// orphaned duplicate. Scoped by organizationID as well as id, so a
// forged/guessed id belonging to a different organization's row is a no-op
// (sql.ErrNoRows), not an edit.
func (s *Store) UpdateSongRelationship(ctx context.Context, organizationID string, id int64, params CreateSongRelationshipParams) (SongRelationship, error) {
	if params.SongIDA == "" || params.SongIDB == "" {
		return SongRelationship{}, errors.New("song_id_a and song_id_b are required")
	}
	if params.SongIDA == params.SongIDB {
		return SongRelationship{}, errors.New("song_id_a and song_id_b must be different songs")
	}

	songIDA, songIDB := params.SongIDA, params.SongIDB
	if songIDA > songIDB {
		songIDA, songIDB = songIDB, songIDA
	}

	var rel SongRelationship
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_relationships
		SET song_id_a = $1, song_id_b = $2, relationship_type = $3, notes = $4, updated_at = now()
		WHERE id = $5 AND organization_id = $6
		RETURNING id, organization_id, song_id_a, song_id_b, relationship_type, notes, created_at, updated_at
	`, songIDA, songIDB, params.RelationshipType, params.Notes, id, organizationID).Scan(
		&rel.ID, &rel.OrganizationID, &rel.SongIDA, &rel.SongIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SongRelationship{}, fmt.Errorf("that relationship no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongRelationship{}, fmt.Errorf("that relationship already exists")
	}
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}
	if err != nil {
		return SongRelationship{}, err
	}

	return rel, nil
}

// ListSongRelationships returns organizationID's relationships involving
// songID on either side. Pass "" for songID to list every relationship
// belonging to that organization.
func (s *Store) ListSongRelationships(ctx context.Context, organizationID, songID string) ([]SongRelationship, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id_a, song_id_b, relationship_type, notes, created_at, updated_at
		FROM song_relationships
		WHERE organization_id = $1 AND ($2 = '' OR song_id_a = $2 OR song_id_b = $2)
		ORDER BY created_at DESC
	`, organizationID, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := []SongRelationship{}
	for rows.Next() {
		var rel SongRelationship
		if err := rows.Scan(&rel.ID, &rel.OrganizationID, &rel.SongIDA, &rel.SongIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, err
		}
		relationships = append(relationships, rel)
	}

	return relationships, rows.Err()
}

func (s *Store) DeleteSongRelationship(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_relationships WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
