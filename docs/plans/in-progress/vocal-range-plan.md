# Plan: vocal range (person + song) and the key engine

**Status: Phase 1 shipped 2026-09-07** - the standalone person vocal range
setup page (migration `00036_person_vocal_range.sql` - `00035` was already
taken by `song_attachment_cache` by the time this landed, so the number
shifted from this doc's own `00035` guess; `internal/scheduling/
person_vocal_range.go`'s `Store` methods; `internal/server/controllers/
vocal_range.go`'s page/set/delete handlers and standalone note-with-octave
parser; `internal/views/scheduling/vocal_range.templ`; the `/scheduling/
vocal-range` route and Setup nav link). **The Phase 1 "Step 2, later"
reference-tone piece from `docs/ideas.md`'s own bullet also shipped
2026-09-07**: a tap-to-hear button next to each stored/entered note
(`internal/views/scheduling/vocal_range.templ`, `IconPlay` in
`internal/views/components/icons.templ`) plays a reference tone
(`playVocalTone`/`parseVocalNoteText`/`playVocalNoteFromInput` in
`web/static/js/scheduling.js`). Originally built as a synthesized-only tone
(a Web Audio `OscillatorNode` at the correct A440-tuned frequency), on the
reasoning that a clean sine tone is just as good a pitch reference as a
recorded note and needs no asset files - **since reversed, also
2026-09-07**: `assets/piano-sound-samples/` (a real chromatic E2-C6 piano
recording set, MIT-licensed, tracked in git) now exists and is served
unauthenticated at `/piano-samples/` (`internal/server/routes.go`,
exempted from `RequireAuth` in `internal/server/middleware/auth.go`), so
`playVocalTone` now plays the real recorded sample for any note within
that range (a more faithful, pleasant reference than a synthesized sine),
falling back to the original oscillator synthesis only for a note outside
E2-C6 - rare in practice, since that range already covers typical choir
range (see that folder's own `MAPPING.md`).

**Phase 2 (key engine + song vocal range) also shipped 2026-09-07** - the
key engine extraction (`internal/scheduling/key_engine.go`:
`ParseKeyPitchClass`/`SemitoneDistance` moved verbatim out of
`sets_builder.go`, plus new `SignedSemitoneDelta`/`ParseNote`/
`AbsoluteSemitone`/`PitchClassAndOctave`; `sets_builder.go`'s
`keyTransitionWarnings`/`suggestedDominantSeventh` now call the
`scheduling.` versions; `vocal_range.go`'s `parseVocalNote`/
`absoluteVocalSemitone` are now thin wrappers around
`scheduling.ParseNote`/`scheduling.AbsoluteSemitone` rather than a second
copy of the same parsing logic; tests relocated to
`internal/scheduling/key_engine_test.go`), the `song_vocal_ranges` table
(migration `00041_song_vocal_range.sql` - `00039`/`00040` were reserved for
other concurrent work landing in the same batch, so the number shifted
from this doc's own `00036` guess), `internal/scheduling/
song_vocal_range.go`'s `Store` methods plus `ComputeVocalRangeOffsets`
(the anchoring rule) and `EffectiveVocalRange` (transposition),
`internal/server/controllers/song_vocal_range.go`'s page/set/delete
handlers, `internal/views/scheduling/song_vocal_range.templ`, the
`/scheduling/song-vocal-range` route and Setup nav link, and a "Vocal
range: G3-D5 (in this key)" line on the Build page's song detail card
(`song_detail_card.templ`) showing the transposed result live - see this
plan's own "Phase 2 prerequisite: the key engine" and "Phase 2: song
vocal range" sections below for the exact math, unchanged from what's
written there, with one flagged deviation: `EffectiveVocalRange` anchors
the reference key's tonic at a **fixed** octave (3) when reconstructing a
display value at render time, rather than the original entry-time octave
- the `song_vocal_ranges` schema, as specified, only persists the two
offsets and the reference key text, not the octave the low/high notes
were actually typed in, so the exact original register can't be
recovered byte-for-byte; a fixed anchor preserves the interval's real
width and the correct transposition shift (the two things that actually
matter for a display), just not necessarily the exact octave a director
had in mind. See that function's own doc comment for the full reasoning.
Phase 3's own mismatch warning has since shipped too - see the status
paragraph below. The matrix view and the crowdsourced-default question
remain not started - see those sections below, unchanged from the
original plan.

**Song Vocal Range page gets play buttons + a current-key line, also
2026-09-07** (the same day the piano samples above landed, and the
natural place to spend them - this page previously had no tap-to-hear
affordance at all, unlike the Person Vocal Range page). Every stored
range's Range column (`internal/views/scheduling/song_vocal_range.templ`)
now shows an "As measured (in `<reference key>`)" line with a play button
on each of its two notes, mirroring the Person Vocal Range page's pattern
exactly (`components.IconPlay`, `templ.JSFuncCall("playVocalTone", ...)`);
the add-range form gained the same lowest/highest-note preview buttons
(`playVocalNoteFromInput`) the person page's form already had. New on top
of that mirror: a second "Current key (`<key>`)" line appears whenever the
song's actual cached PCO arrangement key (`SongArrangementInfo.Key`,
resolved via the existing `resolveArrangementCacheInfo` enqueue-on-miss
helper - `song_vocal_range.go`'s new `currentKeysBySong`, one bulk
cache-only read per render, no new PCO call) is known, non-empty, and
different from the stored reference key - both lines share one new
`computeVocalRangeNotePair` helper (`song_vocal_range.templ`) so
`EffectiveVocalRange`/`PitchClassAndOctave` only run once per line instead
of once per note, replacing the old `songVocalRangeLowDisplay`/
`songVocalRangeHighDisplay` pair. All three handlers touching this page's
table fragment (`SongVocalRangePage`/`SetSongVocalRange`/
`DeleteSongVocalRange`) build and thread the same current-key map, not
just the initial page load. See `docs/pco-api-risks.md`'s entry for the
fan-out-stays-small reasoning (this page only ever lists songs that
already have a stored range, a small subset of the active song library).

**Song Vocal Range page gets a key/octave preview control, 2026-09-08** -
a `docs/ideas.md`-adjacent request ("change the relative key of an
existing range to hear what the modulated range would be... bring it
down an octave... male/female") tracked in `docs/issues.md`. Deliberately
**preview-only**: nothing here changes `r.ReferenceKey`/`LowOffsetSemitones`/
`HighOffsetSemitones` or the As-measured/Current-key lines above it -
picking a key or clicking an octave button only changes what plays and
what a new, third preview line shows. Per-row control
(`vocalRangePreviewControls`, `song_vocal_range.templ`): a text input
(reusing the entry form's shared `key-options` datalist, `musicalKeyOptions`)
plus two octave up/down buttons, all client-side - the transposition
arithmetic (`renderVocalRangePreview` and its helpers, `web/static/js/
scheduling.js`) duplicates `ParseKeyPitchClass`/`SignedSemitoneDelta`/
`PitchClassAndOctave`/`referenceTonicOctave` (`key_engine.go`,
`song_vocal_range.go`) function-for-function rather than round-tripping
through htmx on every keystroke/click - the same "duplicate the
primitives, keep them numerically identical" call already made for
`vocalAbsoluteSemitone`/`vocalNoteFrequency` mirroring `AbsoluteSemitone`.
An empty key input falls back to the row's own reference key, so the
octave buttons alone (no key ever typed) already preview something -
covers the "just show me an octave down" case without requiring a key
pick first. New page-specific `web/static/css/song-vocal-range.css`
(registered in `layout.templ`'s `designSystemCSSFiles`) backs the
control's layout; the play buttons it generates duplicate `IconPlay`'s
exact SVG markup by hand (see that JS section's own doc comment) rather
than sharing a template, so a client-side-generated button still matches
every server-rendered `.icon-btn` on the page.

**Song Vocal Range page's "Current key" line removed, same day
(2026-09-08)** - `docs/issues.md` follow-up feedback, after the preview
control above shipped: a song can have more than one PCO arrangement in
different keys, so "the" current key was never actually a single
well-defined answer, and the preview control already covers "show me this
range in some other key" on demand for whichever key the reader actually
wants - a strictly more useful, always-correct replacement for a line that
could only ever guess at one cached key. Removed: the "Current key
(`<key>`)" line itself, `song_vocal_range.go`'s `currentKeysBySong`
helper (and its dedicated DB test), and the `currentKeysBySong`
parameter threaded through `SongVocalRanges`/`SongVocalRangesTable`. The
"As measured" line and `computeVocalRangeNotePair` helper are unaffected -
both are still used, now for exactly one line per row instead of two.

**Phase 3 (the mismatch warning) shipped 2026-09-08**, exactly as specced
above: `vocalRangeWarningForSong`/`vocalRangeFitsShifted`/
`vocalRangeViolationNotes` (`internal/server/controllers/sets_builder.go`)
cross-reference a plan's roster assignments (`scheduling.Store.FindRoster`/
`ListRosterAssignments` - roster assignments, not an eligibility pool, per
this section's own "who counts" rule) against `ListPersonVocalRanges`, both
local-DB reads with no new PCO call; the one new PCO call this phase adds
is a `personNameMapForIDs` lookup for display names, batched and bounded to
just the roster members who both are assigned and have a range on file (0
calls whenever nobody assigned has one) - the same established pattern
`leaderNames` already uses on this same render path, not a new risk
category, so `docs/pco-api-risks.md` needed no new mitigation entry.
`SetSongView.VocalRangeWarningLabel`/`Message`/`IsTip` feed a new badge in
`songBadgeSpans` (`internal/views/scheduling/set_builder.templ`), reusing
`components.Disclosure` exactly the way `keyTransitionWarnings`' own badge
already does: a hard mismatch renders like the key-transition badge
(conflict-badge styling, a leading ⚠), the octave-shift-fixable case (see
this section's own "Octave-shift tip" paragraph, implemented unchanged)
downgrades to a plain, non-warning pill like the transition-note prompt's
badge - both share one popup with one sentence per affected vocalist
("Sarah's range is A3-D5; this song's current key needs Sarah to reach up
to F5.", naming the person again rather than a pronoun, since no
gender/pronoun data exists anywhere in this codebase to draw one from - a
deviation from this section's own illustrative wording, not from its
substance). Severity/threshold shipped exactly as this section already
called as the default: warn on any note outside range, no tolerance band.
New tests: `TestVocalRangeWarningForSong*`/`TestVocalRangeFitsShifted*`/
`TestVocalRangeViolationNotes*` in `sets_builder_warnings_test.go`. The
matrix view remains its own separate future plan doc, unchanged from this
plan's original "Suggested build order" step 6.

**Two more Build-page pieces landed the same day, both from `docs/issues.md`
feedback rather than this plan's own original scope:**

- **Busy-row decluttering**: once a Songs-tab row's total badge count
  passes a new `badgeCollapseThreshold` (4 - a judgment call, not specified
  anywhere before this), the lower-priority badges collapse behind a "+N
  more" `components.Disclosure` - role badges and resurfaced
  transition-note prompts on a set song's own row (`songBadgeExtras`), role
  and tag badges on a not-yet-added candidate's row (`candidateBadgeExtras`)
  - while status/warning badges (published/moved/info-pending, avoid-pairing,
  repeat, key-transition, and this phase's own vocal-range mismatch) always
  stay directly on the row regardless of count. Same
  `internal/views/scheduling/set_builder.templ`/`web/static/js/
  scheduling.js`/`web/static/css/components/disclosure.css` files as the
  mismatch badge above.
- **Vocal range as a fourth build-page setup surface**: song vocal range
  now has the same three Build-page entry points song key/tags/
  relationships/roles already have - a "Set song vocal range" quick-add
  item (`quickAddItem`, mirroring Tag a song/Relate two songs/Add a role
  exactly), a "Set vocal range" row kebab-menu action opening a fourth
  per-row `quickActionRow` (mirroring Tag/Relate/Mark role exactly, same
  build-scoped route/whole-builder-re-render shape), and a matching jump
  button on the song detail card sidebar (`songDetailCard`,
  `internal/views/scheduling/song_detail_card.templ`) - all three post to
  a new `QuickAddSongVocalRange` handler
  (`internal/server/controllers/sets_detail.go`) that calls
  `setSongVocalRangeFromForm`, a helper factored out of the dedicated setup
  page's own `SetSongVocalRange` handler (`song_vocal_range.go`) so the two
  posting surfaces can never validate/anchor a range differently from each
  other - the exact same `scheduling.Store.SetSongVocalRange` upsert either
  way. This does **not** reverse this section's own "deliberately NOT
  embedded in the Build page's song detail card" decision above: that
  decision was specifically against an always-visible inline form there
  (the way `keyForm`/`descriptionForm` work for genuinely per-occurrence
  values); what shipped here is the same "quick-add panel + kebab-menu
  jump-to-a-hidden-row" pattern song tags/relationships/roles already use
  for their own equally song-level (not per-occurrence) facts, just newly
  extended to cover vocal range too. The card's own read-only "Vocal range:
  G3-D5 (in this key)" line (Phase 2, unchanged) is still the only thing
  permanently visible on the card itself.

Written 2026-09-06, expanding two related `docs/ideas.md` bullets into one
coherent build plan: **"Vocal range per vocalist"** (People section,
~line 462) and **"Song vocal range (relative to key)"** (Songs section,
~line 374). Not built yet. `docs/ideas.md`'s own prioritization note
(~line 692) explicitly sequences these two plus a follow-on matrix view,
and this plan preserves that sequencing rather than re-deriving it:
**person range first** (small, standalone, useful immediately), **then**
song range plus the key engine it needs, **then** a presentation-only
matrix view once both exist. This plan does not strike anything in
`docs/ideas.md` - per this task's own instructions, a brainstorm bullet
stays open/unstruck until something is actually built, the same
convention `docs/plans/shipped/song-tags-setup-redesign-plan.md` followed
for its own source bullet while it was still open (`docs/plans/shipped/key-change-tip-
plan.md` has since built and struck its own bullet the same way, once it
actually shipped).

## Current state

**No vocal range storage exists anywhere in this codebase today** -
confirmed independently by `docs/plans/shipped/song-lead-sheet-attachments-plan.md`
("vocal range itself (person or song) doesn't exist in this codebase
yet"), and reconfirmed here (no `vocal_range`/range table, no note-parsing
helper beyond the key-transition warning's pitch-class-only parser).

**The existing pitch-class math** lives entirely inside
`internal/server/controllers/sets_builder.go`, inlined for one consumer:

- `keyPitchClasses` (~L1244): a 7-entry map, natural letter -> pitch class
  0-11.
- `parseKeyPitchClass` (~L1257): parses a key string ("Bb", "F#m", "G")
  into a 0-11 pitch class, ignoring major/minor quality. No octave
  concept at all - a bare key name ("G") has no register, which is fine
  for comparing two songs' keys but not enough for a vocal range (a
  person's "G" needs to mean G3 or G5, not just "G, some G").
- `semitoneDistance` (~L1283): the minimal **unsigned** circular distance
  (0-6) between two pitch classes - enough to detect "these two keys are
  a half-step apart," but not enough to actually *shift* a stored value
  by the right number of semitones in the right direction, which song
  range transposition needs (see "Key engine" below).
- `keyTransitionWarnings` (~L1319): the sole consumer, flagging a
  half-step/tritone key change between two back-to-back songs.

**How that warning surfaces today**
(`docs/plans/shipped/in-place-set-warnings-plan.md`): a short always-visible
badge on the *second* song's own row in the Songs tab
(`songBadgeSpans`/`set_builder.templ`), wrapping the full explanatory
sentence in a click-to-expand `components.Disclosure` popover
(`keyTransitionToggle`/`keyTransitionMenu`) rather than a hover tooltip or
a top-of-page list. This is the surface a vocal-range mismatch warning
should reuse - see Phase 3 below.

**Existing setup-page pattern** for org-scoped, song- or person-keyed
local facts (`internal/scheduling/song_preferences.go` +
`internal/views/scheduling/preferences.templ` +
`internal/server/controllers/people.go`'s `PersonRelationshipsPage`/
`PositionPreferencesPage`): a dedicated page under `/scheduling/<name>`,
linked from the "Setup" nav dropdown (`internal/views/layout.templ`
~L163-183), with a small inline form (song `<select>` and/or
`PersonPickerField`) posting to a handler that upserts one row per
subject, plus a `SortableTableWrap` listing existing rows with inline
edit/delete. `SongPersonPreference`'s `SetSongPersonPreference` shows the
"only one current value per subject" upsert shape (`ON CONFLICT
(organization_id, song_id, person_id) DO UPDATE`) this plan reuses for
person range (keyed by person alone) and song range (keyed by song
alone).

**Migrations**: the latest applied migration is
`00034_song_arrangement_structure.sql`
(`internal/database/migrations/`), so Phase 1's new table is named here as
`00035_person_vocal_range.sql`. Phase 2's migration number isn't fixed as
precisely, since other migrations may land between the two phases -
called out below as "next available number at build time," following the
same hedge `docs/plans/shipped/song-lead-sheet-attachments-plan.md`
implicitly avoids by being written closer to its own build.

**go-pco already exposes a real cross-org join key candidate** for songs:
`SongData.Attributes.CCLINumber` (`go-pco/songs.go` ~L16, an `int`,
already wrapped, readable via the same `GetSongs`/`GetSong` calls this
app already makes - no new `go-pco` version needed to *read* it). This
directly informs the crowdsourced-default open question below - it's not
"does a join key exist," it's "how it holds up in practice" (see that
section).

## Which product promise this serves

Per CLAUDE.md's "Product direction" test: this serves **both**, but
leans differently by phase. Person range alone (Phase 1) is squarely
**protect the team** - it exists to make an already-real conversation
("can Mike hit this note?") faster and less awkward to have, the same
register as burnout/preference/relational-friction concerns CLAUDE.md
names explicitly. The mismatch warning (Phase 3) is squarely **protect
the service** - it's a "conflict caught before publish," the exact same
shape as the existing key-transition warning it sits next to, catching a
vocal-range problem before it's discovered live in rehearsal rather than
on paper. The two phases aren't in tension; they're the same underlying
data serving each promise at a different point in the workflow (data
entry protects the team's dignity in the moment; the derived warning
protects the service's outcome later).

## Phase 1: person vocal range (standalone)

**Confirmed zero-dependency**: this phase needs no song data at all to be
useful - it's a bare person-level fact, immediately answerable as "does
this person have a comfortable range on record" regardless of whether any
song has one. No new PCO calls: `PersonPickerField`/`/scheduling/people-search`
already exist and are reused as-is.

**Storage shape** - what "lowest/highest comfortable note as a
pitch-class/semitone pair" concretely means: a bare 0-11 pitch class alone
loses register (a "G" doesn't say whether it's G3 or G5, which matters -
a low G and a high G are not remotely the same ask of a vocalist), so
each note needs a **pitch class + octave** pair, combined into one
internally-consistent absolute integer (`octave * 12 + pitch_class` -
no requirement to match real MIDI note numbering, just to be internally
consistent so two absolute values are directly comparable and
subtractable). New migration `00035_person_vocal_range.sql`:

```sql
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
```

One row per person per org (mirrors `SongPersonPreference`'s "only one
current value" upsert shape) - org-scoped like every other table in
`song_preferences.go`/`person_relationships.go`, per CLAUDE.md's
Organization scoping section (a person's PCO id isn't itself org-scoped,
but every local fact this app attaches to it is, same as
`song_person_preferences` already does for a PCO song/person pair).

**New files**, following the existing pattern exactly:

- `internal/scheduling/person_vocal_range.go` - `Store` methods
  `SetPersonVocalRange`, `PersonVocalRange` (single lookup),
  `ListPersonVocalRanges` (bulk map for a page render, matching
  `SongArrangementInfo`'s bulk-read precedent), `DeletePersonVocalRange`,
  mirroring `song_preferences.go`'s shape and doc-comment style.
- `internal/server/controllers/vocal_range.go` - `PersonVocalRangePage`,
  `SetPersonVocalRange`, `DeletePersonVocalRange` handlers, mirroring
  `people.go`'s `PersonRelationshipsPage`/`CreatePersonRelationship`/
  `DeletePersonRelationship` shape (a page handler plus create/delete,
  no separate update - same "delete and re-add" or upsert-on-resubmit
  convention the other setup pages use).
- `internal/views/scheduling/vocal_range.templ` - form
  (`PersonPickerField` + two note inputs) plus a `SortableTableWrap`
  listing every person with a range on record, mirroring
  `preferences.templ`'s structure with the song `<select>` removed.
- Route: `/scheduling/vocal-range` (GET page, POST set, DELETE by id),
  registered in the same `RegisterRoutes` group as the other setup
  routes; nav link added to `internal/views/layout.templ`'s Setup
  dropdown alongside "Song / Person Preferences"/"Person Relationships."

**Note entry UI**: a plain text input per note (lowest, highest) backed
by a `<datalist>`, the same pattern `musicalKeyOptions`
(`song_detail_card.templ`) already uses for key entry, extended with an
octave digit (e.g. "G3", "C#4", "Bb2") spanning a reasonable vocal range
(roughly 2-3 octaves is enough datalist entries to stay small - exact
bounds are a copy/UX detail, not a data-model one). Parsing this string
is new code - see "Key engine" below, since `parseKeyPitchClass` alone
has no octave concept.

## Phase 2 prerequisite: the key engine

`docs/ideas.md`'s own bullet names the trigger for factoring this out:
"[the pitch-class parser is] currently inlined for one warning check - a
second consumer is a real reason to factor it into an actual shared 'key
engine'... rather than copy-pasting the parser again." This phase names
that engine.

**Why now, and not when `key-change-tip-plan.md` considered the same
question**: that plan explicitly declined to do this extraction, but for
a specific, narrow reason - its own new code stayed inside
`sets_builder.go`, called the existing parser directly (no duplication),
and needed nothing storage-shaped (a pure render-time string, nothing
persisted). It said outright: *"When the vocal-range work actually
starts and needs a real `internal/scheduling` key engine, move all of it
... over together at that point."* This is that point - song vocal range
(below) needs to **store** a transposable interval and **transpose** it
using a signed shift, which `sets_builder.go`'s existing unsigned
`semitoneDistance` cannot do alone, and it needs to live somewhere a
setup-page controller and a Build-page controller can both reach without
a controller-to-controller import (`internal/server/controllers` has no
convention of one controller file importing another's internals) -
`internal/scheduling` is the correct home per CLAUDE.md's Architecture
section (domain math and the local data built on it live in
`internal/scheduling`; `internal/server/controllers` orchestrates
requests/PCO calls around it).

**Target file**: `internal/scheduling/key_engine.go` - a new
dependency-free file in the existing domain package (same import
boundary `song_relationships.go`/`song_preferences.go` already sit
inside, no new package needed).

**What moves, verbatim, from `sets_builder.go`**:

- `keyPitchClasses` -> exported (`KeyPitchClasses`, or kept unexported
  behind an exported parse function - match whichever convention reads
  cleaner against the existing exported `Store` methods in the package).
- `parseKeyPitchClass` -> `scheduling.ParseKeyPitchClass`.
- `semitoneDistance` -> `scheduling.SemitoneDistance` (unchanged
  behavior - still the unsigned 0-6 circular distance the existing
  warning needs).

`sets_builder.go`'s `keyTransitionWarnings` calls the `scheduling.`
versions instead of local ones; the local copies are deleted. Existing
tests (`TestParseKeyPitchClass*`, `TestSemitoneDistanceWrapsAround` in
`sets_builder_warnings_test.go`) move to a new
`internal/scheduling/key_engine_test.go`, per CLAUDE.md's "tests
colocated with the code" convention - pure relocation, no behavior
change, `keyTransitionWarnings`' own tests keep passing unchanged.

**What's genuinely new** (not just a move), needed by Phase 2's
transposition step:

- `SignedSemitoneDelta(fromPC, toPC int) int` - the minimal **signed**
  shift (range `-6..6`) to move from one pitch class to another. The
  existing `SemitoneDistance` only ever answered "how far" (unsigned
  magnitude, since the warning only cares whether two keys are a
  half-step/tritone apart); transposing a stored range needs "which
  direction and how far" to actually shift a number, which nothing today
  computes.
- `ParseNote(s string) (pitchClass, octave int, ok bool)` and
  `AbsoluteSemitone(pitchClass, octave int) int` - the octave-aware
  primitives person range parsing/storage need, extending
  `ParseKeyPitchClass`'s letter+accidental parsing (which it reuses
  internally) with a trailing octave digit. `ParseKeyPitchClass` itself
  stays octave-less and unchanged - key *names* ("G", "F#m") never carry
  an octave in this app's existing data (PCO's own key picker doesn't
  offer one), so this is additive, not a signature change to existing
  callers.

**Explicitly NOT moved**: `docs/plans/shipped/key-change-tip-plan.md`'s
dominant-seventh spelling table/`suggestedDominantSeventh` helpers (now
built, `sets_builder.go`-local per that plan's own recommendation, since
their need is "transient and render-time only," not storage-shaped) stay
put. This plan's key engine only takes over the pitch-class parsing/
distance primitives - flag this explicitly so a future implementer
building this plan doesn't assume the whole file's music-theory surface
moves together.

## Phase 2: song vocal range

**Storage shape**: an interval from a reference key, per song - a bare
`(low, high)` note pair would be meaningless without knowing what key it
was measured against (the whole point of "relative to key" in the
bullet's own title). New migration (numbered `00036_song_vocal_range.sql`
if it directly follows Phase 1's migration, otherwise the next available
number at build time):

```sql
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
```

One row per song per org (org-scoped, not shared vocabulary - a
manually-entered range is still one org's own input, the same way
`song_tags`/`song_roles` are org-scoped despite `song_id` itself being a
shared PCO identifier). `reference_key` is stored alongside the offsets
so the entry can be re-derived/audited later, even though only the
offsets are used at transposition time.

**What entering a range concretely means**: the director picks two real
notes (same note-input UI as Phase 1) for the song's lowest and highest
sung notes while typing in the reference key it was measured against
(text input reusing the same `musicalKeyOptions`-style datalist key entry
already uses elsewhere). The controller then anchors the reference key's
tonic to a real octave and computes both offsets as non-negative
integers relative to it:

```
tonicAbs   = AbsoluteSemitone(ParseKeyPitchClass(referenceKey), lowNote.Octave)
if tonicAbs > lowNote.AbsoluteSemitone():
    tonicAbs -= 12   // step down one octave so the tonic sits at or below the low note
lowOffset  = lowNote.AbsoluteSemitone()  - tonicAbs   // >= 0 by construction
highOffset = highNote.AbsoluteSemitone() - tonicAbs   // >= lowOffset
```

This anchoring rule ("the tonic occurrence at or immediately below the
low note") is the one genuinely new piece of music-theory bookkeeping
this phase needs beyond the key engine's raw primitives - worth stating
precisely (mirroring how `key-change-tip-plan.md` worked through its own
enharmonic-spelling edge case explicitly rather than leaving it implicit)
so a future implementer doesn't have to re-derive it from scratch.

**Where entered**: a dedicated setup page, `/scheduling/song-vocal-range`,
mirroring the Song Tags/Song Roles setup-page pattern (song `<select>` +
reference-key input + low/high note inputs + a table of existing
entries) - **not** embedded in the Build page's `song_detail_card`. This
is a deliberate difference from Phase 1's own setup-page placement
reasoning stated for symmetry: a song's range is a fact about the song
itself, reused across every plan that song ever appears in (like
`song_tags`/`song_roles`, keyed by `song_id` alone), not a per-occurrence
value tied to one set - entering it on the Build page would mean
re-entering it every time the song is added to a new plan, which is
exactly the trap `song_tags`/`song_roles` already avoid by living on
their own setup pages. The Build page only ever *reads* the transposed
result (next paragraph), the same way it reads tags/roles/preferences
today rather than editing them inline.

`docs/plans/shipped/song-tags-setup-redesign-plan.md` has since shipped,
but its decided scope didn't need the `SongPickerField`/song-search
endpoint it had originally proposed (every song card renders
unconditionally on that page now, so there was nothing left for a picker
to narrow down) - so there's still no reusable searchable song picker
anywhere in the codebase. This page should keep using the same plain
`<select>` every other setup page uses today, unless some other page
builds a real one first.

**Transposition to the arrangement's actual current key**: happens at
Build-page render time, in `buildSetBuilderData`
(`internal/server/controllers/sets_builder.go`) - the same place
`effectiveKey`/`keyTransitionWarnings` already run, for the same reason
(a song's *effective* key already accounts for a local override falling
back to the cached arrangement default, and that's the key the range
must be transposed into, not whatever `reference_key` happened to be
when the range was entered):

```
delta        = scheduling.SignedSemitoneDelta(referenceKeyPitchClass, effectiveKeyPitchClass)
effectiveLow  = tonicAbs + delta + lowOffset
effectiveHigh = tonicAbs + delta + highOffset
```

producing the song's real practical range in whatever key the
arrangement is actually in right now, expressed as the same absolute
semitone units Phase 1's person ranges already use - directly comparable
without any further conversion.

## Crowdsourced default: needs a real decision, not a default

Per this task's own instruction (and `docs/ideas.md`'s own explicit flag
- "the priciest single item in this file"), this section deliberately
does **not** pick an answer. Following the phrasing convention
`docs/plans/in-progress/live-sync-reflection-plan.md` uses for its own
unresolved mechanism questions:

1. **Join key coverage, not just existence.** `SongData.Attributes.CCLINumber`
   is a real, already-wrapped field (confirmed in `go-pco/songs.go`
   during this research pass), so a reliable cross-org join key
   *exists* - this isn't the open question `docs/ideas.md` implies it
   might be. What's genuinely unknown: what fraction of songs across
   real orgs actually have a CCLI number filled in (originals,
   public-domain hymns, and orgs that simply never populated the field
   would have none), and whether a title/CCLI hybrid fallback is worth
   the collision risk `docs/ideas.md` already warns against. This is an
   empirical question this app cannot answer without either the feature
   already existing (chicken-and-egg) or a manual sample of real
   customer data - not something to guess a percentage for here.
2. **Consent/opt-in model.** Per-org toggle (default off, or default on
   with an easy opt-out)? Consent to contribute *and* consent to
   receive suggestions could plausibly be two separate settings (an org
   might want the benefit without contributing, which raises its own
   fairness question). Whatever the shape, `docs/ideas.md`'s own framing
   constraint applies: never attributable ("Grace Church set this"),
   only an aggregate ("this looks like the range") - that constrains the
   schema (no org-identifying column ever exposed in a read path) but
   doesn't by itself answer *when* a value is aggregated in vs. held
   back pending consent.
3. **Data-quality/moderation.** A single bad entry (a typo, a range
   entered against the wrong reference key, someone testing the feature
   with junk data) poisoning the cross-org aggregate for every other org
   is a real risk with no existing analog in this codebase - nothing
   today aggregates one org's input for another org's benefit. Needs an
   actual decision on at least: a minimum sample size before a
   suggestion is shown at all, some outlier-rejection rule (median
   rather than mean, trimming extreme entries), and whether an org can
   ever flag/report a bad aggregate they're seeing.
4. **New tenancy shape.** Per CLAUDE.md's Organization scoping section,
   "everything today is org-scoped... aside from a couple of
   deliberately shared vocabulary tables" (fixed lookup tables like
   `song_relationship_types`, not live data written by one org and read
   by another). A crowdsourced aggregate is neither - it's org-*written*
   data read cross-org, a genuinely new shape needing its own security
   review (can one org's query ever be shaped to leak another org's raw
   per-org value rather than the aggregate alone?), not just a new
   table.

**Conclusion**: not ready to build without a product decision on at
least (1)'s real-world coverage and (2)'s consent model - (3) and (4) are
buildable once those two are settled. Phase 2 (manual per-org song range
entry) ships and is fully useful on its own regardless of whether this
ever gets built; crowdsourcing is an optional accelerant for filling in
otherwise-blank songs, never a blocker for the rest of this plan.

## Phase 3: mismatch warning UI, and the eventual matrix view

**Where it surfaces**: the Build page's Songs tab, per-song row - the
same `components.Disclosure` surface `keyTransitionWarnings` already
uses (`songBadgeSpans`/`set_builder.templ`), not a new UI pattern. A new
short badge (e.g. "range mismatch") appears on a song's row when a
vocalist assigned to that plan's roster has a stored range that doesn't
cover the song's effective transposed range (Phase 2); the click-to-
expand popover states who and by how much (e.g. "Sarah's range is
A3-D5; this song's current key needs her to reach F5"), mirroring
`keyTransitionMenu`'s one-sentence style.

**Who counts as "a vocalist to check"**: rather than a brittle
team/position-name heuristic (team and position names are org-defined
text, not a fixed enum this app can pattern-match reliably), this plan's
concrete answer is **anyone assigned to any position on that plan's
roster who also has a `person_vocal_ranges` row on file** - range data
only exists for people a director bothered to enter one for in the first
place, which is a reasonable proxy for "this is a vocalist," without this
app needing to know or guess which teams/positions are vocal ones. This
mirrors how `RepeatWarnings`/`ExistingConflicts` already cross-reference
roster and set data together in the same family of functions
(`sets_builder.go`/`rosters_candidate_warnings.go`) - this warning is
one more member of that family, not new plumbing.

**Severity/threshold** (left as a smaller open question, not a blocker):
whether a mismatch by one note at the edge of a range should warn as
loudly as a mismatch by a fifth is a UX-tuning question, not a data
question - propose defaulting to "warn on any note outside the stored
range at all" for a first pass (simplest, matches the key-transition
warning's own all-or-nothing trigger), leaving room to add a tolerance
band later if it proves noisy in practice, the same way
`key-change-tip-plan.md` left its own interval-widening question open
for later tuning rather than guessing a threshold up front.

**Octave-shift tip (register differences)**: a mismatch shouldn't stop at
a flat "doesn't fit" - the whole reason ranges are stored as absolute
semitones rather than bare pitch classes is that a tenor's and an alto's
ranges can share the same pitch classes a full octave apart, and the real
fix for a lot of mismatches is "sing it an octave down/up," not "change
the key." Before rendering a hard mismatch, also check the song's
effective range shifted by ±12 semitones against the person's stored
range - a pure derived comparison over values Phase 1/2 already produce,
no new storage. If a ±1-octave shift brings the song's range fully inside
the person's, downgrade the message from a mismatch warning to a tip
("fits if sung an octave lower/higher than written"), same `Disclosure`
surface, worded as a suggestion rather than a conflict; only render the
hard mismatch when neither the written octave nor a ±1-octave shift
works. This is the concrete answer to "what could make this person fit"
for the single most common real case (a register difference between
voice types), without a second warning mechanism.

**The matrix view**: a presentation-only page once both ranges exist,
cross-tabulating a team's people (rows) against a plan's songs (columns)
to show "who can sing what, in the key it's actually in" at a glance.
Genuinely new UI, not an extension of the Build page, and purely a read
over data Phases 1-2 already produce - no new storage, no new PCO calls.
Per CLAUDE.md's agent-delegation guidance, this is a strong future
candidate for a `general-purpose (isolation: worktree)` build once
Phases 1-2 are live ("a new read-only page" is one of the four builds
CLAUDE.md already cites as having run well that way) - but it's
deliberately not fully speced in this document, the same way this plan
defers the crowdsourced-default piece rather than guessing at its
design; it deserves its own short plan doc once there's real range data
to build the view against.

## Suggested build order

1. **Phase 1**: migration `00035_person_vocal_range.sql`,
   `internal/scheduling/person_vocal_range.go`,
   `internal/server/controllers/vocal_range.go`,
   `internal/views/scheduling/vocal_range.templ`, nav link. Fully
   standalone - ships and is useful with zero song data.
2. **Key engine extraction**: move `ParseKeyPitchClass`/`SemitoneDistance`
   into `internal/scheduling/key_engine.go`, add `SignedSemitoneDelta`/
   `ParseNote`/`AbsoluteSemitone`, update `sets_builder.go`'s caller,
   relocate the existing pitch-class tests. Pure refactor plus new pure
   helpers - no behavior change to the existing key-transition warning,
   verifiable by its existing tests passing unchanged.
3. **Phase 2**: migration for `song_vocal_ranges`,
   `internal/scheduling/song_vocal_range.go`, a song-vocal-range setup
   page, transposition wired into `buildSetBuilderData` using the key
   engine from step 2.
4. **Phase 3's warning**: the roster-cross-referenced mismatch check and
   its `Disclosure` badge on the Songs tab.
5. **Crowdsourced default**: not scheduled - blocked on the product
   decisions in "Crowdsourced default" above.
6. **Matrix view**: a separate future plan doc, once steps 1-4 are live.

## Verification (once built)

- `go build ./...` (empty output = success) after each phase.
- `make test` - new table-driven tests for `ParseNote`/`AbsoluteSemitone`/
  `SignedSemitoneDelta` (key engine), the reference-key anchoring formula
  (a handful of worked examples, including a low note that sits below the
  reference tonic), and the mismatch-detection logic (a range that fits,
  one that doesn't, a vocalist with no range on record skipped
  cleanly); existing `TestParseKeyPitchClass*`/`TestSemitoneDistanceWrapsAround`/
  `TestKeyTransitionWarnings*` tests keep passing unchanged after the
  Phase-2-prerequisite move.
- Live: enter a real person's range, enter a real song's range against a
  known reference key, confirm the Build page's badge fires correctly
  when that person is on the roster and the effective key pushes them
  out of range, and stays silent when it doesn't or when no range is on
  file for anyone assigned.
- Confirm no new PCO call was added anywhere in Phases 1-3 (this is
  entirely local-DB storage plus local math over data already fetched
  for other reasons) - `docs/pco-api-risks.md` needs no update for
  those phases. The crowdsourced-default piece, if it's ever built, is
  the one part of this plan that *would* need a `docs/pco-api-risks.md`
  entry (new cross-org query pattern) and a real security review, per
  its own open questions above.
