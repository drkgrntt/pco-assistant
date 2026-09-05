package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// SessionTTL is how long a session lasts before the cookie stops working and
// PCO sign-in is required again.
const SessionTTL = 30 * 24 * time.Hour

type Session struct {
	// ID is this session's hashed primary key (hashToken of the raw cookie
	// token) - only populated by SessionForBackgroundSync, which reads it
	// straight out of the sessions table. Every other lookup here
	// (GetSession, MostRecentSession) is keyed by the raw token itself and
	// has no need to hand this back out; those leave it "". Exists because
	// UpdateSessionTokens/DeleteSession are keyed by the *raw* token (they
	// hash it themselves) - a caller that found this session by querying
	// the table directly, not from a live request's own cookie, never has
	// that raw token to hash, so UpdateSessionTokensByID/DeleteSessionByID
	// (below) take this instead.
	ID         string
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

// SessionForBackgroundSync returns organizationID's most-recently-created
// still-valid session, if any - the token-source for the background-sync
// cron's per-org pass (see jobs.PCOJobs and
// docs/plans/shipped/pco-background-sync-plan.md's "Per-org pass" section). Unlike
// MostRecentSession (global, read-only, used by dev tooling),
// this is org-scoped and returns Session.ID so the caller can act on this
// session afterward (RefreshAccessToken, then UpdateSessionTokensByID or
// DeleteSessionByID) without ever having its raw cookie token.
//
// Which live session gets picked doesn't matter much when more than one
// exists for the org - every signed-in person is a full admin with
// equivalent PCO access (see SignOutPerson's doc comment, account.go) -
// but "most recently created" is a deliberate, documented choice rather
// than an arbitrary LIMIT 1: it's the same tiebreak MostRecentSession
// already uses, and skews toward whichever session is most likely to
// still be in active use (and therefore least likely to have a refresh
// token PCO has already stopped honoring - see this plan's own "Open
// questions" section on unverified idle-refresh-token lifetime).
//
// ok is false (with a nil error) when the org has no live session at all -
// not a failure, just nothing to borrow a token from this tick.
func (s *Store) SessionForBackgroundSync(ctx context.Context, organizationID string) (session Session, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT id, person_id, person_name, organization_id, organization_name, created_at, expires_at, access_token, refresh_token, token_expires_at
		FROM sessions
		WHERE organization_id = $1 AND expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1
	`, organizationID).Scan(
		&session.ID, &session.PersonID, &session.PersonName, &session.OrganizationID, &session.OrganizationName, &session.CreatedAt, &session.ExpiresAt,
		&session.AccessToken, &session.RefreshToken, &session.TokenExpiresAt,
	)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	return session, true, nil
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

// UpdateSessionTokensByID is UpdateSessionTokens' counterpart for a caller
// that already has the session's hashed id (Session.ID, from
// SessionForBackgroundSync) rather than its raw cookie token - the
// background-sync cron's own case, since it never has (and shouldn't need)
// a live browser session's actual cookie value. Skips the hashToken step
// UpdateSessionTokens does internally; everything else is identical.
func (s *Store) UpdateSessionTokensByID(ctx context.Context, id, accessToken, refreshToken string, tokenExpiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET access_token = $2, refresh_token = $3, token_expires_at = $4
		WHERE id = $1
	`, id, accessToken, refreshToken, tokenExpiresAt)
	return err
}

// DeleteSessionByID is DeleteSession's counterpart for a caller that
// already has the session's hashed id rather than its raw cookie token -
// see UpdateSessionTokensByID's identical reasoning. The background-sync
// cron uses this when a session's refresh token is no longer honored by
// PCO (see jobs.PCOJobs): a stronger, earlier "this session is actually
// useless" signal than deleteExpiredSessions' 30-day TTL sweep alone.
func (s *Store) DeleteSessionByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// ActiveSession is one row from ListActiveSessions - enough to show and
// act on a currently signed-in person from the Account page.
type ActiveSession struct {
	PersonID, PersonName string
	CreatedAt, ExpiresAt time.Time
}

// ListActiveSessions returns every live session for orgID, most recent
// first - one row per session, not deduped by person (a person signed in
// on two devices shows twice; auth.SeatCounts is the source of truth for
// the deduped billed count, this is just a visibility/action list).
func (s *Store) ListActiveSessions(ctx context.Context, orgID string) ([]ActiveSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT person_id, person_name, created_at, expires_at
		FROM sessions
		WHERE organization_id = $1 AND expires_at > now()
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []ActiveSession
	for rows.Next() {
		var sess ActiveSession
		if err := rows.Scan(&sess.PersonID, &sess.PersonName, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	return sessions, rows.Err()
}

// DeleteSessionsForPerson ends every live session a specific person has in
// this org - the "sign this person out" action from the Account page.
// Unlike DeleteSession (keyed by one raw token, for self-logout via
// /auth/logout), this is keyed by person id since whoever's performing
// this action doesn't have the target person's session token. Returns how
// many sessions were deleted (0 if the person had none - not an error).
func (s *Store) DeleteSessionsForPerson(ctx context.Context, orgID, personID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM sessions WHERE organization_id = $1 AND person_id = $2
	`, orgID, personID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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
