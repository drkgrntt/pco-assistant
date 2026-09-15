-- +goose Up
-- Durable, org-scoped directory of every person who's signed in - unlike
-- sessions (deleted on logout, expires after SessionTTL), this survives
-- both, so the admin app (cmd/admin) has somewhere to read a name/email
-- from even for someone who isn't currently signed in. Upserted on every
-- sign-in (see auth.Callback), same pattern as organizations itself.
-- PCO person ids are only unique within one PCO organization, not
-- globally, hence the composite key rather than a bare person_id PK -
-- same reasoning as every other org-scoped table (see CLAUDE.md's
-- Organization scoping section).
CREATE TABLE people (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    email           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id)
);

-- +goose Down
DROP TABLE people;
