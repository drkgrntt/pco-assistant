-- +goose Up
-- The one local place account-level state (subscription/plan status)
-- lives - organization_id was already threaded through every scheduling
-- table (migration 00019) as the tenant boundary, but until now there
-- was no row anywhere to hang that state on. status mirrors Stripe's own
-- subscription status strings once a subscription exists ('active',
-- 'past_due', 'canceled', ...) plus 'trialing' as the pre-Stripe
-- default - deliberately not our own enum, so it never drifts out of
-- sync with what Stripe actually reports. plan is 'standard' or
-- 'multi_campus' (see internal/billing), '' until a subscription
-- exists. See UpsertOrganization/GetOrganization/
-- UpdateOrganizationSubscription (internal/auth) and
-- RequireActiveOrganization (internal/server/middleware) for how this
-- is written and read.
CREATE TABLE organizations (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'trialing',
    plan                   TEXT NOT NULL DEFAULT '',
    stripe_customer_id     TEXT NOT NULL DEFAULT '',
    stripe_subscription_id TEXT NOT NULL DEFAULT '',
    -- 14-day trial length is a plain literal here, not read from
    -- anywhere configurable - trivially changed later, but nothing
    -- computes against a different value elsewhere so there's no
    -- constant to keep in sync yet.
    trial_ends_at          TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '14 days'),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill from sessions - every org that's ever signed in already has
-- its id/name sitting there, no reason to make them sign in again just
-- to get a row. Note this also means a pre-existing org gets a fresh
-- 14-day trial starting from whenever this migration runs, same as a
-- brand new one - there's no real subscription history to backfill
-- trial_ends_at from.
INSERT INTO organizations (id, name)
SELECT DISTINCT organization_id, organization_name FROM sessions
WHERE organization_id != ''
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE organizations;
