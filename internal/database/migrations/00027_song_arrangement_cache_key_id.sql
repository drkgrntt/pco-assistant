-- +goose Up
-- key_id is a genuinely separate relationship from arrangement_id
-- (migration 00026), not implied by it - confirmed live: an item with
-- only its arrangement linked still shows no key at all, so publishing a
-- song's real key (see pco.CreateItemParams.KeyID, go-pco v0.11.0) needs
-- this too.
ALTER TABLE song_arrangement_cache
    ADD COLUMN key_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE song_arrangement_cache
    DROP COLUMN key_id;
