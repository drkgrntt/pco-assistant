-- +goose Up
-- Post-service debrief log / transition notes
-- (docs/plans/shipped/transition-notes-plan.md) - persistent, cross-plan
-- notes keyed to a song or a specific directional song-to-song transition,
-- resurfaced automatically the next time that song/pairing comes up in a
-- new draft set. Deliberately NOT song_relationships' canonical-ordering
-- shape (see that plan's Schema section): song_id_a/song_id_b are stored
-- exactly in the order entered ("out of A, into B"), since direction is
-- the entire content of a transition note - "abrupt into Communion" and
-- "abrupt into Gratitude" are different statements about the same two
-- songs, not the same relationship entered twice. Append-only (no
-- update-in-place, no unique constraint): every entry is a fresh INSERT,
-- and a row is only ever deleted (never edited), a plain input-correction
-- affordance mirroring song_relationships' DeleteSongRelationship. kind is
-- a plain CHECK, not an admin-extensible lookup table like
-- song_relationship_types (migration 00015) - there are only ever these
-- two kinds, nothing here needs an admin to add a third. No FK to a local
-- songs table, same as song_relationships - PCO is the source of truth
-- for song data, this app doesn't mirror a songs table locally.
CREATE TABLE song_transition_notes (
    id              BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('debrief', 'talking_point')),
    song_id_a       TEXT NOT NULL,
    song_id_b       TEXT,             -- NULL = single-song note; set = A-into-B transition note
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_transition_notes_not_self CHECK (song_id_b IS NULL OR song_id_a <> song_id_b)
);

CREATE INDEX song_transition_notes_lookup_idx
    ON song_transition_notes (organization_id, song_id_a, song_id_b, kind);

-- +goose Down
DROP TABLE song_transition_notes;
