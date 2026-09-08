-- +goose Up
-- A durable record of every /feedback submission, alongside (not instead
-- of) the existing Mailgun email notification - see internal/feedback and
-- docs/issues.md: "add a feedback db table to the app so I can revisit
-- feedback at some point without having to dig through my email." Email
-- stays the real-time "notice this now" channel; this table is the
-- browsable archive.
--
-- Deliberately NOT organization-scoped the way every other table in this
-- app is (see CLAUDE.md's Organization scoping section) - feedback isn't
-- one org's own data to be filtered to that org's view, it's the app
-- operator's own record spanning every org. organization_id/person_id/
-- person_name are still recorded, purely for context on read (who said
-- this, from where), not as a query scope. There is deliberately no
-- web-facing read path yet (see internal/feedback's own doc comment) -
-- only Create is implemented; this table is queried directly for now.
CREATE TABLE feedback (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    organization_name TEXT NOT NULL,
    person_id TEXT NOT NULL,
    person_name TEXT NOT NULL,
    submitter_email TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE feedback;
