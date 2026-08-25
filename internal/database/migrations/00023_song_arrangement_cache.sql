-- +goose Up
-- A row's mere existence means "we've already checked Planning Center for
-- this song's arrangement" - even an all-blank row (PCO's own bare
-- default arrangement) counts, so a song with genuinely nothing set
-- doesn't get re-fetched forever (see songArrangementInfo's earlier
-- per-render live-call cost, and resolveArrangementInfo/
-- UpsertSongArrangementInfo which read/write this table). Unlike
-- tracked_new_songs' permanent one-time gate, this is refreshable - a
-- manual re-sync (see the Song Info page) upserts over whatever was here
-- before.
CREATE TABLE song_arrangement_cache (
    organization_id TEXT NOT NULL,
    song_id         TEXT NOT NULL,
    chord_chart_key TEXT NOT NULL DEFAULT '',
    bpm             DOUBLE PRECISION NOT NULL DEFAULT 0,
    meter           TEXT NOT NULL DEFAULT '',
    length_seconds  INT NOT NULL DEFAULT 0,
    cached_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, song_id)
);

-- +goose Down
DROP TABLE song_arrangement_cache;
