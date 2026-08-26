package auth

import (
	"context"
	"time"
)

// OrgStatusActive is Stripe's own subscription status string for a
// current, paid-up subscription - organizations.status is set to
// whatever Stripe itself reports (see internal/billing), plus this
// package's own OrgStatusTrialing for the pre-Stripe default, so it
// never drifts out of sync with what Stripe actually thinks is true.
const OrgStatusActive = "active"

// OrgStatusTrialing is the default status a brand new organization gets
// (see the DEFAULT on organizations.status) - allowed access until
// TrialEndsAt passes, see RequireActiveOrganization
// (internal/server/middleware).
const OrgStatusTrialing = "trialing"

// Organization mirrors the organizations table - the one local place
// account-level state (subscription status, plan, Stripe ids) lives, see
// migration 00025.
type Organization struct {
	ID, Name, Status, Plan string
	StripeCustomerID       string
	StripeSubscriptionID   string
	TrialEndsAt            time.Time
	CreatedAt, UpdatedAt   time.Time
}

// UpsertOrganization records this org's current PCO name - called at
// sign-in (see Callback), same moment its id/name are already resolved
// for the session itself. Deliberately refreshes name only: status,
// plan, and every Stripe field are left untouched on conflict, so a
// routine sign-in never clobbers whatever the trial/billing state
// actually is. A brand new org gets OrgStatusTrialing and a fresh
// 14-day trial via the table's own DEFAULTs.
func (s *Store) UpsertOrganization(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO organizations (id, name)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE
			SET name = EXCLUDED.name, updated_at = now()
	`, id, name)
	return err
}

// GetOrganization returns this org's full billing/trial state - an
// error (no special "not found" case) if no row exists yet, e.g. a
// session pre-dating this table, or an upsert that failed at sign-in.
// Callers should treat that as "unknown," not "blocked" - see
// RequireActiveOrganization's fail-open handling.
func (s *Store) GetOrganization(ctx context.Context, id string) (Organization, error) {
	var org Organization
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, status, plan, stripe_customer_id, stripe_subscription_id, trial_ends_at, created_at, updated_at
		FROM organizations
		WHERE id = $1
	`, id).Scan(
		&org.ID, &org.Name, &org.Status, &org.Plan,
		&org.StripeCustomerID, &org.StripeSubscriptionID, &org.TrialEndsAt,
		&org.CreatedAt, &org.UpdatedAt,
	)
	if err != nil {
		return Organization{}, err
	}
	return org, nil
}

// UpdateOrganizationSubscription is the billing webhook's write path
// (see internal/billing) - sets status/plan/Stripe ids together after
// any subscription lifecycle event (checkout completed, subscription
// updated or canceled). Unlike UpsertOrganization, this is never called
// from the sign-in path and never touches name.
func (s *Store) UpdateOrganizationSubscription(ctx context.Context, id, stripeCustomerID, stripeSubscriptionID, status, plan string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE organizations
		SET stripe_customer_id = $2, stripe_subscription_id = $3, status = $4, plan = $5, updated_at = now()
		WHERE id = $1
	`, id, stripeCustomerID, stripeSubscriptionID, status, plan)
	return err
}
