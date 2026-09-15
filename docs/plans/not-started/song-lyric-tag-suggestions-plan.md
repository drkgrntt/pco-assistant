# Plan: lyric-derived song tag suggestions

Spun out of `docs/plans/shipped/song-theme-and-lyric-tag-suggestions-plan.md`'s
own remaining unbuilt piece once that plan's Phase 1 (PCO Themes-based
suggestions) shipped 2026-09-11 - see that doc for the full build history/
context this builds on (the shipped title-word/Themes suggestion mechanics
this mirrors, and the org-scoping/storage precedent it reuses). Not
started. The content that follows is pulled forward from that plan's own
"Live PCO findings," "Decisions," and "Phase 2: lyric-derived suggestions"
sections, with one correction: the original's UX plan (below) described
extending the once-real standalone `/scheduling/song-tag-suggestions`
page. That page was deleted and merged into a dismissible panel
(`SongTagSuggestionsPanel`) on `/scheduling/song-tags` by
`docs/plans/shipped/song-tags-suggestions-merge-plan.md`, shipped later
the same day as Phase 1 - the "UX integration" section below describes the
panel as it actually exists today, not the page it originally described.

## Live PCO findings (2026-09-11)

Pulled directly from the dev org (org 14485, "New Life Church") via a
session access token from the local `sessions` table, per CLAUDE.md's
"verify live against the signed-in dev PCO account" guidance. 168 active
(non-hidden) songs total. Re-verify against the live org before actually
building this, rather than trusting these numbers are still current.

### Finding 1: PCO's own `Themes` field is a second, materially free signal source (context for why lyrics is still worth building)

78 of 168 songs (46%) have a non-empty `Themes` value - real CCLI/PCO-
curated theological vocabulary. Higher-precision than anything a
word-frequency approach over lyric text could produce, and Phase 1 already
ships it for free. Lyrics is still worth building because of Finding 2.

### Finding 2: coverage overlap between Themes and lyric text

Cross-referencing the same 168 songs' `Themes` and cached `Lyrics`:

| | count | % of library |
|---|---|---|
| Both Themes and usable lyric text | 41 | 24% |
| Lyrics only (Themes empty) | 52 | 31% |
| Themes only (no usable lyric text) | 37 | 22% |
| Neither | 38 | 23% |

Lyrics genuinely adds new coverage beyond Themes (52 songs) - this is why
this plan exists rather than stopping at Phase 1. No single source covers
the whole library; 23% of songs have neither signal, an expected/tolerated
gap, not a bug to chase.

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

This confirms, with real numbers, that `SongTitleWords`' existing "shared
by 2+ songs" floor alone would produce suggestion groups spanning roughly
half the library for words like these - not useful.

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

## Decisions (resolved 2026-09-11)

### Granularity/taxonomy

Freeform, not a curated topic dictionary and not scripture-reference
detection (left to `docs/ideas.md`'s separate, not-yet-built "Scripture
reference as a structured field" idea). Single significant words, same
shape as `SongTitleWords` - not phrase/n-gram extraction. Phrase
extraction (e.g. a repeated chorus line like "amazing grace") would likely
be higher-signal/lower-noise, but is real new engineering (n-gram
counting, not a rename of existing set operations) - noted as a future
enhancement, not built now.

### Mechanism - naive, non-AI

No AI: consistent with `SongTitleWords`' existing "no AI" precedent - real
per-run cost that doesn't scale to a whole-library batch job, and AI being
polarizing enough that if it's ever used here it should be an opt-in,
clearly-labeled, separate thing, not a default swapped in underneath an
existing feature. If an AI source is ever added later, it needs its own
explicit org-level opt-in flag and clear in-UI labeling that it's
AI-generated - noted for whenever that's actually revisited, not scoped by
this plan.

### Confidence/review model - human-gated

Matches the house style already established by title words and Themes
(explicit selection, nothing applied automatically) and the
Publish/Discard sync model generally (never push a change without a
deliberate action). No reason to deviate.

### Where it runs - live on page-open

Reads already-durably-cached `song_arrangement_cache` rows (the new
columns from "Storage" below) - cheap, no PCO call, no cron/button needed.

### Lyrics-availability edge case

"No usable lyric text" is an expected, frequent, silently-skipped case -
not an error, and not something that should look like "we checked and
found nothing thematic." The existing "row exists = checked successfully"
convention `song_arrangement_cache` already uses elsewhere handles this
fine as long as the extraction step checks `LyricsEnabled`/`len(Lyrics) >
0` first.

### Extraction parameters - resolved, grounded in live data

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

### 1. Extend the existing arrangement-info fetch, no new PCO call

In `fetchAndCacheArrangementInfo`
(`internal/server/controllers/scheduling.go`), read `attrs.Lyrics` and
`attrs.LyricsEnabled` alongside the fields already extracted, and pass
them through to `scheduling.SongArrangementInfo`. No new `pco.*` call, no
change to `songInfoSyncBatchPause`'s 2-calls-per-song math (still 2:
`GetArrangements` + `GetAttachments` via `fetchAndCacheSongAttachments`) -
this is the rare case where a feature adds real value with zero rate-limit
cost, worth calling out explicitly in `docs/pco-api-risks.md` for exactly
that reason (see "Docs" below).

### 2. Storage: extend `song_arrangement_cache`, not a new table

Unlike the lead-sheet-attachments case (which needed a new
one-song-to-many-attachments table), lyrics are one-value-per-song - the
same shape every other field in `song_arrangement_cache` already has. New
migration `internal/database/migrations/00046_song_arrangement_lyrics.sql`
(confirmed next available number as of 2026-09-15 - re-confirm against
`internal/database/migrations/` before actually building this, in case
other migrations land first):

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
kept fresh by all three existing sync paths (queue-drain on set-builder
use, the manual "Update from Planning Center" button, and the per-org
background-sync cron) with zero new plumbing, and lyrics genuinely belong
with the rest of a song's "cached Planning Center summary" rather than as
their own concern.

Why a durable local-DB cache rather than the in-process TTL cache
`FetchActiveSongs`/`song_arrangement_cache` are the two live conventions
in this app: `FetchActiveSongs`'s 60s in-process TTL cache fits data
that's cheap to refetch and needed live every render (titles, for the
song picker). Lyric text is a meaningfully larger payload than anything
else this app fetches per-song, essentially never changes once a song's
arrangement is set up, and the whole point of this feature is to read it
back across the *entire* library repeatedly (recomputing suggestions) -
exactly `song_arrangement_cache`'s existing profile (key/BPM/notes/
structure), not `FetchActiveSongs`'s.

### 3. Extraction: clean, filter, group - see "Decisions" above for the concrete parameters

New function alongside `SongTitleWords` (e.g. `SongLyricWords`), applying,
in order: (a) strip PCO structural section labels line-by-line, (b) split
into words the same way `SongTitleWords` does (lowercased, non-letter/
digit boundaries, length >= 3, not all-digits), (c) filter against the new
`SongLyricStopwords`, (d) dedupe per song. `SongLyricSuggestions` (mirrors
`SongTagSuggestions`/`SongThemeSuggestions`, likely sharing the same
`songTagSuggestionGroups` helper those two already delegate to) then
groups songs sharing a word across the library, applying both the
existing "shared by >= 2 songs" floor and the new ~20-25%
document-frequency ceiling, skipping already-tagged songs/ignored words
exactly like the other two sources.

### 4. Suggestion storage: no new table, no new flag - reuse `song_tags`/`ignored_song_tag_suggestions` exactly as-is

Per the title-word/Themes precedent, a suggestion is never itself a
database row - it's computed at read time from cached lyric text plus
whatever this org has already tagged/ignored. `ignored_song_tag_suggestions`
is directly reusable as-is for lyric words (same single-token shape it
already handles for title words) - no new ignore table needed. Whether a
selected suggestion becomes a `song_tags` row happens via whatever the
"UX integration" section below routes it through (today: the existing
bulk-add form's own submit, which calls `CreateSongTag` per selected
song) - see that section for the exact mechanism.

### 5. UX integration: extend the existing Suggestions panel on `/scheduling/song-tags`

**This section replaces the original plan's description of extending a
standalone suggestions page - that page was deleted; see this doc's own
opening note.** The real integration point is `SongTagSuggestionsPanel`
(`internal/views/scheduling/song_tags.templ`) and its `Panel Data` type
(`SongTagSuggestionsPanelData`, `song_tag_suggestion_types.go`), which
today carries `TitleGroups`/`ThemeGroups []SongTagSuggestionGroup`:

- Add a third field, `LyricGroups []SongTagSuggestionGroup`, computed the
  same way `TitleGroups`/`ThemeGroups` already are (`song_tags.go`'s
  `songTagSuggestions`/`songThemeSuggestions` helpers have a clear
  `songLyricSuggestions` counterpart to add).
- `songTagSuggestionsPanelBody` (`song_tags.templ`) gets a third `if
  len(data.LyricGroups) > 0` block, same `<h4>`/`songTagSuggestionRow`
  shape as the existing two ("From song titles" / "From Planning Center
  themes"), labeled "From lyrics".
- `songTagSuggestionsPanelHasPending` needs `|| len(data.LyricGroups) > 0`
  added to its OR chain (controls whether the panel opens expanded and
  whether the nav badge - see "Nav badge" below - considers anything
  pending).
- No new endpoint needed: `songTagSuggestionRow`'s existing "Select these
  songs" button (`applySongTagSuggestion`,
  `web/static/js/scheduling-song-tags-contexts.js`) is purely client-side
  - it pre-fills and opens the page's existing bulk-add form and checks
  the matching songs, and that form's own submit is what actually calls
  `CreateSongTag`. A lyric-derived group needs zero new JS to be
  selectable this way. The existing `POST
  /scheduling/song-tags/suggestions/ignore` endpoint (takes a plain
  `word` param) is likewise directly reusable for a lyric word with no
  change.
- Unlike title words and Themes (both available instantly from the
  already-cached `FetchActiveSongs` result), lyric-derived groups depend
  on `song_arrangement_cache` having actually synced each song's lyrics,
  which happens asynchronously via the paced background queue/cron
  described in "Storage" above. This page's data builder should treat a
  song with no cached arrangement row (never synced) the same way
  `fetchAndCacheArrangementInfo`'s callers already treat "song info not
  cached yet" elsewhere in this app - the lyric-derived suggestion list is
  simply computed only from what's cached at that moment, self-correcting
  as the background sync catches up over the following minutes, not a
  blocking live fetch on page open. No error state needed for this; it is
  the same "eventually consistent" posture every other Song Info field
  already tolerates.

### 6. Nav badge

`middleware.PopulateNavState`'s pending-suggestions check (`nav.go`,
currently ORing `scheduling.SongTagSuggestions`/`SongThemeSuggestions`)
needs a third OR for `scheduling.SongLyricSuggestions` - unlike the other
two, this needs a local-DB read of whatever's currently cached in
`song_arrangement_cache` (cheap, no PCO call, fits within the same
`navStateTTL` cache window) rather than data already in-process. Makes the
badge partial during a fresh org's cold-start window (some songs' lyrics
not synced yet) - worth a doc comment, not a blocker.

## Org scoping

No new decision needed: `song_arrangement_cache` (extended) stays
org-scoped exactly as it is today, for the same reason `song_tags`/
`song_vocal_ranges` are (`song_id` isn't a globally unique identifier
across different orgs' separate PCO accounts). `song_tag_categories` stays
shared vocabulary, unaffected by this plan - lyric-derived suggestions
still target the existing `theme` category by default.

## Docs

- `docs/pco-api-risks.md`: one short entry for `fetchAndCacheArrangementInfo`'s
  field-set growth (`Lyrics`/`LyricsEnabled`, zero new calls, same
  response, previously-ignored fields, and a note that the cached payload
  per song is now meaningfully larger - a storage/row-size consideration,
  not a rate-limit one). No change to `songInfoSyncBatchPause`'s math.
- `README.md`: extend the existing Song Tag Suggestions bullet to mention
  lyrics once this ships.
- `docs/ideas.md`: strike the "Lyric-derived tags" bullet under "Sermon
  topic/passage matching" with a pointer to wherever this lands, per
  CLAUDE.md's struck-item convention - worth a note there that the
  already-shipped Phase 1 also covers PCO's native `Themes` field, which
  wasn't part of the original brainstorm bullet.
- `CLAUDE.md`: likely no architectural-boundary change (this stays inside
  `internal/scheduling`/existing controllers/views).

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`.
- Confirm a song with real lyric text in PCO gets `lyrics`/
  `lyrics_enabled` populated in `song_arrangement_cache` after a sync
  (background cron, queue-drain, or the manual refresh button - whichever
  path is easiest to trigger in dev); confirm a song with no lyrics
  entered in PCO caches `lyrics_enabled = false`/`lyrics = ''` without
  erroring; confirm the Suggestions panel on `/scheduling/song-tags`
  renders a "From lyrics" group (or gracefully shows none) without a new
  PCO call - check `docs/pco-api-risks.md`'s claim of "zero new calls"
  against a real network trace, not just the code read; spot-check that
  section labels ("chorus," "verse," etc.) never appear as a suggested tag
  value, and that the frequency ceiling is actually filtering common words
  like "love"/"jesus"/"god" out of real suggestion groups against the live
  library, not just the 2026-09-11 sample.
