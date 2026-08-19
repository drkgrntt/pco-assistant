package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Relationship types are free text (validated here, not via a DB CHECK) so
// new ones don't need a migration. These are just a starting set.
const (
	RelationshipFlowsWell    = "flows_well"
	RelationshipSameTheme    = "same_theme"
	RelationshipAvoidPairing = "avoid_pairing"
)

var validRelationshipTypes = map[string]bool{
	RelationshipFlowsWell:    true,
	RelationshipSameTheme:    true,
	RelationshipAvoidPairing: true,
}

type SongRelationship struct {
	ID               int64     `json:"id"`
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
// application/x-www-form-urlencoded/multipart ones.
type CreateSongRelationshipParams struct {
	SongIDA          string `json:"song_id_a" form:"song_id_a"`
	SongIDB          string `json:"song_id_b" form:"song_id_b"`
	RelationshipType string `json:"relationship_type" form:"relationship_type"`
	Notes            string `json:"notes" form:"notes"`
}

func (s *Store) CreateSongRelationship(ctx context.Context, params CreateSongRelationshipParams) (SongRelationship, error) {
	if params.SongIDA == "" || params.SongIDB == "" {
		return SongRelationship{}, errors.New("song_id_a and song_id_b are required")
	}
	if params.SongIDA == params.SongIDB {
		return SongRelationship{}, errors.New("song_id_a and song_id_b must be different songs")
	}
	if !validRelationshipTypes[params.RelationshipType] {
		return SongRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
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
		INSERT INTO song_relationships (song_id_a, song_id_b, relationship_type, notes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (song_id_a, song_id_b, relationship_type) DO NOTHING
		RETURNING id, song_id_a, song_id_b, relationship_type, notes, created_at, updated_at
	`, songIDA, songIDB, params.RelationshipType, params.Notes).Scan(
		&rel.ID, &rel.SongIDA, &rel.SongIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SongRelationship{}, fmt.Errorf("that relationship already exists")
	}
	if err != nil {
		return SongRelationship{}, err
	}

	return rel, nil
}

// ListSongRelationships returns relationships involving songID on either
// side. Pass "" to list every relationship.
func (s *Store) ListSongRelationships(ctx context.Context, songID string) ([]SongRelationship, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, song_id_a, song_id_b, relationship_type, notes, created_at, updated_at
		FROM song_relationships
		WHERE $1 = '' OR song_id_a = $1 OR song_id_b = $1
		ORDER BY created_at DESC
	`, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := []SongRelationship{}
	for rows.Next() {
		var rel SongRelationship
		if err := rows.Scan(&rel.ID, &rel.SongIDA, &rel.SongIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, err
		}
		relationships = append(relationships, rel)
	}

	return relationships, rows.Err()
}

func (s *Store) DeleteSongRelationship(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_relationships WHERE id = $1`, id)
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
