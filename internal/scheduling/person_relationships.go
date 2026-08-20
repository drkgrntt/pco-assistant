package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Relationship types are free text (validated here, not via a DB CHECK) so
// new ones don't need a migration - same approach as song relationships.
const (
	PersonRelationshipWorksWell    = "works_well"
	PersonRelationshipMentorship   = "mentorship"
	PersonRelationshipAvoidPairing = "avoid_pairing"
)

var validPersonRelationshipTypes = map[string]bool{
	PersonRelationshipWorksWell:    true,
	PersonRelationshipMentorship:   true,
	PersonRelationshipAvoidPairing: true,
}

type PersonRelationship struct {
	ID               int64     `json:"id"`
	PersonIDA        string    `json:"person_id_a"`
	PersonIDB        string    `json:"person_id_b"`
	RelationshipType string    `json:"relationship_type"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// See CreateSongRelationshipParams for why both json and form tags are here.
type CreatePersonRelationshipParams struct {
	PersonIDA        string `json:"person_id_a" form:"person_id_a"`
	PersonIDB        string `json:"person_id_b" form:"person_id_b"`
	RelationshipType string `json:"relationship_type" form:"relationship_type"`
	Notes            string `json:"notes" form:"notes"`
}

// CreatePersonRelationship largely mirrors CreateSongRelationship, with one
// deliberate difference: works_well and avoid_pairing are symmetric ("A
// works well with B" means the same thing as "B works well with A"), so
// those get stored in canonical (person_id_a < person_id_b) order like song
// relationships, to dedup regardless of entry order. Mentorship is NOT
// symmetric - "A mentors B" is a different fact from "B mentors A" - so it's
// stored exactly as entered (person_id_a is always the mentor) and never
// reordered.
func (s *Store) CreatePersonRelationship(ctx context.Context, params CreatePersonRelationshipParams) (PersonRelationship, error) {
	if params.PersonIDA == "" || params.PersonIDB == "" {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b are required")
	}
	if params.PersonIDA == params.PersonIDB {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b must be different people")
	}
	if !validPersonRelationshipTypes[params.RelationshipType] {
		return PersonRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}

	personIDA, personIDB := params.PersonIDA, params.PersonIDB
	if params.RelationshipType != PersonRelationshipMentorship && personIDA > personIDB {
		personIDA, personIDB = personIDB, personIDA
	}

	var rel PersonRelationship
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO person_relationships (person_id_a, person_id_b, relationship_type, notes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (person_id_a, person_id_b, relationship_type) DO NOTHING
		RETURNING id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
	`, personIDA, personIDB, params.RelationshipType, params.Notes).Scan(
		&rel.ID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PersonRelationship{}, fmt.Errorf("that relationship already exists")
	}
	if err != nil {
		return PersonRelationship{}, err
	}

	return rel, nil
}

// UpdatePersonRelationship overwrites an existing relationship's fields in
// place, keyed by id - same validation and canonical/mentorship-direction
// handling as CreatePersonRelationship (see its doc comment), but an
// actual edit of that row rather than an insert-or-conflict on the natural
// key.
func (s *Store) UpdatePersonRelationship(ctx context.Context, id int64, params CreatePersonRelationshipParams) (PersonRelationship, error) {
	if params.PersonIDA == "" || params.PersonIDB == "" {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b are required")
	}
	if params.PersonIDA == params.PersonIDB {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b must be different people")
	}
	if !validPersonRelationshipTypes[params.RelationshipType] {
		return PersonRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}

	personIDA, personIDB := params.PersonIDA, params.PersonIDB
	if params.RelationshipType != PersonRelationshipMentorship && personIDA > personIDB {
		personIDA, personIDB = personIDB, personIDA
	}

	var rel PersonRelationship
	err := s.db.QueryRowContext(ctx, `
		UPDATE person_relationships
		SET person_id_a = $1, person_id_b = $2, relationship_type = $3, notes = $4, updated_at = now()
		WHERE id = $5
		RETURNING id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
	`, personIDA, personIDB, params.RelationshipType, params.Notes, id).Scan(
		&rel.ID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PersonRelationship{}, fmt.Errorf("that relationship no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return PersonRelationship{}, fmt.Errorf("that relationship already exists")
	}
	if err != nil {
		return PersonRelationship{}, err
	}

	return rel, nil
}

// ListPersonRelationships returns relationships involving personID on either
// side. Pass "" to list every relationship.
func (s *Store) ListPersonRelationships(ctx context.Context, personID string) ([]PersonRelationship, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
		FROM person_relationships
		WHERE $1 = '' OR person_id_a = $1 OR person_id_b = $1
		ORDER BY created_at DESC
	`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := []PersonRelationship{}
	for rows.Next() {
		var rel PersonRelationship
		if err := rows.Scan(&rel.ID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, err
		}
		relationships = append(relationships, rel)
	}

	return relationships, rows.Err()
}

func (s *Store) DeletePersonRelationship(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM person_relationships WHERE id = $1`, id)
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
