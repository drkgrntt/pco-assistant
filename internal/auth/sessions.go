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
	// OrganizationName is the signed-in person's PCO organization (church)
	// name - fetched once at sign-in (see auth.Callback) and shown next to
	// PersonName in the header (see layout.templ). Can be "" if that lookup
	// failed at sign-in; callers should treat that as "unknown," not retry
	// it themselves.
	OrganizationName string
	// OrganizationID is that same organization's PCO id - unlike
	// OrganizationName, this is load-bearing: every scheduling.Store call
	// this session's requests make is scoped to it (see
	// middleware.CurrentOrganizationID), so a second PCO organization
	// signed into this same instance never sees or touches this one's
	// data. Also captured once at sign-in; can be "" if that lookup
	// failed, in which case RequireAuth treats the session as unusable
	// rather than letting requests through unscoped - see its own comment.
	OrganizationID string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	// AccessToken/RefreshToken/TokenExpiresAt are this session's own PCO
	// OAuth token - unlike PersonID/PersonName (looked up once at sign-in
	// and never touched again), these get refreshed over the session's
	// life (see RequireAuth in middleware/auth.go) as the access token
	// nears its ~2 hour PCO-imposed expiry. Every PCO API call this
	// session makes authenticates with AccessToken (go-pco.WithAccessToken)
	// - there's no other credential backing it.
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt time.Time
}

// CreateSession mints a new session and returns the raw token to set as the
// cookie value. Only its SHA-256 hash is stored, so reading the sessions
// table doesn't hand out anything replayable - that protection doesn't
// extend to accessToken/refreshToken, which have to be stored recoverable
// since outbound PCO calls need the literal value back (see the migration
// that added these columns for the tradeoff this implies).
func (s *Store) CreateSession(ctx context.Context, personID, personName, organizationID, organizationName, accessToken, refreshToken string, tokenExpiresAt time.Time) (token string, err error) {
	token, err = randomToken()
	if err != nil {
		return "", err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, person_id, person_name, organization_id, organization_name, expires_at, access_token, refresh_token, token_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, hashToken(token), personID, personName, organizationID, organizationName, time.Now().Add(SessionTTL), accessToken, refreshToken, tokenExpiresAt)
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
		SELECT person_id, person_name, organization_id, organization_name, created_at, expires_at, access_token, refresh_token, token_expires_at
		FROM sessions
		WHERE id = $1 AND expires_at > now()
	`, hashToken(token)).Scan(
		&session.PersonID, &session.PersonName, &session.OrganizationID, &session.OrganizationName, &session.CreatedAt, &session.ExpiresAt,
		&session.AccessToken, &session.RefreshToken, &session.TokenExpiresAt,
	)
	if err != nil {
		return Session{}, err
	}

	return session, nil
}

// MostRecentSession returns the most recently created still-valid session,
// if any - cmd/dbseed uses this to borrow a real signed-in person's PCO
// access token so seeded data can reference real songs/people instead of
// synthetic placeholders. Deliberately read-only: unlike GetSession (looked
// up by the session's own raw token, for authenticating a request), this
// never refreshes or writes anything back, since it doesn't have - and
// shouldn't need - the raw session token UpdateSessionTokens is keyed by
// (see hashToken), and rotating a stored refresh token from a background
// script risks invalidating the real browser session that's still using it.
func (s *Store) MostRecentSession(ctx context.Context) (Session, error) {
	var session Session
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id, person_name, organization_id, organization_name, created_at, expires_at, access_token, refresh_token, token_expires_at
		FROM sessions
		WHERE expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(
		&session.PersonID, &session.PersonName, &session.OrganizationID, &session.OrganizationName, &session.CreatedAt, &session.ExpiresAt,
		&session.AccessToken, &session.RefreshToken, &session.TokenExpiresAt,
	)
	if err != nil {
		return Session{}, err
	}

	return session, nil
}

// UpdateSessionTokens overwrites a session's PCO OAuth token after a
// refresh (see RequireAuth) - the session itself (its id, expiry, person)
// is unaffected, only which PCO credentials it authenticates API calls
// with going forward.
func (s *Store) UpdateSessionTokens(ctx context.Context, token, accessToken, refreshToken string, tokenExpiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET access_token = $2, refresh_token = $3, token_expires_at = $4
		WHERE id = $1
	`, hashToken(token), accessToken, refreshToken, tokenExpiresAt)
	return err
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
