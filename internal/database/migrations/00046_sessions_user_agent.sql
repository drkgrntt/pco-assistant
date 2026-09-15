-- +goose Up
-- Captures the raw User-Agent header at sign-in (see auth.Callback) so the
-- Account page's "who's signed in" list can show which browser/device each
-- session belongs to (auth.DeviceLabel derives the short label from this at
-- render time - never stored pre-derived, so a smarter label later applies
-- retroactively to every still-live session) - the two rows a person signed
-- in on two devices produces are otherwise visually identical. Pre-existing
-- session rows get '' (auth.DeviceLabel's "Unknown device") until they
-- naturally expire (30-day TTL) or that person re-authenticates - not
-- something to backfill, since the original request that created them is
-- long gone.
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN user_agent;
