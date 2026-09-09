-- +goose Up
-- Fixes a real production bug: song_vocal_ranges stored an interval (low/
-- high offset above a reference-key tonic, internal/scheduling/
-- song_vocal_range.go's ComputeVocalRangeOffsets) rather than the exact
-- notes as entered, then reconstructed a displayable register at render
-- time by anchoring the tonic to a FIXED octave (referenceTonicOctave = 3)
-- instead of the octave the notes were actually entered in - a deviation
-- docs/plans/in-progress/vocal-range-plan.md's own Phase 2 status already
-- flagged as a known limitation. Concretely: E3-E4 entered against
-- reference key A came back displayed as E4-E5 - the entry-time anchoring
-- rule placed the tonic at A2 (immediately below E3), but the fixed-anchor
-- display placed it at A3, a full octave higher, silently shifting both
-- notes up an octave.
--
-- Fix: store the low/high notes as their own exact pitch class + octave,
-- the same shape person_vocal_ranges already uses, instead of an offset
-- from a re-anchored tonic. EffectiveVocalRange now just shifts these two
-- absolute values by SignedSemitoneDelta directly - an exact round trip
-- whenever the target key equals the reference key (no anchor-octave guess
-- involved at all), and a correct transposition otherwise.
--
-- Backfill for any row already on file computes the SAME values the old
-- fixed-anchor display logic already produced (tonic = AbsoluteSemitone(
-- reference key pitch class, 3) + offset) - so an existing row's displayed
-- range does not change as of this migration; it keeps showing exactly what
-- it showed before, until the next time someone re-saves that song's range
-- (which then captures the real entered octave, going forward, exactly).
-- The original entered octave genuinely isn't recoverable from what was
-- stored (that's the bug) - this is the best a migration can do for
-- existing data without asking anyone to blindly re-enter it.
ALTER TABLE song_vocal_ranges
    ADD COLUMN lowest_pitch_class SMALLINT,
    ADD COLUMN lowest_octave SMALLINT,
    ADD COLUMN highest_pitch_class SMALLINT,
    ADD COLUMN highest_octave SMALLINT;

-- Pitch-class lookup mirrors internal/scheduling/key_engine.go's
-- ParseKeyPitchClass exactly (same 7-letter table, one optional sharp/flat
-- accidental, a trailing "m" ignored) - safe to assume every reference_key
-- on file already matches this grammar, since setSongVocalRangeFromForm
-- (internal/server/controllers/song_vocal_range.go) has validated every
-- write through ParseKeyPitchClass since this table's very first migration.
--
-- Wrapped in goose's own StatementBegin/StatementEnd markers - goose's
-- default statement splitter breaks a migration file on every semicolon,
-- which would slice this DO block into fragments at each internal ";" and
-- send Postgres a truncated one missing its closing "$$" (surfaced as
-- "unterminated dollar-quoted string" at startup) - these markers tell
-- goose to treat everything between them as the one statement it actually
-- is.
-- +goose StatementBegin
DO $$
DECLARE
    rec RECORD;
    ref_pc INT;
    tonic_abs INT;
    low_abs INT;
    high_abs INT;
    accidental TEXT;
    key_no_mode TEXT;
BEGIN
    FOR rec IN SELECT id, reference_key, low_offset_semitones, high_offset_semitones FROM song_vocal_ranges LOOP
        key_no_mode := regexp_replace(trim(rec.reference_key), 'm$', '');
        ref_pc := CASE upper(left(key_no_mode, 1))
            WHEN 'C' THEN 0 WHEN 'D' THEN 2 WHEN 'E' THEN 4 WHEN 'F' THEN 5
            WHEN 'G' THEN 7 WHEN 'A' THEN 9 WHEN 'B' THEN 11
            ELSE NULL
        END;
        accidental := substring(key_no_mode from 2);
        IF accidental = '#' OR accidental = '♯' THEN
            ref_pc := ref_pc + 1;
        ELSIF accidental = 'b' OR accidental = '♭' THEN
            ref_pc := ref_pc - 1;
        END IF;
        ref_pc := ((ref_pc % 12) + 12) % 12;

        -- Mirrors the old fixed referenceTonicOctave=3 anchor exactly, so
        -- an existing row's displayed range is unchanged by this migration.
        tonic_abs := 3 * 12 + ref_pc;
        low_abs := tonic_abs + rec.low_offset_semitones;
        high_abs := tonic_abs + rec.high_offset_semitones;

        UPDATE song_vocal_ranges SET
            lowest_pitch_class = ((low_abs % 12) + 12) % 12,
            lowest_octave = (low_abs - (((low_abs % 12) + 12) % 12)) / 12,
            highest_pitch_class = ((high_abs % 12) + 12) % 12,
            highest_octave = (high_abs - (((high_abs % 12) + 12) % 12)) / 12
        WHERE id = rec.id;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE song_vocal_ranges
    ALTER COLUMN lowest_pitch_class SET NOT NULL,
    ALTER COLUMN lowest_octave SET NOT NULL,
    ALTER COLUMN highest_pitch_class SET NOT NULL,
    ALTER COLUMN highest_octave SET NOT NULL,
    DROP COLUMN low_offset_semitones,
    DROP COLUMN high_offset_semitones;

-- +goose Down
ALTER TABLE song_vocal_ranges
    ADD COLUMN low_offset_semitones SMALLINT,
    ADD COLUMN high_offset_semitones SMALLINT;

-- +goose StatementBegin
DO $$
DECLARE
    rec RECORD;
    ref_pc INT;
    tonic_abs INT;
    accidental TEXT;
    key_no_mode TEXT;
BEGIN
    FOR rec IN SELECT id, reference_key, lowest_pitch_class, lowest_octave, highest_pitch_class, highest_octave FROM song_vocal_ranges LOOP
        key_no_mode := regexp_replace(trim(rec.reference_key), 'm$', '');
        ref_pc := CASE upper(left(key_no_mode, 1))
            WHEN 'C' THEN 0 WHEN 'D' THEN 2 WHEN 'E' THEN 4 WHEN 'F' THEN 5
            WHEN 'G' THEN 7 WHEN 'A' THEN 9 WHEN 'B' THEN 11
            ELSE NULL
        END;
        accidental := substring(key_no_mode from 2);
        IF accidental = '#' OR accidental = '♯' THEN
            ref_pc := ref_pc + 1;
        ELSIF accidental = 'b' OR accidental = '♭' THEN
            ref_pc := ref_pc - 1;
        END IF;
        ref_pc := ((ref_pc % 12) + 12) % 12;

        tonic_abs := 3 * 12 + ref_pc;

        UPDATE song_vocal_ranges SET
            low_offset_semitones = (rec.lowest_octave * 12 + rec.lowest_pitch_class) - tonic_abs,
            high_offset_semitones = (rec.highest_octave * 12 + rec.highest_pitch_class) - tonic_abs
        WHERE id = rec.id;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE song_vocal_ranges
    ALTER COLUMN low_offset_semitones SET NOT NULL,
    ALTER COLUMN high_offset_semitones SET NOT NULL,
    DROP COLUMN lowest_pitch_class,
    DROP COLUMN lowest_octave,
    DROP COLUMN highest_pitch_class,
    DROP COLUMN highest_octave;
