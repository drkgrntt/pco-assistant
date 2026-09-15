-- +goose Up
-- A running count of how many times this person has signed in - the
-- cheapest real usage-frequency signal available today (see
-- docs/plans/in-progress/admin-tooling-plan.md's "Painting an admin
-- picture" section for why sessions/sets/rosters don't already give us
-- one). Incremented in UpsertPerson alongside the existing name/email
-- refresh, starting at 1 on first insert.
ALTER TABLE people ADD COLUMN sign_in_count INT NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE people DROP COLUMN sign_in_count;
