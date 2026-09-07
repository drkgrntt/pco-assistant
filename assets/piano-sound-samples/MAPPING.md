# About this asset set

Trimmed and renamed from [piano_88_key_sound_sample](https://github.com/Leethring/piano_88_key_sound_sample)
(MIT, see `LICENSE-piano-sound-samples`; original naming/license explained
in `SOURCE-README.md`) down to a chromatic **E2–C6** run (45 semitones,
sharps only - the original pack recorded enharmonic flat spellings too,
dropped as redundant).

Files are named in plain scientific pitch notation - `<Letter><s if
sharp><octave>` - middle C is `C4`. Each `<name>.mp3` has a matching
`<name>.png` (piano-staff notation for that note). No decode table
needed; the filename is the pitch.

Range chosen to cover typical choir range (bass low E2 to soprano high
C6), per the vocal-range writeup in `docs/ideas.md`. Tracked in git -
served unauthenticated at `/piano-samples/` (see `internal/server/
routes.go`) and played by `playVocalTone` (`web/static/js/
scheduling.js`) as the vocal-range reference tone's primary sound, with
synthesis kept only as the fallback for a note outside this range.
