// Package auth handles "who is allowed to use this app": signing in via PCO
// OAuth (used purely as an identity provider - the resulting PCO access
// token is used once, to look up who's signing in, then discarded) and the
// DB-backed sessions that follow. This is unrelated to the Personal Access
// Token the rest of the app uses for its own PCO API calls (see go-pco's
// NewRequest) - that stays exactly as it is.
package auth

import "database/sql"

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
