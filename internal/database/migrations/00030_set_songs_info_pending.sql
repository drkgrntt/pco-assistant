-- +goose Up
-- Tracks "this song's local Description/Key differs from what's actually
-- live in Planning Center" - the same concept `moved` already covers for
-- position, just for item content instead. Set whenever the description
-- or key override is edited (UpdateSetSongDescription/
-- UpdateSetSongKeyOverride); cleared once that content is actually pushed
-- (a successful create at Publish time, or a successful update via
-- Refresh song info) - see ClearInfoPending. Without this, an
-- already-published song's edited note/key looked identical to one that
-- was already in sync, with nothing on the page saying otherwise.
ALTER TABLE set_songs ADD COLUMN info_pending BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE set_songs DROP COLUMN info_pending;
