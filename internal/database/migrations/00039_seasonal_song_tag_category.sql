-- +goose Up
-- Adds the "seasonal" song_tag_category (see migration 00016's comment:
-- "add a row there for another one, no code change needed") for songs
-- like Christmas/Easter numbers that have an intentional long gap between
-- scheduled dates by design - used to exempt those songs from the
-- candidate list's "not sung in a while" nudge (see scheduling.
-- TagCategorySeasonal, internal/scheduling/song_tags.go).
INSERT INTO song_tag_categories (code, label, sort_order) VALUES
    ('seasonal', 'Seasonal', 4);

-- +goose Down
DELETE FROM song_tag_categories WHERE code = 'seasonal';
