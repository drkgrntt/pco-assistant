-- +goose Up
-- The real per-occurrence notes feature migration 00026 mentioned as a
-- future possibility ("cut the bridge this week," "new keys player, go
-- slow") - a local, editable draft field per set_songs row, distinct from
-- the arrangement's own persistent notes. PublishSet/RefreshSongInfo fall
-- back to the arrangement's notes when this is empty, same default
-- behavior as before this column existed.
ALTER TABLE set_songs ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE set_songs DROP COLUMN description;
