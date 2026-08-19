-- +goose Up
-- A "set" is a draft song lineup for one PCO plan - local to this app only,
-- nothing here is written back to Planning Center. One set per plan (the
-- unique constraint below), identified by the same (service_type_id,
-- plan_id) pair PCO itself needs to address a plan.
CREATE TABLE sets (
    id BIGSERIAL PRIMARY KEY,
    service_type_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sets_unique_plan UNIQUE (service_type_id, plan_id)
);

CREATE TABLE set_songs (
    id BIGSERIAL PRIMARY KEY,
    set_id BIGINT NOT NULL REFERENCES sets (id) ON DELETE CASCADE,
    song_id TEXT NOT NULL,
    -- 0-based order within the set. Kept contiguous (0..n-1) by the store
    -- layer on every insert/delete/reorder, so "next position" and
    -- "swap with neighbor" are simple arithmetic rather than needing gap
    -- bookkeeping.
    position INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT set_songs_unique_song UNIQUE (set_id, song_id),
    -- DEFERRABLE so a reorder can swap two rows' positions within one
    -- transaction (briefly colliding mid-transaction) without the swap
    -- needing a temporary sentinel value.
    CONSTRAINT set_songs_unique_position UNIQUE (set_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX set_songs_set_id_idx ON set_songs (set_id);

-- +goose Down
DROP TABLE set_songs;
DROP TABLE sets;
