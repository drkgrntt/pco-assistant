-- +goose Up
-- Normalize existing rows to the same canonical order (song_id_a <
-- song_id_b) CreateSongRelationship now enforces on writes - otherwise a
-- pre-existing "backwards" row wouldn't collide with a future duplicate
-- entered in canonical order, since the unique constraint compares the
-- literal column values, not the unordered pair.
UPDATE song_relationships
SET song_id_a = song_id_b, song_id_b = song_id_a
WHERE song_id_a > song_id_b;

-- +goose Down
-- No-op: a relationship has no inherent direction, so there's nothing
-- meaningful to revert to.
