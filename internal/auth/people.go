package auth

import (
	"context"
	"time"
)

// Person mirrors the people table - a durable, org-scoped record of
// everyone who's ever signed in, independent of any one session's
// lifetime. See the people table's own migration for why this exists
// separately from sessions/organizations.
type Person struct {
	OrganizationID, PersonID string
	// OrganizationName is joined in from organizations at read time (see
	// ListPeople), not stored on people itself - organizations.name is
	// already the one place that's kept current (UpsertOrganization
	// refreshes it every sign-in), so reading it live avoids a second,
	// independently-staleable copy.
	OrganizationName     string
	Name, Email          string
	CreatedAt, UpdatedAt time.Time
}

// UpsertPerson records id/organizationID's current name/email - called at
// sign-in (see Callback), same moment UpsertOrganization runs. Refreshes
// name/email on every sign-in so this self-heals if either changes in
// PCO; updated_at doubles as a cheap "last seen" signal for other admin
// views (dormant-org lookups, etc.) without a separate column for it.
func (s *Store) UpsertPerson(ctx context.Context, organizationID, personID, name, email string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO people (organization_id, person_id, name, email)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id, person_id) DO UPDATE
			SET name = EXCLUDED.name, email = EXCLUDED.email, updated_at = now()
	`, organizationID, personID, name, email)
	return err
}

// ListPeople returns every person across every organization, newest
// last-seen first - the admin app's (cmd/admin) read path, same shape as
// feedback.Store.List: this app has no "app operator" access-control
// concept to safely gate a cross-org view with on the main multi-tenant
// app itself, so this exists purely for the separate, shared-secret-gated
// admin binary. See docs/plans/in-progress/admin-tooling-plan.md.
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	// LEFT JOIN, not INNER: UpsertOrganization/UpsertPerson are both
	// best-effort at sign-in (see Callback) - if the former ever failed
	// while the latter succeeded, an INNER JOIN would silently drop that
	// person from this list instead of just showing them with no
	// organization name.
	rows, err := s.db.QueryContext(ctx, `
		SELECT people.organization_id, people.person_id, COALESCE(organizations.name, ''), people.name, people.email, people.created_at, people.updated_at
		FROM people
		LEFT JOIN organizations ON organizations.id = people.organization_id
		ORDER BY people.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.OrganizationID, &p.PersonID, &p.OrganizationName, &p.Name, &p.Email, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
