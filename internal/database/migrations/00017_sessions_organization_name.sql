-- +goose Up
-- The signed-in person's PCO organization (church) name, fetched once at
-- sign-in (see auth.Callback) and shown next to their name in the header -
-- see layout.templ. Stored on the session rather than re-fetched from PCO
-- on every request, same tradeoff as person_name below it: one extra API
-- call at login instead of one on every single page load.
ALTER TABLE sessions ADD COLUMN organization_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN organization_name;
