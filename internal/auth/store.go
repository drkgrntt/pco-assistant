// Package auth handles "who is allowed to use this app, and as whom do
// their PCO API calls run": signing in via PCO OAuth, and the DB-backed
// sessions that follow. Each session keeps its own PCO access/refresh
// token (see Session, CreateSession) - every PCO API call this app makes
// for a signed-in user authenticates as that user (go-pco's
// WithAccessToken, wired up in middleware.RequireAuth), not a single
// shared Personal Access Token - required for a second real PCO
// account/organization to be able to use this app at all.
package auth

import "database/sql"

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
