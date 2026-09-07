package scheduling

// Post-service debrief log / transition notes
// (docs/plans/shipped/transition-notes-plan.md) - persistent, cross-plan
// notes keyed to a song or a specific directional song-to-song transition
// (migration 00040), resurfaced automatically the next time that song or
// pairing comes up in a new draft set (see the controller's
// transitionNotePrompts). Distinct from song_relationships.go's
// SongRelationship: that table canonicalizes its pair (undirected - "A
// flows well with B" and "B flows well with A" mean the same thing), which
// is exactly wrong here, since direction is the entire content of a
// transition note. See that file's own doc comment for the fuller
// comparison.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TransitionNoteDebrief/TransitionNoteTalkingPoint are the only two kinds
// this table's kind column accepts (see migration 00040's CHECK
// constraint) - unlike song_relationship_types/song_tag_categories, this
// is deliberately not an admin-extensible lookup table: there will only
// ever be exactly these two kinds.
const (
	TransitionNoteDebrief      = "debrief"
	TransitionNoteTalkingPoint = "talking_point"
)

// TransitionNote is one row of song_transition_notes - see that migration
// for the schema this mirrors. SongIDB is "" for a single-song note (NULL
// in the database), matching this codebase's usual "empty string is the
// Go-level 'no value' sentinel for a nullable TEXT column" convention
// (e.g. SetSong.PCOItemID).
type TransitionNote struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Kind           string    `json:"kind"`
	SongIDA        string    `json:"song_id_a"`
	SongIDB        string    `json:"song_id_b"`
	Note           string    `json:"note"`
	CreatedAt      time.Time `json:"created_at"`
}

// See CreateSongRelationshipParams for why there's no OrganizationID field
// here.
type CreateTransitionNoteParams struct {
	Kind    string `json:"kind" form:"kind"`
	SongIDA string `json:"song_id_a" form:"song_id_a"`
	SongIDB string `json:"song_id_b" form:"song_id_b"`
	Note    string `json:"note" form:"note"`
}

// rowScanner is the *sql.Row/*sql.Rows subset scanTransitionNoteRow needs
// - both satisfy it, so the same scan code backs QueryRowContext's single
// row (LatestTransitionNote) and QueryContext's iteration (every List*
// method below) without duplicating the column list four times.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTransitionNoteRow scans one song_transition_notes row in the fixed
// column order every query in this file selects it in: id, organization_id,
// kind, song_id_a, song_id_b, note, created_at. song_id_b is nullable (see
// TransitionNote's own doc comment) - scanned via sql.NullString and
// folded to "" on the Go side either way.
func scanTransitionNoteRow(row rowScanner) (TransitionNote, error) {
	var n TransitionNote
	var songIDB sql.NullString
	if err := row.Scan(&n.ID, &n.OrganizationID, &n.Kind, &n.SongIDA, &songIDB, &n.Note, &n.CreatedAt); err != nil {
		return TransitionNote{}, err
	}
	n.SongIDB = songIDB.String
	return n, nil
}

// CreateTransitionNote inserts a new note - always a fresh row, never an
// upsert (see this package's own doc comment above on why the table is
// append-only). song_id_b is stored as real NULL (via NULLIF) when
// params.SongIDB is "", not the literal empty string - the resurfacing
// lookups below (ListTransitionNotesForKey/LatestTransitionNote) rely on
// genuine NULL to mean "single-song note."
func (s *Store) CreateTransitionNote(ctx context.Context, organizationID string, params CreateTransitionNoteParams) (TransitionNote, error) {
	if params.Kind != TransitionNoteDebrief && params.Kind != TransitionNoteTalkingPoint {
		return TransitionNote{}, fmt.Errorf("unknown kind %q", params.Kind)
	}
	if params.SongIDA == "" {
		return TransitionNote{}, errors.New("song_id_a is required")
	}
	if params.SongIDB != "" && params.SongIDA == params.SongIDB {
		return TransitionNote{}, errors.New("song_id_a and song_id_b must be different songs")
	}
	note := strings.TrimSpace(params.Note)
	if note == "" {
		return TransitionNote{}, errors.New("note is required")
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO song_transition_notes (organization_id, kind, song_id_a, song_id_b, note)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		RETURNING id, organization_id, kind, song_id_a, song_id_b, note, created_at
	`, organizationID, params.Kind, params.SongIDA, params.SongIDB, note)
	return scanTransitionNoteRow(row)
}

// ListTransitionNotesForKey returns every note (newest first) for one
// exact (songIDA, songIDB, kind) key - the debrief "log" view (see this
// package's own doc comment on why the table stays append-only rather
// than upserting in place). songIDB "" matches the single-song rows
// (stored as real NULL - see CreateTransitionNote); the comparison is
// null-safe (IS NOT DISTINCT FROM) since a plain "=" never matches NULL to
// NULL.
func (s *Store) ListTransitionNotesForKey(ctx context.Context, organizationID, songIDA, songIDB, kind string) ([]TransitionNote, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, kind, song_id_a, song_id_b, note, created_at
		FROM song_transition_notes
		WHERE organization_id = $1
			AND song_id_a = $2
			AND song_id_b IS NOT DISTINCT FROM NULLIF($3, '')
			AND kind = $4
		ORDER BY created_at DESC, id DESC
	`, organizationID, songIDA, songIDB, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []TransitionNote{}
	for rows.Next() {
		n, err := scanTransitionNoteRow(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// LatestTransitionNote returns the single most recent row for one exact
// key, ok=false when there isn't one yet - what the talking-points
// "current value" reading and the Run Sheet's inline display both
// actually read (see docs/plans/shipped/transition-notes-plan.md's
// Schema section on why talking-point rows are read this way despite the
// table being append-only).
func (s *Store) LatestTransitionNote(ctx context.Context, organizationID, songIDA, songIDB, kind string) (TransitionNote, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, organization_id, kind, song_id_a, song_id_b, note, created_at
		FROM song_transition_notes
		WHERE organization_id = $1
			AND song_id_a = $2
			AND song_id_b IS NOT DISTINCT FROM NULLIF($3, '')
			AND kind = $4
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, organizationID, songIDA, songIDB, kind)

	n, err := scanTransitionNoteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TransitionNote{}, false, nil
	}
	if err != nil {
		return TransitionNote{}, false, err
	}
	return n, true, nil
}

// ListTransitionNotesForSong returns every note (either kind, single-song
// or as either side of a pair - newest first) touching songID - the song
// detail card's own "notes on file" listing, mirroring
// ListSongRelationships' "either side" query shape (song_relationships.go)
// so a note entered against a pair is reachable from *both* songs' own
// cards, not just whichever one it was entered from - see
// docs/plans/shipped/transition-notes-plan.md's resolved "both cards"
// decision. Pass "" for songID to list every note belonging to this
// organization at once (same convention as ListSongRelationships) - used
// by the controller to build the whole org's resurfacing-prompt lookup in
// one query rather than one per song in the set.
func (s *Store) ListTransitionNotesForSong(ctx context.Context, organizationID, songID string) ([]TransitionNote, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, kind, song_id_a, song_id_b, note, created_at
		FROM song_transition_notes
		WHERE organization_id = $1 AND ($2 = '' OR song_id_a = $2 OR song_id_b = $2)
		ORDER BY created_at DESC, id DESC
	`, organizationID, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := []TransitionNote{}
	for rows.Next() {
		n, err := scanTransitionNoteRow(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// DeleteTransitionNote removes a note - a plain input-correction
// affordance (mirroring DeleteSongRelationship), not an edit: this table
// has no update method by design (see this package's own doc comment
// above).
func (s *Store) DeleteTransitionNote(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_transition_notes WHERE id = $1 AND organization_id = $2`, id, organizationID)
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
