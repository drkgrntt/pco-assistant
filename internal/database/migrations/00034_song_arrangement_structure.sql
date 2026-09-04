-- +goose Up
-- Two more Planning Center arrangement fields that were already being
-- fetched and discarded every sync (see fetchAndCacheArrangementInfo,
-- internal/server/controllers/scheduling.go): the section-by-section
-- structure (Arrangement.Sequence/SequenceShort, joined into one display
-- string - see formatSongStructure) and whether the arrangement has a
-- chord chart attached (Arrangement.HasChordChart). Same
-- "always-overwrite" cache row as the rest of song_arrangement_cache
-- (00023) - no separate backfill needed, the existing sync paths
-- (resolveArrangementCacheInfo's cache-miss path, SyncSongInfo's bulk
-- backfill) just start writing these two columns on their next pass.
ALTER TABLE song_arrangement_cache ADD COLUMN structure TEXT NOT NULL DEFAULT '';
ALTER TABLE song_arrangement_cache ADD COLUMN has_chord_chart BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE song_arrangement_cache DROP COLUMN has_chord_chart;
ALTER TABLE song_arrangement_cache DROP COLUMN structure;
