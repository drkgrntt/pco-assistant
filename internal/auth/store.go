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
	// tokenEncryptionKey is the raw (already base64-decoded) AES-256 key
	// used to encrypt/decrypt sessions.access_token/refresh_token - see
	// token_encryption.go for the full format and reasoning. nil/empty
	// means encryption is disabled (SESSION_TOKEN_ENCRYPTION_KEY unset),
	// in which case encryptToken/decryptToken pass values through
	// unchanged.
	tokenEncryptionKey []byte
}

// NewStore constructs a Store. tokenEncryptionKey should come from
// LoadTokenEncryptionKeyFromEnv (token_encryption.go) - pass nil to
// disable token encryption (as that helper does when
// SESSION_TOKEN_ENCRYPTION_KEY is unset).
func NewStore(db *sql.DB, tokenEncryptionKey []byte) *Store {
	return &Store{db: db, tokenEncryptionKey: tokenEncryptionKey}
}
