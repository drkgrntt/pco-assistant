-- +goose Up
CREATE TABLE song_roles (
    id BIGSERIAL PRIMARY KEY,
    song_id TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_roles_unique UNIQUE (song_id, role)
);

CREATE INDEX song_roles_song_id_idx ON song_roles (song_id);
CREATE INDEX song_roles_role_idx ON song_roles (role);

-- +goose Down
DROP TABLE song_roles;
