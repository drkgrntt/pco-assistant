-- +goose Up
-- The one local place account-level state (subscription/plan status)
-- lives - organization_id was already threaded through every scheduling
-- table (migration 00019) as the tenant boundary, but until now there
-- was no row anywhere to hang that state on. status is a free-form
-- string, not an enum type - 'active' is the only value anything sets
-- today; see UpsertOrganization/OrganizationStatus (internal/auth) and
-- RequireActiveOrganization (internal/server/middleware) for how it's
-- written and read.
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill from sessions - every org that's ever signed in already has
-- its id/name sitting there, no reason to make them sign in again just
-- to get a row.
INSERT INTO organizations (id, name)
SELECT DISTINCT organization_id, organization_name FROM sessions
WHERE organization_id != ''
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE organizations;
