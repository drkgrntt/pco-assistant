package auth

import "context"

// OrgStatusActive is the only status value anything writes today -
// organizations.status exists so a future billing flow has somewhere to
// write "past_due"/"canceled"/etc without a schema change, see
// RequireActiveOrganization (internal/server/middleware).
const OrgStatusActive = "active"

// UpsertOrganization records this org's current PCO name - called at
// sign-in (see Callback), same moment its id/name are already resolved
// for the session itself. Deliberately refreshes name only: status is
// left untouched on conflict, so a routine sign-in never clobbers
// whatever a future billing flow set it to. A brand new org gets
// OrgStatusActive via the table's own DEFAULT.
func (s *Store) UpsertOrganization(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO organizations (id, name)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE
			SET name = EXCLUDED.name, updated_at = now()
	`, id, name)
	return err
}

// OrganizationStatus returns this org's current status - "" and an error
// if no row exists yet (a session pre-dating this table, or an upsert
// that failed at sign-in). Callers should treat that as "unknown," not
// "blocked" - see RequireActiveOrganization's fail-open handling.
func (s *Store) OrganizationStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = $1`, id).Scan(&status)
	if err != nil {
		return "", err
	}
	return status, nil
}
