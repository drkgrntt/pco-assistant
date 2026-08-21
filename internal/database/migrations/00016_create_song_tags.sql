-- +goose Up
-- Song tags are separate from song_roles (opener/response - see migration
-- 00004/00015): roles feed the set builder's candidate filtering, tags are
-- purely descriptive, letting a song be found later by sermon theme,
-- related scripture, or a loose idea. Categories are a lookup table for the
-- same operational reason as song_role_types/song_preference_types - a new
-- category is just a row here, no code change/deploy needed - but the value
-- itself is free text, since themes/scripture references/ideas are
-- open-ended in a way opener/response never is.
CREATE TABLE song_tag_categories (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO song_tag_categories (code, label, sort_order) VALUES
    ('theme', 'Theme', 1),
    ('scripture', 'Scripture', 2),
    ('idea', 'Idea', 3);

CREATE TABLE song_tags (
    id BIGSERIAL PRIMARY KEY,
    song_id TEXT NOT NULL,
    category TEXT NOT NULL REFERENCES song_tag_categories (code),
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_tags_unique UNIQUE (song_id, category, value)
);

CREATE INDEX song_tags_song_id_idx ON song_tags (song_id);
CREATE INDEX song_tags_category_idx ON song_tags (category);

-- +goose Down
DROP TABLE song_tags;
DROP TABLE song_tag_categories;
