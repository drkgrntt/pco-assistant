-- +goose Up
-- Which song was most recently added/replaced/re-anchored in this set -
-- lets AddSongToSet place a newly-added song right after it (same anchor,
-- next position) instead of always defaulting back to the top of service.
-- NULL/empty means "no basis to chain from," falling back to that default.
ALTER TABLE sets ADD COLUMN last_touched_song_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sets DROP COLUMN last_touched_song_id;
