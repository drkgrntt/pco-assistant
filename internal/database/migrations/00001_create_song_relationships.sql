-- +goose Up
CREATE TABLE song_relationships (
    id BIGSERIAL PRIMARY KEY,
    song_id_a TEXT NOT NULL,
    song_id_b TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_relationships_not_self CHECK (song_id_a <> song_id_b),
    CONSTRAINT song_relationships_unique UNIQUE (song_id_a, song_id_b, relationship_type)
);

CREATE INDEX song_relationships_song_id_a_idx ON song_relationships (song_id_a);
CREATE INDEX song_relationships_song_id_b_idx ON song_relationships (song_id_b);

-- +goose Down
DROP TABLE song_relationships;
