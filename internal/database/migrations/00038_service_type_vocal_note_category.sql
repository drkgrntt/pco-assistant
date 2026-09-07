-- +goose Up
-- Remembers, per organization + service type, which Planning Center Item
-- Note category (pco.ItemNoteCategory - "Vocals," "Singers," whatever the
-- org named it) the song leader (set_songs.leader_person_id, migration
-- 00037) should sync into on Publish/Refresh song info - see
-- docs/plans/shipped/song-leader-plan.md's "Which note category is 'the
-- vocal one'?" decision (option 2: an explicit, one-time per-service-type
-- designation, not fuzzy name matching). Same flat, no-children shape as
-- service_type_first_song_anchors (00021) - one row per (organization_id,
-- service_type_id), no separate id/foreign-key children. '' (or no row at
-- all) both mean "not configured yet," in which case the leader sync is a
-- no-op - see scheduling.Store.VocalNoteCategoryID.
CREATE TABLE service_type_vocal_note_categories (
    organization_id TEXT NOT NULL,
    service_type_id TEXT NOT NULL,
    category_id     TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, service_type_id)
);

-- +goose Down
DROP TABLE service_type_vocal_note_categories;
