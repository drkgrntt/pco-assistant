# Plan: PCO-Themes- and lyric-derived song tag suggestions

**Status: Phase 1 shipped 2026-09-11; Phase 2 spun out, not built.**
Phase 1 (PCO Themes-based suggestions) shipped 2026-09-11 -
`scheduling.SongThemeSuggestions`/`SongThemeValues`
(`internal/scheduling/song_tag_suggestions.go`), a shared grouping helper
refactored out of the existing title-word suggestions, a third labeled
section ("From Planning Center themes") on what was then
`/scheduling/song-tag-suggestions`, and the extended nav badge check - see
`docs/pco-api-risks.md`'s Changelog entry for the zero-new-PCO-calls
detail. Phase 2 (lyric-derived suggestions - extending
`song_arrangement_cache` with `Lyrics`/`LyricsEnabled`, section-label
stripping, a new `SongLyricStopwords` list, a document-frequency ceiling)
was never built and has been spun out to its own doc,
`docs/plans/not-started/song-lyric-tag-suggestions-plan.md` - see that doc
for its design content. This doc's own "Live PCO findings"/"Decisions"/
Phase 2 "Resolved architecture" sections below are kept here unchanged,
for the full build history they were spun out of - same convention as
`docs/plans/shipped/vocal-range-plan.md`.

**Stale-doc note (found 2026-09-15, while doing this split):** this doc's
Phase 1 "UX" bullet and Phase 2's "UX integration" section below both
describe extending the standalone `/scheduling/song-tag-suggestions` page
- that page no longer exists. `docs/plans/shipped/
song-tags-suggestions-merge-plan.md` shipped later the same day
(2026-09-11) and merged both suggestion sources into a dismissible
"Suggestions" panel (`SongTagSuggestionsPanel`,
`internal/views/scheduling/song_tags.templ`) on `/scheduling/song-tags`
instead - that merge plan's own "Docs" section (item 11) explicitly
flagged this doc as needing an update to match, which never actually
happened until now. The content below is left as originally written for
history; `docs/plans/not-started/song-lyric-tag-suggestions-plan.md`'s own
"UX integration" section describes the real, current panel instead.

Written 2026-09-10 as a lyrics-only plan, prompted by revisiting
`docs/ideas.md`'s "Sermon topic/passage matching" section's "Lyric-derived
tags" bullet now that the underlying data-availability question is settled:
`go-pco`'s `ArrangementAttributes.Lyrics` is real and already wrapped
(confirmed while scoping
`docs/plans/shipped/song-lead-sheet-attachments-plan.md`). Revised
2026-09-11 after live verification against the dev PCO org (org 14485,
"New Life Church," 168 active songs) surfaced a second, higher-precision,
near-free signal source (PCO's own `Themes` field) and grounded every
previously-open mechanism question in real data instead of guesses - see
"Live PCO findings (2026-09-11)" below. Same spirit as
`docs/plans/shipped/song-lead-sheet-attachments-plan.md`/
`docs/plans/shipped/song-tags-setup-redesign-plan.md`.

**Scope note, read first**: the original version of this doc deliberately
left the extraction mechanism undecided - see the old "Open questions"
list, now superseded by "Decisions (resolved 2026-09-11)" below. Those are
now resolved, informed by a real sample pulled live from the dev org
rather than assumptions. Everything else (where the text comes from,
how/where it's cached, how a suggestion gets in front of a human, how it's
stored once accepted, org scoping) was already resolved concretely in the
original pass, because none of it depended on which extraction mechanism
got picked.

## Current state

### Song tags (`song_tag_categories`/`song_tags`, migration `00016`,
`internal/scheduling/song_tags.go`)

`song_tag_categories` is a shared-vocabulary lookup (`theme`/`scripture`/
`idea`/`seasonal` today, migrations `00016`/`00039`) - not org-scoped,
adding a category is just a row, no code change. `song_tags` is the actual
data: `(organization_id, song_id, category, value)`, org-scoped despite
`song_id` itself being a PCO identifier, because `song_id` isn't globally
unique across different orgs' separate PCO accounts - migration `00041`'s
own comment states this explicitly ("org-scoped, not shared vocabulary...
the same way `song_tags`/`song_roles` are org-scoped despite `song_id`
itself being a shared PCO identifier"). `CreateSongTag`/`UpdateSongTag`/
`DeleteSongTag`/`ListSongTags`/`DistinctSongTagValues` are the whole
surface - there's no "manually applied" vs. "suggested" flag anywhere in
this schema, and no need for one (see "Storage" below).

### There is already a shipped lyric-adjacent suggestion feature - title-word tag suggestions

This is the single most important piece of prior art for this plan, and it
already answers several of the "how would this even work" mechanical
questions:

- `internal/scheduling/song_tag_suggestions.go`: `SongTitleWords` splits a
  song title into significant words (lowercased, stopword-filtered,
  length-filtered, deduped per title) using a **fixed, hand-picked
  stopword list** (`SongTitleStopwords`), explicitly "per the 'no AI'
  decision it was designed against" (that function's own doc comment).
  `SongTagSuggestions` groups songs sharing a significant title word (2+
  songs per group), skips already-tagged songs/ignored words, and returns
  `SongTagSuggestionGroup` values.
- `ignored_song_tag_suggestions` (migration `00022`) is a permanent per-org
  "don't suggest this word again" dismiss list, keyed by
  `(organization_id, word)` with `word` a plain unconstrained `TEXT`
  column - no un-ignore UI. Confirmed (2026-09-11, reading the migration
  directly) that `word` isn't limited to a single token, so it's reusable
  as-is for a multi-word Theme phrase used as an ignore key (see Phase 1
  below), not just single lyric words.
- `internal/server/controllers/song_tag_suggestions.go` +
  `internal/views/scheduling/song_tag_suggestions.templ`
  (`/scheduling/song-tag-suggestions` page, registered in
  `scheduling.go`): renders each group as a `components.Card` with a
  checklist of not-yet-tagged songs, a category/value form, an "Apply to
  selected" button (`ApplySongTagSuggestion` -> plain `CreateSongTag`
  calls, one per selected song), and a separate "Not a theme - ignore"
  button (`IgnoreSongTagSuggestion` -> writes to
  `ignored_song_tag_suggestions`). The whole list re-renders after either
  action via htmx (`hx-target="#song-tag-suggestions-list"`).
- `internal/server/middleware/nav.go`'s `PopulateNavState` computes
  `views.NavSuggestionsPendingKey` for the nav badge by calling
  `scheduling.SongTagSuggestions` live (fed from the already-60s-cached
  `scheduling.FetchActiveSongs` plus `ListSongTags`/
  `ListIgnoredSongTagSuggestionWords`), cached itself for `navStateTTL`.

Crucially: **once a suggestion is applied, it's just a `song_tags` row -
indistinguishable from a manually-added tag.** The "suggestion" only ever
exists as a computed, ephemeral in-memory grouping over already-cached
data; nothing about a suggestion is itself ever persisted except the
ignore-list. This resolves this plan's own "does storage need a
suggested-vs-applied flag" question by precedent: no, it doesn't, for the
same reason the shipped feature didn't need one. This precedent holds
identically for both sources this plan now covers.

### The consuming end already works "for free"

`internal/server/controllers/sets_builder.go`'s `buildSetBuilderData` calls
`store.DistinctSongTagValues` to populate the "Browse by tag" chips
(`set_builder.templ`), and `candidateMatchesSearch` matches typed search
text against a song's tag values as well as its title - both read plain
`song_tags` rows with no awareness of where a tag came from. So a
Theme- or lyric-derived tag, once accepted through whatever UI this plan
builds, shows up in "Browse by tag" and candidate search identically to a
manual tag, with zero additional wiring - confirmed by reading both call
sites, not assumed.

### Lyric text is already being fetched and thrown away

`fetchAndCacheArrangementInfo`
(`internal/server/controllers/scheduling.go`) is the function that keeps
`song_arrangement_cache` (migration `00023`, extended by `00026`/`00027`/
`00034`) current - it calls `pco.GetArrangements(ctx, songID,
&pco.ArrangementsParams{PerPage: 1, Include: []string{"keys"}})` and reads
`Key`/`BPM`/`Meter`/`Length`/`Notes`/`Sequence`/`SequenceShort`/
`HasChordChart` off the response's `attrs` (`pco.ArrangementAttributes`).
That same struct already has `Lyrics string` and `LyricsEnabled bool` -
**this app already makes the PCO call that returns lyric text for every
song it caches arrangement info for; it just never reads those two
fields.** Adding lyric caching therefore adds **zero new PCO calls** - the
opposite of the usual "new feature, new rate-limit risk" shape this
codebase is normally wary of.

This function is called from three places, all of which already cover the
*entire* active song library over time, not just songs added to a set:

1. `resolveArrangementCacheInfo`'s cache-miss path (queues via
   `enqueueSongSync`, drained in paced batches by
   `runSongInfoQueueSync`/`triggerSongInfoQueueSync`) - triggered whenever
   a song is actually used in a set builder render.
2. `RefreshSetSongArrangementInfo` - a manual, single-song, synchronous
   "Update from Planning Center" button.
3. `triggerSongInfoBackgroundSync`/`RunOrgBackgroundSync`
   (`internal/server/controllers/pco_background_sync.go`, called by
   `jobs.PCOJobs` per `docs/plans/shipped/pco-background-sync-plan.md`) -
   a per-org cron pass that diffs `scheduling.FetchActiveSongs` against
   `store.SongArrangementInfo` and enqueues *every* active song missing a
   cached row, draining through the same paced queue. This is what
   guarantees eventual full-library coverage even for songs nobody has
   ever added to a set - important, because the lyric suggestion page's
   "whole library" scope depends on the equivalent lyric cache also
   eventually covering the whole library, not just set-touched songs.

### Lyrics availability isn't guaranteed

`docs/plans/shipped/song-lead-sheet-attachments-plan.md`'s "Why automatic
range extraction is out of scope" section already establishes that
`Arrangement.Lyrics`/`ChordChart` are **plain text fields, not required by
PCO** - a song can have `LyricsEnabled: false`, or `LyricsEnabled: true`
with an empty `Lyrics` string if nobody ever pasted lyrics in. Confirmed
live (2026-09-11, see below): of 168 active songs, 162 have
`lyrics_enabled = true` but only 93 have actual non-empty `Lyrics` text.
Any lyric-derived mechanism has to treat "no usable lyric text" as a
normal, frequent case to skip, not an error.

## Live PCO findings (2026-09-11)

Pulled directly from the dev org (org 14485, "New Life Church") via a
session access token from the local `sessions` table, per CLAUDE.md's
"verify live against the signed-in dev PCO account" guidance - this
section replaces what would otherwise have been guesswork in "Decisions"
below. 168 active (non-hidden) songs total.

### Finding 1: PCO's own `Themes` field is a second, materially free signal source

`go-pco`'s `SongAttributes.Themes` (`songs.go`) is a plain string already
returned by the exact same `services/v2/songs` call
`scheduling.FetchActiveSongs` already makes and already caches for 60s -
reading it costs **zero new PCO calls, zero new caching, not even a
migration**, just using a field of the struct `FetchActiveSongs` already
returns today.

78 of 168 songs (46%) have a non-empty `Themes` value - real CCLI/PCO-
curated theological vocabulary, comma-separated, e.g.:

- "How Great Is Our God" -> `Kingship, Majesty / God's Attributes, Praise,
  Worthiness / God's Attributes`
- "Break Every Chain" -> `Atonement, Freedom, Jesus' name, Power,
  Redemption, Sacrifice`
- "Jesus Paid It All" -> `Crucifixion, Jesus, Reconciliation, Redemption,
  Sacrifice`

This is materially higher-precision than anything a word-frequency
approach over lyric text could produce (specific theological categories
like "Second Coming," "Reconciliation" - not derivable from raw word
counting), and dramatically cheaper to build than the lyrics path (no
migration, no cache extension, no cleanup/filtering logic - see Phase 1
below).

### Finding 2: coverage overlap between Themes and lyric text

Cross-referencing the same 168 songs' `Themes` and cached `Lyrics`:

| | count | % of library |
|---|---|---|
| Both Themes and usable lyric text | 41 | 24% |
| Lyrics only (Themes empty) | 52 | 31% |
| Themes only (no usable lyric text) | 37 | 22% |
| Neither | 38 | 23% |

Lyrics genuinely adds new coverage beyond Themes (52 songs), which is why
this plan keeps both sources rather than dropping lyrics now that Themes
exists - but no single source covers the whole library; 23% of songs have
neither signal, an expected/tolerated gap, not a bug to chase.

### Finding 3: raw `Lyrics` text embeds PCO's own structural section labels - a required cleaning step, not an optional one

Sampling all 93 songs with non-empty `Lyrics` text: the field isn't pure
lyric content. PCO's lyric editor stores structural markers as literal
lines within the same text - `"Chorus 1"`, `"Verse 1"`, `"Misc 1"`,
`"(Ending)"`, `"(REPEAT)"`, etc. Real example (`"Break Every Chain"`):

```
"Chorus 1\n\nThere is power in the name of Jesus\n...\nVerse 1\n\n..."
```

Left in, these dominate any naive word-frequency count: before stripping,
the most common "lyric" words across the sample were `verse` (100% of
songs), `chorus` (99%), `misc` (83%), `bridge` (75%) - a naive port of
`SongTitleWords` over raw `Lyrics` text would suggest tagging nearly the
entire library "chorus." **Stripping these is a required first pass for
any lyric extraction, not a refinement to consider later.**

### Finding 4: even after cleanup, generic worship vocabulary dominates - confirms the need for a frequency ceiling, not just a stopword list

After stripping section labels and applying the existing
`SongTitleStopwords` list, document frequency across the same 93-song
sample:

| word | % of songs |
|---|---|
| love | 56% |
| jesus | 49% |
| god | 48% |
| name / sing / life / heart / praise | 33-40% |
| lord / power / hope | 24-30% |

This confirms, with real numbers, the concern raised in discussion before
pulling any data: `SongTitleWords`' existing "shared by 2+ songs" floor
alone would produce suggestion groups spanning roughly half the library
for words like these - not useful.

The distribution has a clear knee: 22 words sit at >=25% document
frequency, 41 more at 15-25%, then it drops off sharply. Words in the
2-7-song range are markedly more specific and genuinely tag-worthy -
sampled directly from real results: `hallelujah`, `lamb`, `reigns`,
`washed`, `promise`, `paid`, `debt`, `faith`, `trust`, `peace`.

Even in that low-frequency tail, generic filler still leaks through
(`them`, `other`, `within`, `yeah`, `may`, `began`, `left`, `say`, `i'm`,
`there's`) that `SongTitleStopwords` - built for short titles, not full
lyric text - was never designed to catch. Confirms lyrics need their own,
separate stopword list rather than extending the title one: extending the
shared list risks regressing the shipped, already-tuned title-word
feature with lyrics-specific noise it doesn't need.

## Decisions (resolved 2026-09-11, supersedes "Open questions" below)

### Build order: Themes first (Phase 1), lyrics second (Phase 2)

Themes needs no migration, no cache extension, no cleanup/filtering logic
- it's a comma-split over a field the app already holds in memory. Lyrics
needs the arrangement-cache extension, section-label stripping, a
frequency ceiling, and a new stopword list (all below). Ship Themes first
as a fast, high-precision win; lyrics second as the higher-effort pass
that covers the 31% of the library (52/168 in the live sample) Themes
alone doesn't reach.

### A. Granularity/taxonomy

Both sources are freeform, not a curated topic dictionary and not
scripture-reference detection (left to `docs/ideas.md`'s separate,
not-yet-built "Scripture reference as a structured field" idea, per the
original doc's note distinguishing the two). Themes: each comma-separated
segment of PCO's `Themes` string is a candidate tag value close to as-is
(trim whitespace; drop empty segments - e.g. "How He Loves" ->
`", Father, Glory, ..."` has a leading empty segment to skip). Lyrics:
single significant words, same shape as `SongTitleWords` - not phrase/
n-gram extraction. Phrase extraction (e.g. a repeated chorus line like
"amazing grace") would likely be higher-signal/lower-noise, but is real
new engineering (n-gram counting, not a rename of existing set
operations) - noted as a future enhancement, not built now.

### B. Mechanism - naive, non-AI, both sources

No AI, for both sources: consistent with `SongTitleWords`' existing "no
AI" precedent, and matches the explicit reasoning behind that call -
real per-run cost that doesn't scale to a whole-library batch job, and AI
being polarizing enough that if it's ever used here it should be an
opt-in, clearly-labeled, separate thing, not a default swapped in
underneath an existing feature.

Forward-compatibility for a possible future AI source: not an interface/
strategy abstraction (speculative generality for what would still be a
single non-AI implementation today) - instead, each suggestion group
carries a `Source` label (`"title_words"` / `"pco_themes"` /
`"lyrics_naive"`, future `"lyrics_ai"`), rendered as its own labeled
section on the existing Song Tag Suggestions page, each with its own
ignore-list table if its suggestion unit needs one. Adding an AI source
later is "write a new function, add a section," not a refactor of what
ships now. If ever built, it needs its own explicit org-level opt-in flag
(per the polarization concern above) and clear in-UI labeling that it's
AI-generated - noted for whenever that's actually revisited, not scoped
by this plan.

(Phase 1 note: shipped without a literal `Source` field/type on
`SongTagSuggestionGroup` - the two sources are distinguished structurally
instead, by which of `SongTagSuggestionsData`'s two slices
(`Suggestions`/`ThemeSuggestions`) a group is in and which labeled page
section renders it. Same effect - "add a source, add a section" - without
adding a field nothing yet reads.)

### C. Confidence/review model - human-gated, both sources

Matches the house style already established by title words (explicit
Apply click) and the Publish/Discard sync model generally (never push a
change without a deliberate action). No reason to deviate surfaced for
either source.

### D. Where it runs - live on page-open, both sources

Themes: trivial - reads the already-60s-cached `FetchActiveSongs` result,
identical timing to title words today. Lyrics: reads already-durably-
cached `song_arrangement_cache` rows (the new columns from Phase 2 below)
- cheap, no PCO call, no cron/button needed, since the chosen mechanism is
free and instant rather than something with real per-run cost (which is
what would have forced a background/cron or manual-trigger design instead
- see the now-moot "Old open question D" discussion preserved below for
that reasoning).

### E. Lyrics/Themes-availability edge case

"No usable lyric text" and "no `Themes` value" are both expected, frequent,
silently-skipped cases for their respective source - not an error, and not
something that should look like "we checked and found nothing thematic."
The existing "row exists = checked successfully" convention
`song_arrangement_cache` already uses elsewhere handles the lyrics case
fine as long as the extraction step checks `LyricsEnabled`/`len(Lyrics) >
0` first; the Themes case just checks for a non-empty string.

### F. Lyrics extraction parameters - resolved, grounded in live data

- **Strip PCO structural section labels before any word extraction** - a
  line-level regex matching section names (verse/chorus/pre-chorus/
  bridge/tag/misc/interlude/intro/outro/ending/refrain/vamp/repeat/
  instrumental/breakdown/turnaround), optionally numbered, optionally
  parenthesized, optionally colon-terminated. Confirmed necessary and
  largely sufficient against the live sample (Finding 3 above); some
  inline directives like `"(REPEAT)"` embedded mid-line rather than on
  their own line may still slip through as a residual imperfection - low
  severity, since the frequency ceiling below would suppress a word like
  "repeat" anyway if it stays common enough to matter.
- **A new `SongLyricStopwords` list, separate from `SongTitleStopwords`**
  - starter set seeded directly from Finding 4: the >=25%-document-
  frequency words that are worship-common-but-uninformative in this
  domain (`god`, `jesus`, `lord`, `christ`, `praise`, `glory`,
  `hallelujah`, `worship`, `holy`, `sing`, `name`, `love`, `heart`,
  `life`, `come`, `will`, `one`, `now`, `has`, `have`, `what`, `who`,
  `every`, `let`, `here`, `there`, `through`, `never`, `like`, `still`,
  `where`, `could`) plus generic filler observed leaking through even the
  low-frequency tail (`them`, `other`, `within`, `yeah`, `may`, `began`,
  `left`, `say`, `i'm`, `there's`). Not exhaustive by design - the
  existing ignore-list mechanism is the standing pressure valve for
  whatever this starter list misses, same as it already is for title
  words; this list doesn't need to be perfect on day one.
- **A document-frequency ceiling on top of the stopword list**: a word
  qualifies for a suggestion group only if it appears in lyrics of no more
  than roughly 20-25% of the org's active songs with cached lyric text
  (exact threshold to tune once built against a real run, not hardcoded
  blindly from a single sample) - self-tunes per org's actual library
  composition (a Christmas/Easter-heavy library has a different "too
  common" set than one that isn't) rather than relying purely on a fixed
  list, and catches whatever the curated list didn't anticipate. The
  existing "shared by >= 2 songs" floor from the title-word feature stays
  unchanged as the lower bound.
- **Per-song word presence is deduped**, not frequency-counted within a
  song - directly mirrors `SongTitleWords`' existing per-title dedup, so a
  chorus repeated 4x doesn't inflate a word's weight within its own song.

## Resolved architecture

### Phase 1: PCO Themes-based suggestions - shipped 2026-09-11

1. **Fetch**: none needed. `pco.SongData.Attributes.Themes` is already on
   every element `scheduling.FetchActiveSongs` returns.
2. **Computation**: `scheduling.SongThemeSuggestions` (alongside the
   existing `SongTagSuggestions`) that, for each active song, splits
   `Themes` on `,`, trims each segment, drops empties (`SongThemeValues`),
   and groups songs sharing an exact (case-insensitive-normalized) theme
   value - same "2+ songs per group" floor as title words, same
   skip-already-tagged/skip-ignored filtering. Both `SongTagSuggestions`
   and `SongThemeSuggestions` now delegate to a shared, unexported
   `songTagSuggestionGroups` helper parameterized by an extractor
   (`wordsForSong`, `normalizedKey -> displayText`) so the grouping/sort/
   already-tagged logic isn't duplicated - the group's display `Word` is
   taken from the alphabetically-first (by title) song's own casing for
   that key, so PCO's original Theme casing (e.g. "God's Attributes") is
   preserved rather than lowercased and reconstituted.
3. **Storage**: none new. Accepting a suggestion is `CreateSongTag`
   exactly as today. Ignoring one writes to `ignored_song_tag_suggestions`
   exactly as today - confirmed schema-compatible with multi-word theme
   phrases as the ignore key (see "Current state" above).
4. **UX**: a second labeled section on the existing
   `/scheduling/song-tag-suggestions` page ("From Planning Center
   themes"), reusing the same Card/checklist/Apply/Ignore pattern
   (factored into a shared `songTagSuggestionGroupCard` templ component so
   the markup isn't duplicated between the two sections).
5. **Nav badge**: `middleware.PopulateNavState`'s pending-suggestions check
   now ORs in `scheduling.SongThemeSuggestions`' result alongside the
   existing title-word check - cheap, since `Themes` is already in the
   same cached `FetchActiveSongs` call the check already makes.

### Phase 2: lyric-derived suggestions - not yet built

#### 1. Extend the existing arrangement-info fetch, no new PCO call

In `fetchAndCacheArrangementInfo`
(`internal/server/controllers/scheduling.go`), read `attrs.Lyrics` and
`attrs.LyricsEnabled` alongside the fields already extracted, and pass
them through to `scheduling.SongArrangementInfo`. No new `pco.*` call, no
change to `songInfoSyncBatchPause`'s 2-calls-per-song math (still 2:
`GetArrangements` + `GetAttachments` via `fetchAndCacheSongAttachments`) -
this is the rare case where a feature adds real value with zero rate-limit
cost, worth calling out explicitly in `docs/pco-api-risks.md` for exactly
that reason (see "Docs" below).

#### 2. Storage: extend `song_arrangement_cache`, not a new table

Unlike the lead-sheet-attachments case (which needed a new
one-song-to-many-attachments table), lyrics are one-value-per-song - the
same shape every other field in `song_arrangement_cache` already has. New
migration `internal/database/migrations/00046_song_arrangement_lyrics.sql`
(confirmed next available number as of 2026-09-11 - re-confirm against
`internal/database/migrations/` before actually building this phase, since
Phase 1 added none but other migrations may have landed since):

```sql
-- +goose Up
ALTER TABLE song_arrangement_cache ADD COLUMN lyrics TEXT NOT NULL DEFAULT '';
ALTER TABLE song_arrangement_cache ADD COLUMN lyrics_enabled BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE song_arrangement_cache DROP COLUMN lyrics_enabled;
ALTER TABLE song_arrangement_cache DROP COLUMN lyrics;
```

`internal/scheduling/song_arrangement_cache.go`:
- Add `Lyrics string`/`LyricsEnabled bool` to `SongArrangementInfo`
  (doc-commented the same way `HasChordChart`/`Structure` already are -
  "zero value is legitimate, not 'not cached yet'").
- Extend `UpsertSongArrangementInfo`'s `INSERT`/`ON CONFLICT DO UPDATE`
  and `SongArrangementInfo`'s `SELECT`/`Scan` to carry the two new
  columns. Both are mechanical one-line-per-field additions to existing
  queries, not new methods.

Why extend this table rather than add a new `song_lyric_cache`: it's
already the exact "durable local Postgres cache for PCO-derived data that
doesn't change per-request" pattern this needs (per CLAUDE.md's own
framing of `song_arrangement_cache`), it's already org-scoped for the
right reason (`song_id` isn't globally unique across orgs), it's already
kept fresh by all three existing sync paths above with zero new plumbing,
and lyrics genuinely belong with the rest of a song's "cached Planning
Center summary" rather than as their own concern - there's no
"attachment-like" one-to-many relationship here that would justify a
separate table the way attachments needed one.

Why a durable local-DB cache rather than the in-process TTL cache
`FetchActiveSongs`/`song_arrangement_cache` are the two live conventions
in this app: `FetchActiveSongs`'s 60s in-process TTL cache fits data
that's cheap to refetch and needed live every render (titles, for the
song picker). Lyric text is a meaningfully larger payload than anything
else this app fetches per-song, essentially never changes once a song's
arrangement is set up, and the whole point of the suggestion page is to
read it back across the *entire* library repeatedly (recomputing
suggestions) - exactly `song_arrangement_cache`'s existing profile
(key/BPM/notes/structure), not `FetchActiveSongs`'s.

#### 3. Extraction: clean, filter, group - see "Decisions" F above for the concrete parameters

New function alongside `SongTitleWords` (e.g. `SongLyricWords`), applying,
in order: (a) strip PCO structural section labels line-by-line, (b) split
into words the same way `SongTitleWords` does (lowercased, non-letter/
digit boundaries, length >= 3, not all-digits), (c) filter against the new
`SongLyricStopwords`, (d) dedupe per song. `SongLyricSuggestions` (mirrors
`SongTagSuggestions`) then groups songs sharing a word across the library,
applying both the existing "shared by >= 2 songs" floor and the new
~20-25% document-frequency ceiling, skipping already-tagged songs/ignored
words exactly like the title-word path.

#### 4. Suggestion storage: no new table, no new flag - reuse `song_tags`/`CreateSongTag` exactly as-is

Per the title-word precedent, a suggestion is never itself a database row
- it's computed at read time from cached lyric text plus whatever this org
has already tagged/ignored. Accepting a suggestion is a plain
`CreateSongTag` call. `ignored_song_tag_suggestions` is directly reusable
as-is for lyric words (same single-token shape it already handles for
title words) - no new ignore table needed for this source.

#### 5. UX integration: extend the existing Song Tag Suggestions page

A third labeled section ("From lyrics") alongside title words and Themes
(Phase 1), same Card/checklist/Apply/Ignore pattern (reusing Phase 1's
`songTagSuggestionGroupCard` templ component). Unlike title words and
Themes (both available instantly from the already-cached
`FetchActiveSongs` result), lyric-derived groups depend on
`song_arrangement_cache` having actually synced each song's lyrics, which
happens asynchronously via the paced background queue/cron described
above. This page's data builder should treat a song with no cached
arrangement row (never synced) the same way `fetchAndCacheArrangementInfo`'s
callers already treat "song info not cached yet" elsewhere in this app -
the lyric-derived suggestion list is simply computed only from what's
cached at that moment, self-correcting as the background sync catches up
over the following minutes, not a blocking live fetch on page open. No
error state needed for this; it is the same "eventually consistent"
posture every other Song Info field already tolerates.

#### 6. Nav badge

Same extension as Phase 1, but - unlike Themes/title words - it needs a
local-DB read of whatever's currently cached in `song_arrangement_cache`
(cheap, no PCO call, fits within the same `navStateTTL` cache window)
rather than data already in-process. Makes the badge partial during a
fresh org's cold-start window (some songs' lyrics not synced yet) - worth
a doc comment, not a blocker.

### Org scoping (both phases)

No new decision needed: `song_arrangement_cache` (extended) and
`FetchActiveSongs`'s `Themes` reads both stay org-scoped exactly as they
are today, for the same reason `song_tags`/`song_vocal_ranges` are
(`song_id` isn't a globally unique identifier across different orgs'
separate PCO accounts - two orgs' same-valued `song_id` can be two
unrelated real songs). `song_tag_categories` stays shared vocabulary,
unaffected by this plan - both sources still target the existing `theme`
category by default.

### Docs

- `docs/pco-api-risks.md`: two short entries - Phase 1's `Themes` read
  (shipped, see the 2026-09-11 Changelog entry) and Phase 2's
  `fetchAndCacheArrangementInfo` field-set growth (`Lyrics`/
  `LyricsEnabled`, zero new calls, same response, previously-ignored
  fields, and a note that the cached payload per song is now meaningfully
  larger - a storage/row-size consideration, not a rate-limit one, still
  to be added once Phase 2 ships). No change to `songInfoSyncBatchPause`'s
  math from either phase.
- `README.md`: Phase 1's extended Song Tag Suggestions bullet shipped; add
  a lyrics mention once Phase 2 ships.
- `docs/ideas.md`: strike the "Lyric-derived tags" bullet under "Sermon
  topic/passage matching" with a pointer to wherever this actually lands,
  per CLAUDE.md's struck-item convention, once **both** phases ship (not
  before - Phase 1 alone doesn't fully deliver that bullet, since it's
  Themes-based, not lyric-derived) - worth a note there that the shipped
  feature also covers PCO's native `Themes` field, which wasn't part of
  the original brainstorm bullet.
- `CLAUDE.md`: likely no architectural-boundary change (this stays inside
  `internal/scheduling`/existing controllers/views).

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`.
- Phase 1 live: confirm a song with a real `Themes` value in PCO produces
  a suggestion group on `/scheduling/song-tag-suggestions`; confirm a song
  with an empty `Themes` value is silently skipped, not an error.
- Phase 2 live: confirm a song with real lyric text in PCO gets `lyrics`/
  `lyrics_enabled` populated in `song_arrangement_cache` after a sync
  (background cron, queue-drain, or the manual refresh button - whichever
  path is easiest to trigger in dev); confirm a song with no lyrics
  entered in PCO caches `lyrics_enabled = false`/`lyrics = ''` without
  erroring; confirm the Song Tag Suggestions page renders lyric-derived
  groups (or gracefully shows none) without a new PCO call - check
  `docs/pco-api-risks.md`'s claim of "zero new calls" against a real
  network trace, not just the code read; spot-check that section labels
  ("chorus," "verse," etc.) never appear as a suggested tag value, and
  that the frequency ceiling is actually filtering common words like
  "love"/"jesus"/"god" out of real suggestion groups against the live
  library, not just the 2026-09-11 sample.
