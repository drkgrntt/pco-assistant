-- +goose Up
-- Phase 2 of docs/plans/in-progress/vocal-range-plan.md: a song's sung
-- range, stored relative to the key it was measured in - a bare (low, high)
-- note pair would be meaningless without knowing what key it was measured
-- against (the whole point of "relative to key" in the source
-- docs/ideas.md bullet's own title). reference_key is kept alongside the
-- two offsets purely so the entry can be re-derived/audited later, even
-- though only the offsets are used at transposition time - see
-- internal/scheduling/song_vocal_range.go's ComputeVocalRangeOffsets/
-- EffectiveVocalRange.
--
-- Numbered 00041, not 00039/00040 (the next-available numbers as of this
-- migration's own creation) - those two were reserved for other concurrent
-- work landing in this same batch, per this task's own build instructions.
--
-- One row per song per org - org-scoped, not shared vocabulary: a
-- manually-entered range is still one org's own input, the same way
-- song_tags/song_roles are org-scoped despite song_id itself being a
-- shared PCO identifier.
CREATE TABLE song_vocal_ranges (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    song_id TEXT NOT NULL,
    reference_key TEXT NOT NULL,
    low_offset_semitones SMALLINT NOT NULL,
    high_offset_semitones SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, song_id)
);

-- +goose Down
DROP TABLE song_vocal_ranges;
