package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// These are the relationship_type codes the roster-suggestion algorithm
// (see rosters.go) gives special meaning to - works_well/mentorship boost a
// pairing, avoid_pairing flags a conflict. They're seeded into
// person_relationship_types by migration 00015, but that table is the
// source of truth for which codes actually exist and whether each is
// directional - see personRelationshipType, which CreatePersonRelationship
// and UpdatePersonRelationship query instead of comparing against
// PersonRelationshipMentorship by name. An admin can add more codes
// straight into the DB and they'll show up as a selectable, displayable
// relationship type with no special algorithmic behavior, same as any code
// the algorithm doesn't recognize.
const (
	PersonRelationshipWorksWell    = "works_well"
	PersonRelationshipMentorship   = "mentorship"
	PersonRelationshipAvoidPairing = "avoid_pairing"
)

type PersonRelationshipType struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Directional bool   `json:"directional"`
	SortOrder   int    `json:"sort_order"`
}

// ListPersonRelationshipTypes returns every selectable relationship type,
// ordered for display in the picker - add a row to
// person_relationship_types to make a new one available, no code change
// needed. Not organization-scoped: shared vocabulary, not any one org's
// data.
func (s *Store) ListPersonRelationshipTypes(ctx context.Context) ([]PersonRelationshipType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, label, directional, sort_order
		FROM person_relationship_types
		ORDER BY sort_order, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []PersonRelationshipType{}
	for rows.Next() {
		var t PersonRelationshipType
		if err := rows.Scan(&t.Code, &t.Label, &t.Directional, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

// personRelationshipTypeDirectional looks up whether relationshipType is
// directional (e.g. mentorship: "A mentors B" and "B mentors A" are
// different facts, so the pair is never reordered) or symmetric (e.g. works
// well with: reorder to a canonical person_id_a < person_id_b so the same
// pair entered either order dedupes to one row). sql.ErrNoRows means the
// code isn't in person_relationship_types at all - an unknown type.
func (s *Store) personRelationshipTypeDirectional(ctx context.Context, relationshipType string) (bool, error) {
	var directional bool
	err := s.db.QueryRowContext(ctx, `
		SELECT directional FROM person_relationship_types WHERE code = $1
	`, relationshipType).Scan(&directional)
	return directional, err
}

type PersonRelationship struct {
	ID               int64     `json:"id"`
	OrganizationID   string    `json:"organization_id"`
	PersonIDA        string    `json:"person_id_a"`
	PersonIDB        string    `json:"person_id_b"`
	RelationshipType string    `json:"relationship_type"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// See CreateSongRelationshipParams for why there's no OrganizationID field
// here.
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
func (s *Store) CreatePersonRelationship(ctx context.Context, organizationID string, params CreatePersonRelationshipParams) (PersonRelationship, error) {
	if params.PersonIDA == "" || params.PersonIDB == "" {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b are required")
	}
	if params.PersonIDA == params.PersonIDB {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b must be different people")
	}
	directional, err := s.personRelationshipTypeDirectional(ctx, params.RelationshipType)
	if errors.Is(err, sql.ErrNoRows) {
		return PersonRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}
	if err != nil {
		return PersonRelationship{}, err
	}

	personIDA, personIDB := params.PersonIDA, params.PersonIDB
	if !directional && personIDA > personIDB {
		personIDA, personIDB = personIDB, personIDA
	}

	var rel PersonRelationship
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO person_relationships (organization_id, person_id_a, person_id_b, relationship_type, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, person_id_a, person_id_b, relationship_type) DO NOTHING
		RETURNING id, organization_id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
	`, organizationID, personIDA, personIDB, params.RelationshipType, params.Notes).Scan(
		&rel.ID, &rel.OrganizationID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
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
// place, keyed by id and organizationID (see UpdateSongRelationship's doc
// comment for why both) - same validation and canonical/mentorship-
// direction handling as CreatePersonRelationship (see its doc comment), but
// an actual edit of that row rather than an insert-or-conflict on the
// natural key.
func (s *Store) UpdatePersonRelationship(ctx context.Context, organizationID string, id int64, params CreatePersonRelationshipParams) (PersonRelationship, error) {
	if params.PersonIDA == "" || params.PersonIDB == "" {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b are required")
	}
	if params.PersonIDA == params.PersonIDB {
		return PersonRelationship{}, errors.New("person_id_a and person_id_b must be different people")
	}
	directional, err := s.personRelationshipTypeDirectional(ctx, params.RelationshipType)
	if errors.Is(err, sql.ErrNoRows) {
		return PersonRelationship{}, fmt.Errorf("unknown relationship_type %q", params.RelationshipType)
	}
	if err != nil {
		return PersonRelationship{}, err
	}

	personIDA, personIDB := params.PersonIDA, params.PersonIDB
	if !directional && personIDA > personIDB {
		personIDA, personIDB = personIDB, personIDA
	}

	var rel PersonRelationship
	err = s.db.QueryRowContext(ctx, `
		UPDATE person_relationships
		SET person_id_a = $1, person_id_b = $2, relationship_type = $3, notes = $4, updated_at = now()
		WHERE id = $5 AND organization_id = $6
		RETURNING id, organization_id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
	`, personIDA, personIDB, params.RelationshipType, params.Notes, id, organizationID).Scan(
		&rel.ID, &rel.OrganizationID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt,
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

// ListPersonRelationships returns organizationID's relationships involving
// personID on either side. Pass "" for personID to list every relationship
// belonging to that organization.
// HasAnyPersonRelationships reports whether the organization has set up at
// least one person relationship of any kind - see
// HasAnySongRelationships's doc comment (same shape, same purpose: the
// home page's setup checklist).
func (s *Store) HasAnyPersonRelationships(ctx context.Context, organizationID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM person_relationships WHERE organization_id = $1)
	`, organizationID).Scan(&exists)
	return exists, err
}

func (s *Store) ListPersonRelationships(ctx context.Context, organizationID, personID string) ([]PersonRelationship, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, person_id_a, person_id_b, relationship_type, notes, created_at, updated_at
		FROM person_relationships
		WHERE organization_id = $1 AND ($2 = '' OR person_id_a = $2 OR person_id_b = $2)
		ORDER BY created_at DESC
	`, organizationID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := []PersonRelationship{}
	for rows.Next() {
		var rel PersonRelationship
		if err := rows.Scan(&rel.ID, &rel.OrganizationID, &rel.PersonIDA, &rel.PersonIDB, &rel.RelationshipType, &rel.Notes, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, err
		}
		relationships = append(relationships, rel)
	}

	return relationships, rows.Err()
}

// DistinctPersonRelationshipPersonIDs returns every distinct person id
// (either side of a relationship) referenced by organizationID's
// person_relationships - one of three cheap local-DB queries the People
// library index derives its person set from (see docs/plans/not-started/
// song-person-detail-pages-plan.md's "songs are a cached library, people
// are not" section, and DistinctSongPersonPreferencePersonIDs/
// DistinctPersonVocalRangePersonIDs, this method's counterparts on the
// other two tables that same index draws from). Same existence-check shape
// as HasAnyPersonRelationships, just returning the actual ids instead of a
// bool.
func (s *Store) DistinctPersonRelationshipPersonIDs(ctx context.Context, organizationID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT person_id FROM (
			SELECT person_id_a AS person_id FROM person_relationships WHERE organization_id = $1
			UNION
			SELECT person_id_b AS person_id FROM person_relationships WHERE organization_id = $1
		) both_sides
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

func (s *Store) DeletePersonRelationship(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM person_relationships WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
