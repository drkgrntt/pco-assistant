// Package scheduling holds domain concepts PCO has no notion of - relating
// songs to each other, noting which people are preferred/discouraged for a
// given song, and guardrails (like repetition warnings) built on top of
// live PCO data. Songs and people are referenced by their PCO IDs; there's
// no local copy of PCO's own records, so details (titles, names) are always
// resolved live via go-pco.
package scheduling

import "database/sql"

// Store provides access to scheduling data. All methods are safe for
// concurrent use, same as the *sql.DB they wrap.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
