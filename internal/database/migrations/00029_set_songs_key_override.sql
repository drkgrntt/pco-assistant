-- +goose Up
-- A local, editable per-occurrence key override ("put this in Bb this
-- week"), distinct from the song's arrangement's own default key -
-- editing this never touches the arrangement, only what this one
-- occurrence's plan item links to. Same "local draft, applied on the next
-- Publish or Refresh song info" pattern as description (migration 00028).
ALTER TABLE set_songs ADD COLUMN key_override TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE set_songs DROP COLUMN key_override;
