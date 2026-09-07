-- +goose Up
-- A local, editable per-occurrence song leader ("who's actually leading
-- this song this week") - same "local draft, applied on the next Publish
-- or Refresh song info" pattern as description (00028) and key_override
-- (00029). Stores a PCO person id, not a name, so it stays a real person
-- reference rather than free text that can drift - see
-- docs/plans/shipped/song-leader-plan.md. Empty string means "no leader
-- set," the same "empty means nothing set yet" convention key_override/
-- description already use, not a nullable/NULL column.
ALTER TABLE set_songs ADD COLUMN leader_person_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE set_songs DROP COLUMN leader_person_id;
