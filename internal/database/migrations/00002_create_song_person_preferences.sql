-- +goose Up
CREATE TABLE song_person_preferences (
    id BIGSERIAL PRIMARY KEY,
    song_id TEXT NOT NULL,
    person_id TEXT NOT NULL,
    preference TEXT NOT NULL CHECK (preference IN ('preferred', 'discouraged')),
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_person_preferences_unique UNIQUE (song_id, person_id)
);

CREATE INDEX song_person_preferences_song_id_idx ON song_person_preferences (song_id);
CREATE INDEX song_person_preferences_person_id_idx ON song_person_preferences (person_id);

-- +goose Down
DROP TABLE song_person_preferences;
