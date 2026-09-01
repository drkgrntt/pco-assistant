-- +goose Up
-- Extend the free trial from 14 to 30 days. Only changes the default
-- applied to new rows going forward - deliberately not backfilling
-- trial_ends_at on organizations already mid-trial (see migration 00025),
-- since the product isn't live yet and there's no real trial history to
-- preserve or adjust.
ALTER TABLE organizations ALTER COLUMN trial_ends_at SET DEFAULT (now() + interval '30 days');

-- +goose Down
ALTER TABLE organizations ALTER COLUMN trial_ends_at SET DEFAULT (now() + interval '14 days');
