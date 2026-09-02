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

// DB returns the underlying connection pool this Store wraps - for callers
// outside this package (e.g. a DB-backed test in internal/server/
// controllers) that need to run their own ad-hoc query against the same
// database, such as backdating a cache row's synced_at to simulate
// staleness. Every other package should keep going through Store's own
// methods rather than this - it exists for tests, not everyday use.
func (s *Store) DB() *sql.DB {
	return s.db
}
