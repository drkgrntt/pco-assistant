package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// SessionTTL is how long a session lasts before the cookie stops working and
// PCO sign-in is required again.
const SessionTTL = 30 * 24 * time.Hour

type Session struct {
	PersonID   string
	PersonName string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// CreateSession mints a new session and returns the raw token to set as the
// cookie value. Only its SHA-256 hash is stored, so reading the sessions
// table doesn't hand out anything replayable.
func (s *Store) CreateSession(ctx context.Context, personID, personName string) (token string, err error) {
	token, err = randomToken()
	if err != nil {
		return "", err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, person_id, person_name, expires_at)
		VALUES ($1, $2, $3, $4)
	`, hashToken(token), personID, personName, time.Now().Add(SessionTTL))
	if err != nil {
		return "", err
	}

	return token, nil
}

// GetSession looks up a session by its raw (cookie) token. It returns
// sql.ErrNoRows if the token is unknown or has expired.
func (s *Store) GetSession(ctx context.Context, token string) (Session, error) {
	var session Session
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id, person_name, created_at, expires_at
		FROM sessions
		WHERE id = $1 AND expires_at > now()
	`, hashToken(token)).Scan(&session.PersonID, &session.PersonName, &session.CreatedAt, &session.ExpiresAt)
	if err != nil {
		return Session{}, err
	}

	return session, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, hashToken(token))
	return err
}

// DeleteExpiredSessions purges expired rows. Called periodically from
// jobs.AuthJobs so the table doesn't grow unbounded.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.New("generate session token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
