-- +goose Up
-- A song can now appear more than once in a set (e.g. reprised as the
-- response song) - "duplicate" is now an explicit action (see
-- DuplicateSongInSet), so uniqueness on (set_id, song_id) no longer holds.
-- The normal Add flow still refuses to add a song that's already active,
-- but that's an application-level check now, not a database constraint.
DROP INDEX set_songs_unique_active_song;

-- Every set_songs operation used to identify "which song" by its PCO
-- song_id, which stops being unique once duplicates are allowed. They all
-- now identify a specific row by its own id instead, so "last touched" has
-- to track that row id rather than a song_id string - ON DELETE SET NULL
-- so a hard-deleted row (see RemoveSongFromSet) just clears the pointer
-- instead of leaving it dangling.
ALTER TABLE sets DROP COLUMN last_touched_song_id;
ALTER TABLE sets ADD COLUMN last_touched_set_song_id BIGINT REFERENCES set_songs (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE sets DROP COLUMN last_touched_set_song_id;
ALTER TABLE sets ADD COLUMN last_touched_song_id TEXT NOT NULL DEFAULT '';

-- Only safe if no set currently has an active duplicate - matches the
-- original migration's assumption.
CREATE UNIQUE INDEX set_songs_unique_active_song ON set_songs (set_id, song_id) WHERE pending_delete = false;
