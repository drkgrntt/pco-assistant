# Plan: post-service debrief log / transition notes

**Status: shipped 2026-09-07.** Built per this doc's own "Suggested build
order," with its three "genuinely unresolved" open questions resolved as:
table name `song_transition_notes` (kept as drafted below); a note entered
against a pair is listed from **both** songs' own detail cards (the
"either side" query already supported this - the UI now actually surfaces
it that way); and "latest only," no history view, is sufficient for this
build (the append-only schema still supports adding one later without a
migration change). The song-picker dependency noted below never landed
(still not built elsewhere in this codebase) - the "into/out of" picker
uses a plain `<select>` of every active song, same as every other setup
page, per this doc's own fallback guidance. See
`internal/scheduling/song_transition_notes.go` (store),
`internal/server/controllers/transition_notes.go` (matching logic +
handlers), `internal/views/scheduling/song_detail_card.templ`'s
`transitionNotesSection`, `songBadgeSpans`' new badge
(`set_builder.templ`), and `run_sheet.templ`'s `talkingPointsFor`.

Written 2026-09-05, expanding `docs/ideas.md`'s "Run sheet (day-of output)"
section's **Post-service debrief log / transition notes** brainstorm bullet
into a real design. Not built yet. Distinct from the bullet immediately
above it in the same section - **Day-of ephemeral note** - which is a
single throwaway text field on the local `sets` row ("running 10 late,"
gone the moment the plan is done). This plan is the opposite in every way
that matters: persistent across plans, keyed to a *song* (or a specific
song-to-song transition) rather than to one plan's `sets` row, and meant
to resurface automatically the next time that song or pairing comes up -
not disposable.

The brainstorm bullet describes two related but distinct note kinds:

- **Debrief (retrospective)** - a short post-Sunday note capturing what
  happened, for next time.
- **Talking points (prospective)** - what to actually say bridging out of
  one song into another (or into/out of a specific song), carried into
  the service.

## Current state

Three existing pieces this plan sits next to, none of which already solve
what it needs:

- **`internal/scheduling/song_relationships.go`** - the only existing
  concept of "a relationship between two specific songs." `SongRelationship`
  stores `(organization_id, song_id_a, song_id_b, relationship_type, notes)`
  with a `UNIQUE (song_id_a, song_id_b, relationship_type)` constraint
  (migration `00001_create_song_relationships.sql`), and
  `relationship_type` is a fixed, admin-extensible vocabulary
  (`song_relationship_types`, migration `00015`) rather than free text.
  Critically, **the pair has no inherent direction**: `CreateSongRelationship`
  and `UpdateSongRelationship` both normalize `song_id_a`/`song_id_b` into
  canonical (lexicographically smaller-first) order before writing, with
  the doc comment explicitly stating why - "A flows well with B" and "B
  flows well with A" mean the same thing, so without canonicalization the
  same pair entered in the opposite order would silently create a
  redundant second row. This canonicalization is exactly wrong for
  transition notes: "the transition out of Gratitude into Communion felt
  abrupt" is not the same statement as "the transition out of Communion
  into Gratitude" - direction is the entire content of a transition note.
  So `song_relationships` is not a pair-identity table this plan can
  reuse as-is; its *shape* (two `song_id_*` TEXT columns, no FK to a local
  songs table since none exists - PCO is the source of truth for song
  data) is the right pattern to copy, but its canonical-ordering behavior
  must specifically **not** be copied.
- **`keyTransitionWarnings`** (`internal/server/controllers/sets_builder.go`)
  - the existing precedent for "recognize these two songs are adjacent
    again," and it confirms there is **no persistent identity for "this
    specific pair"** anywhere in the app today. It walks `rows []mergedRow`
    (the set's current, merged song+non-song order) and compares each
    song to the one immediately before it (`rows[i-1]` vs `rows[i]`),
    entirely recomputed from the set's live order on every call - no
    stored "these two are adjacent" record, no caching. This is the
    matching-logic precedent the brainstorm bullet's own last line is
    asking for, and it's a direct, reusable pattern: walk `ServiceOrder`
    the same way, but look up notes keyed to `(prev.SongID, cur.SongID)`
    instead of computing a key interval.
  - `Recommendations`/`computeRecommendations` (`internal/scheduling/sets.go`)
    is a different, weaker precedent worth ruling out explicitly: it's
    anchored only on the *last* song in the set (not general adjacency)
    and reuses `song_relationships`' undirected pairing, so it isn't a fit
    for a directional, any-adjacency-in-the-set note either.
- **Song-history tracking** (`internal/scheduling/new_songs.go`'s
  `TrackedNewSongs`/`PublishedSongAppearanceCounts`) - confirms this app
  already tracks *how many times* a song has been used locally (via
  distinct `plan_id` counts on `set_songs`), but tracks nothing about
  *what was said or noted* on any of those past occurrences. "The last
  time this pairing came up" for prefill purposes has to come from the
  new notes table itself (most recent row for that key), not from this
  existing appearance-count machinery - there's no existing note content
  to prefill from anywhere in the app today.
- **`plan_notes`** (migration `00032`, `internal/scheduling/plan_notes.go`)
  - the closest existing example of "a small persistent local note keyed
    to something PCO doesn't track," and a good model for the *simple*
    single-value-per-key case (`GetPlanNote`/`UpsertPlanNote`, one row per
    `(organization_id, service_type_id, plan_id)`, always overwritten in
    place). It's a weaker fit for this plan specifically because debrief
    notes are explicitly described as a *log* ("post-service debrief
    log"), not a single evolving value - see Schema below for why this
    plan's table doesn't just copy `plan_notes`' upsert-in-place shape
    wholesale.
- **`songDetailCard`** (`internal/views/scheduling/song_detail_card.templ`)
  - the sidebar card shown per-song on the Build page, already the home
    for a mix of PCO-backed per-category notes (`SongNoteView`, pushed
    straight to PCO) and this app's own per-occurrence `Description`
    field (local until Publish, tied to that one `set_songs` row, cleared
    on Discard/replace). Notably, `songDetailCard` disables **all** edit
    forms when `data.ReadOnly` is true (past plans render key/description
    as plain text, no forms at all) - see Proposed design below for why
    that guard needs a deliberate carve-out for debrief entry.
- **`RunSheetPage`/`RunSheet`** (`internal/server/controllers/run_sheet.go`,
  `internal/views/scheduling/run_sheet.templ`) - currently a strictly
  "pure view over `SetBuilderData`/`RosterBuilderData`, no new storage"
  page, by explicit design (see its own doc comment). This plan is the
  first thing that requires it to read genuinely new storage (the latest
  talking point per song/transition) - a deliberate, narrow exception,
  not a drift away from that framing; see Proposed design.

## Proposed design

### Schema: one table, directional pair key, append-only

```sql
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
```

(Next sequential migration number - not guessing an exact one here, since
other work may land first; the most recent migration at the time this was
written is `00034_song_arrangement_structure.sql`.)

Design decisions this encodes, and why:

- **One table, `kind` column** - matches the brainstorm bullet's own
  wording ("a kind so debrief and talking-point don't collide") rather
  than two tables. `kind` is a plain `CHECK` constraint, not a
  `song_relationship_types`-style admin-extensible lookup table: unlike
  relationship types, there will only ever be exactly these two kinds -
  nothing here needs an admin to be able to add a third.
- **Directional, non-canonicalized pair** - `song_id_a`/`song_id_b` are
  stored exactly in the order entered ("out of A, into B"), the opposite
  of `song_relationships`' canonical-ordering behavior. This is the single
  most important divergence from the existing pattern (see Current state)
  and is deliberate: a debrief about the Gratitude-into-Communion
  transition must never be shown as the same row as one about
  Communion-into-Gratitude.
- **`song_id_b` nullable, same table** - a single-song note ("into/out of
  a specific song," per the brainstorm's own talking-points bullet) is
  represented as a row with `song_id_b = NULL`, rather than a second table
  or a separate "scope" enum. Keeps one lookup shape (`song_id_a` +
  optional `song_id_b` + `kind`) for both the Build-page resurfacing query
  and the Run Sheet's talking-points query.
- **Append-only, no update-in-place, no unique constraint** - this is the
  one place this plan deliberately does *not* follow `plan_notes`'
  upsert-and-overwrite shape. Every new entry is a fresh `INSERT`; existing
  rows are never updated (only ever deleted, as a plain input-correction
  affordance mirroring `DeleteSongRelationship`). This single append-only
  shape serves both kinds, but they're *read* differently:
  - **Debrief** rows are read as a full chronological list for a given
    key - "post-service debrief **log**" is the brainstorm bullet's own
    word, and an accumulating journal (this transition has now been noted
    three separate times) is more useful than a single overwritten value.
  - **Talking-point** rows are read as "latest row for this key only" -
    behaviorally a single current value, the same as `plan_notes`, but
    stored the same append-only way as debrief rows rather than as a
    separate upsert path. This keeps the store code uniform (one
    `CreateNote`, one `ListNotes`) at the cost of the talking-points table
    slowly accumulating superseded rows over time - an acceptable trade
    for schema/code simplicity, since this table will never be large
    enough for that to matter (a handful of notes per song, not per-plan
    volume). Older talking-point rows stay queryable later as history if
    that turns out to be wanted (open question below), which an
    overwrite-in-place design would have foreclosed.
- **No FK to a local songs table** - matches `song_relationships`: PCO is
  the source of truth for song data, this app doesn't mirror a songs
  table locally, so `song_id_a`/`song_id_b` are plain `TEXT` PCO ids with
  no referential integrity beyond the app's own `fetchActiveSongs` lookups
  at render time (same as everywhere else in the codebase that stores a
  song id).
- **Naming** - `song_transition_notes` is not a fully comfortable name
  given that half its rows (`song_id_b IS NULL`) aren't transitions at
  all; flagged as an open question below rather than treated as settled.

Store methods, in a new `internal/scheduling/song_transition_notes.go`
(matching `song_relationships.go`'s per-concern-file convention):

- `CreateTransitionNote(ctx, organizationID, params) (TransitionNote, error)`
  - `params`: `Kind`, `SongIDA`, `SongIDB` (optional), `Note`.
- `ListTransitionNotesForKey(ctx, organizationID, songIDA, songIDB string, kind string) ([]TransitionNote, error)`
  - all rows for one exact key+kind, newest first - the debrief log view.
- `LatestTransitionNote(ctx, organizationID, songIDA, songIDB, kind string) (TransitionNote, bool, error)`
  - single most recent row - what the talking-points "current value" and
    the Run Sheet both actually read.
- `ListTransitionNotesForSong(ctx, organizationID, songID string) ([]TransitionNote, error)`
  - every note (either kind, single-song or as either side of a pair)
    touching one song - powers the song detail card's own "notes on
    file" listing, mirroring `ListSongRelationships`' "either side"
    query shape.
- `DeleteTransitionNote(ctx, organizationID, id int64) error` - mirrors
  `DeleteSongRelationship`.

### Resurfacing trigger: mirror `keyTransitionWarnings` exactly

A new pure function alongside `keyTransitionWarnings` in
`sets_builder.go` (or its own small file, following that file's existing
split), fed the same `[]mergedRow` and a map of notes keyed by
`(song_id_a, song_id_b)`/`(song_id_a, "")`:

- **Single-song notes** surface as soon as that song is anywhere in
  `ServiceOrder` - a simple membership check per row, no adjacency needed.
- **Pair notes** surface only once two songs actually land back-to-back in
  the set's current order - literally the same `rows[i-1]`/`rows[i]`
  walk `keyTransitionWarnings` already does, just keyed on song id pairs
  instead of key-interval math. Recomputed fresh on every render, same as
  `keyTransitionWarnings` - no caching, no persisted "this pair occurred"
  tracking needed anywhere else.

Only the **latest** row per key+kind is what surfaces in this resurfacing
banner (not the full debrief log) - matches the brainstorm's own "prefill/
suggest from whatever was used last time" framing, and keeps the banner
from growing unboundedly noisy for a song/pair debriefed many times. The
full log stays available on the song's own detail card for anyone who
wants to scroll back further.

This should land as new `SetBuilderData` fields (e.g.
`TransitionNotePrompts []string` or a small view struct carrying kind/key/
text/link-to-detail-card), rendered as a new banner section styled as
informational surfacing rather than a warning - it's history being handed
back to the director, not a problem being flagged, so it shouldn't share
`RepeatWarnings`/`ExistingConflicts`' warning styling. Wired the same way
those are: computed in `buildSetBuilderData`, pushed via the existing
OOB-refresh mechanism (`songWarningsOOB`'s sibling) any time the set order
or a note changes.

### Three distinct UI moments, not one form

The brainstorm bullet describes the two note *kinds*; in practice there
are three separate *moments* a director interacts with this data, on two
different pages:

1. **Writing a debrief** - after Sunday, from the Build page. Proposed
   home: a new subsection on `songDetailCard`, next to the existing
   per-occurrence `Description` field but clearly separated from it (this
   is persistent, cross-plan data, not a per-occurrence one) - a small
   add-entry form (kind toggle debrief/talking-point, an optional "into/
   out of" song picker reusing the existing song-search pattern from
   `docs/plans/not-started/song-tags-setup-redesign-plan.md`'s proposed
   `SongPickerField` if that's landed by then, or a plain `<select>`
   otherwise, plus a text field) and a list of that song's existing notes
   (via `ListTransitionNotesForSong`).

   **This needs a deliberate carve-out from `songDetailCard`'s
   `data.ReadOnly` guard.** Today, `data.ReadOnly` (true for past plans)
   disables every form on the card, showing key/description as plain text
   instead. But a debrief is, by definition, almost always written about a
   plan that already happened - the natural moment to write "the bridge
   ran long" is while looking at last Sunday's now-past, now-read-only
   plan on the Build page. Unlike `Description`/key overrides (which
   mutate this specific plan's draft, and correctly stay frozen once
   that plan is done), a transition note doesn't touch the plan's draft
   at all - it's keyed to the song(s), not to this `set_songs` row or
   this `plan_id` - so it's safe, and necessary, to keep the debrief-entry
   form active even when `data.ReadOnly` is true. This is a real,
   easy-to-miss exception worth calling out explicitly when building this,
   not something to discover as a bug later.
2. **Talking points surfacing** - the Run Sheet, printed/carried into the
   service. `RunSheetPage`/`RunSheet` need to additionally fetch the
   latest talking-point note (`LatestTransitionNote`) for each song in
   `ServiceOrder` and each adjacent pair, and render it inline - the same
   treatment `hasAnyNoteContent`/`row.Song.Notes` already gives PCO
   category notes, just from this new source. This is the one place this
   plan knowingly breaks `RunSheetPage`'s current "pure view, no new
   storage" framing (see Current state) - an intentional, narrow
   exception because the Run Sheet is specifically where a talking point
   needs to physically be for it to do any good (carried into the
   service), not a drift back toward the Run Sheet accumulating its own
   data-entry surface. The Run Sheet stays print-first and read-only;
   talking points are still only ever *written* on the Build page (moment
   1, same form/kind as debrief), never on the Run Sheet itself.
3. **A debrief/talking-point resurfacing as a prompt** - back on the Build
   page, the next time that song or pairing is drafted into a new set (see
   Resurfacing trigger above). This is a read-only banner, not an entry
   point - clicking through to add a *new* note for that key still happens
   via moment 1's form on the relevant song's detail card.

### Prefill vs. reference-only: recommend reference-only

For talking points specifically ("worth prefilling/suggesting from
whatever was used last time"), this plan recommends **showing the last
note as a read-only reference next to a blank new-entry field**, not
copying it forward verbatim as an editable starting point. Reasoning: a
copy-forward default risks silently carrying stale wording forward
unnoticed (the "still says what it said three months ago because nobody
touched it" failure mode) - a blank field forces an active choice each
time ("does this still apply, or has the transition changed"), with the
prior note sitting right next to it so nothing has to be reconstructed
from memory. This is a judgment call, not a forced conclusion from the
research above - worth confirming against how the person actually using
this wants to work before building it, but it's the direction this plan
recommends and the one the build order below assumes.

### Open questions (genuinely unresolved, not answered by this plan)

- **Table name** - `song_transition_notes` is imprecise for its
  single-song rows (see Schema). Alternatives (`song_notes`,
  `song_debrief_notes`) all have their own imprecision (`song_notes`
  especially risks confusion with the existing PCO-backed
  `SongNoteView`/`SongNoteFormFragment` per-category note concept, which
  is unrelated). Needs a real decision before writing the migration, not
  a guess.
- **Does talking-point history need its own "see past ones" view**, or is
  "latest only" genuinely sufficient? The append-only schema supports
  either without a migration change either way, so this can be deferred
  past the first build rather than blocking it.
- **Should a debrief or talking point entered against a *pair* also be
  reachable/listed from *both* songs' detail cards**, or only from
  whichever song's card the director happened to be looking at when they
  added it? `ListTransitionNotesForSong`'s "either side" query (mirroring
  `ListSongRelationships`) makes this free to support, but the UI/build
  order above only spells out entry from one song's card - worth deciding
  before building the detail-card subsection, not after.
- **Song-picker dependency** - moment 1's "into/out of" picker would
  ideally reuse a searchable song picker rather than a plain `<select>`
  of the whole library (same pain point `song-tags-setup-redesign-plan.md`
  documents for the setup pages). That plan isn't built yet; this one
  shouldn't block on it, but should reuse `SongPickerField` if it lands
  first rather than building a second one-off picker.

## Suggested build order

1. Migration + `internal/scheduling/song_transition_notes.go` Store
   methods (independent, low-risk, same shape as `song_relationships.go`).
2. Song detail card: notes subsection (list + add-entry form), including
   the explicit `data.ReadOnly` carve-out described above. This is the
   only way notes get written at all, so it has to land before anything
   downstream can be tested against real data.
3. Build-page resurfacing banner (new pure matching function next to
   `keyTransitionWarnings`, new `SetBuilderData` fields, OOB wiring).
4. Run Sheet: fetch + render latest talking points per song/pair.
5. Tests: a pure-logic unit test for the new adjacency-matching function
   (mirroring `computeAvoidPairings`'/`computeRecommendations`'s "pure
   logic pulled out, tested without a live DB" convention already used
   for `keyTransitionWarnings`' neighbors in `sets_builder.go`), plus a
   DB-backed test for the Store methods following
   `song_tags_db_test.go`'s `RUN_DB_TESTS=1` pattern.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`, plus `RUN_DB_TESTS=1 go test ./internal/scheduling/...` for
  the new Store-level tests.
- Live: add a debrief note on a song, confirm it appears in that song's
  detail-card list and, in a fresh set, no note prompt shows until that
  song is actually added. Add a pair transition note between two specific
  songs, confirm the resurfacing banner appears only once those two songs
  are placed truly back-to-back (not merely both present) in a new set,
  in the same directional order the note was entered against - the
  reverse order should not match. Add a talking-point note, confirm it
  shows up on the Run Sheet for the relevant song/transition, and confirm
  the Run Sheet itself still has no form to write one (write-only from the
  Build page, per the design above). Confirm a debrief entered against a
  past (`data.ReadOnly`) plan's song actually saves, despite that plan's
  other forms being disabled.
- **No `docs/pco-api-risks.md` update needed** - this feature is entirely
  local-database storage keyed to song ids already available from
  `fetchActiveSongs`/the set's own data; it makes no new PCO calls of any
  kind (same conclusion, same reasoning, as
  `song-tags-setup-redesign-plan.md`'s own Verification section).
