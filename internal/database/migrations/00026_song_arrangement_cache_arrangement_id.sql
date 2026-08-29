-- +goose Up
-- arrangement_id lets a publish link a plan item straight to the song's
-- real arrangement (see pco.CreateItemParams.ArrangementID, go-pco v0.9.0) -
-- without it, an item created via the API has no arrangement relationship
-- at all (confirmed live), so Planning Center shows nothing but the bare
-- title for it: no chord chart, lyrics, or structure, unlike a song added
-- through PCO's own UI. notes carries the arrangement's own persistent
-- Notes text through as the item's one-time Description default - the
-- closest thing to a sensible default until there's a real per-occurrence
-- (rather than per-arrangement) notes feature.
ALTER TABLE song_arrangement_cache
    ADD COLUMN arrangement_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN notes          TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE song_arrangement_cache
    DROP COLUMN arrangement_id,
    DROP COLUMN notes;
