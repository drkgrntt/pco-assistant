-- +goose Up
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL,
    person_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
