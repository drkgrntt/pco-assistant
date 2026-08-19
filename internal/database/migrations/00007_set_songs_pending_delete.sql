-- +goose Up
-- Removing (or replacing) a song that's already been published needs to
-- remember its pco_item_id long enough for the next Submit to actually
-- delete that item in Planning Center - so instead of hard-deleting the
-- row immediately, it's tombstoned here and cleaned up locally only after
-- the real delete succeeds (see scheduling.Store.RemoveSongFromSet).
ALTER TABLE set_songs ADD COLUMN pending_delete BOOLEAN NOT NULL DEFAULT false;

-- A tombstoned row and a fresh row can now briefly share a song_id (remove
-- then re-add the same song before publishing), so uniqueness on song_id
-- only applies among non-tombstoned rows.
ALTER TABLE set_songs DROP CONSTRAINT set_songs_unique_song;
CREATE UNIQUE INDEX set_songs_unique_active_song ON set_songs (set_id, song_id) WHERE pending_delete = false;

-- +goose Down
DROP INDEX set_songs_unique_active_song;
ALTER TABLE set_songs ADD CONSTRAINT set_songs_unique_song UNIQUE (set_id, song_id);
ALTER TABLE set_songs DROP COLUMN pending_delete;
