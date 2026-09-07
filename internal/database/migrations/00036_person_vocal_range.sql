-- +goose Up
-- Phase 1 of docs/plans/in-progress/vocal-range-plan.md: a bare person-level
-- vocal range fact, independent of any song data. A pitch class alone
-- (0-11) loses register - a "G" doesn't say whether it's G3 or G5, which
-- matters - so each note is stored as a pitch class + octave pair. The two
-- pairs combine into one internally-consistent absolute integer
-- (octave * 12 + pitch_class, no requirement to match real MIDI numbering)
-- for comparison, computed at read time rather than stored redundantly -
-- see internal/scheduling/person_vocal_range.go.
--
-- One row per person per org (mirrors song_person_preferences' "only one
-- current value" upsert shape, see SetSongPersonPreference) - org-scoped
-- like every other table in song_preferences.go/person_relationships.go,
-- even though a person's PCO id isn't itself org-scoped (same reasoning
-- song_person_preferences already applies to a PCO song/person pair).
CREATE TABLE person_vocal_ranges (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    person_id TEXT NOT NULL,
    lowest_pitch_class SMALLINT NOT NULL,
    lowest_octave SMALLINT NOT NULL,
    highest_pitch_class SMALLINT NOT NULL,
    highest_octave SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, person_id)
);

-- +goose Down
DROP TABLE person_vocal_ranges;
