# Plan: lyric-derived song tag suggestions

Written 2026-09-10, prompted by revisiting `docs/ideas.md`'s "Sermon
topic/passage matching" section's "Lyric-derived tags" bullet now that the
underlying data-availability question is settled: `go-pco`'s
`ArrangementAttributes.Lyrics` is real and already wrapped (confirmed while
scoping `docs/plans/shipped/song-lead-sheet-attachments-plan.md`). Not built
yet; written up for later, same spirit as
`docs/plans/shipped/song-lead-sheet-attachments-plan.md`/
`docs/plans/shipped/song-tags-setup-redesign-plan.md`.

**Scope note, read first**: this doc deliberately does *not* pick the
parsing/matching mechanism that turns raw lyric text into a topic/theme
suggestion - see "Open questions" below. Everything else (where the text
comes from, how/where it's cached, how a suggestion gets in front of a
human, how it's stored once accepted, org scoping) is resolved concretely,
because none of that depends on which extraction mechanism eventually gets
picked.

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
  stopword list**, explicitly "per the 'no AI' decision it was designed
  against" (that function's own doc comment). `SongTagSuggestions` groups
  songs sharing a significant title word (2+ songs per group), skips
  already-tagged songs/ignored words, and returns `SongTagSuggestionGroup`
  values.
- `ignored_song_tag_suggestions` (migration `00022`) is a permanent per-org
  "don't suggest this word again" dismiss list, keyed by the word itself,
  no un-ignore UI.
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
same reason the shipped feature didn't need one.

### The consuming end already works "for free"

`internal/server/controllers/sets_builder.go`'s `buildSetBuilderData` calls
`store.DistinctSongTagValues` to populate the "Browse by tag" chips
(`set_builder.templ`), and `candidateMatchesSearch` matches typed search
text against a song's tag values as well as its title - both read plain
`song_tags` rows with no awareness of where a tag came from. So a
lyric-derived tag, once accepted through whatever UI this plan builds,
shows up in "Browse by tag" and candidate search identically to a manual
tag, with zero additional wiring - confirmed by reading both call sites,
not assumed.

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
   ever added to a set - important, because the title-word suggestion
   page's "whole library" scope depends on the equivalent lyric cache
   also eventually covering the whole library, not just set-touched
   songs.

### Lyrics availability isn't guaranteed

`docs/plans/shipped/song-lead-sheet-attachments-plan.md`'s "Why automatic
range extraction is out of scope" section already establishes that
`Arrangement.Lyrics`/`ChordChart` are **plain text fields, not required by
PCO** - a song can have `LyricsEnabled: false`, or `LyricsEnabled: true`
with an empty `Lyrics` string if nobody ever pasted lyrics in. Any
lyric-derived mechanism has to treat "no usable lyric text" as a normal,
frequent case to skip, not an error - see "Open questions" below for how
this interacts with the extraction-mechanism choice.

## Resolved architecture

### 1. Fetch: extend the existing arrangement-info fetch, no new PCO call

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
(next available number as of this writing):

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

### 3. Suggestion storage: no new table, no new flag - reuse `song_tags`/`CreateSongTag` exactly as-is

Per the title-word precedent above, a suggestion is never itself a
database row - it's computed at read time from the cached lyric text plus
whatever this org has already tagged/ignored, the same way title-word
groups are computed from `FetchActiveSongs` plus `ListSongTags`/
`ListIgnoredSongTagSuggestionWords`. Accepting a suggestion is just a
`CreateSongTag` call, identical to a manually-typed tag - there is
deliberately no way to tell them apart after the fact, matching the
shipped feature's own resolution of this exact question. The only new
persisted state this needs is an ignore/dismiss list for whatever
suggestion unit the eventual mechanism produces - if it's still
word-based, `ignored_song_tag_suggestions` may be directly reusable as-is;
if the mechanism produces a different unit (a topic id, a phrase, a
scripture reference), it likely needs its own small ignore table
mirroring `ignored_song_tag_suggestions`'s shape. This is downstream of
the mechanism decision in "Open questions," not resolved here.

### 4. UX integration: extend the existing Song Tag Suggestions page, not a new one

The lowest-friction spot is `/scheduling/song-tag-suggestions`
(`SongTagSuggestionsPage`, `song_tag_suggestions.templ`) - it's already
the one place in this app where an algorithmic tag suggestion gets
reviewed and accepted/rejected by a human, it's already linked from the
nav with a "pending suggestions" badge, and the Card/checklist/Apply/
Ignore interaction pattern is already built and already the thing a user
has learned. Per CLAUDE.md's friction test ("is this the fast, obvious
way to do the thing, or does it hand the user a second UI pattern to
learn"), building a second, separate "lyric tag suggestions" page would
be exactly the failure mode that test warns against - same underlying
job (review algorithmic tag suggestions, apply or dismiss), a different
address to remember to check.

Concretely: extend `buildSongTagSuggestionsData`
(`internal/server/controllers/song_tag_suggestions.go`) to compute a
second slice of suggestion groups from cached lyric text (source function
TBD per "Open questions"), and either render it as a second, clearly
labeled section within the same `SongTagSuggestionsList` fragment (e.g.
"From shared title words" / "From lyrics") or merge both sources into one
list if the eventual group shape allows it - a real mockup call once the
mechanism is chosen, not decided here. The Apply/Ignore forms themselves
need no new shape: they already parameterize on `(category, value,
song_ids)`, which any lyric-derived group can populate identically to a
title-word group.

One asymmetry worth resolving concretely now: unlike title words
(available instantly from the already-cached `FetchActiveSongs` result),
lyric-derived groups depend on `song_arrangement_cache` having actually
synced each song's lyrics, which happens asynchronously via the paced
background queue/cron above. This page's data builder should treat a
song with no cached arrangement row (never synced) the same way
`fetchAndCacheArrangementInfo`'s callers already treat "song info not
cached yet" elsewhere in this app - the lyric-derived suggestion list is
simply computed only from what's cached at that moment, self-correcting
as the background sync catches up over the following minutes, not a
blocking live fetch on page open. No error state needed for this; it is
the same "eventually consistent" posture every other Song Info field
already tolerates.

### 5. Nav badge: extend, with an explicit tradeoff noted

`PopulateNavState`'s "suggestions pending" check can be extended to also
consider lyric-derived groups, but - unlike title words - it can't do so
from data it already has cheaply in-process; it would need a local-DB
read of whatever's currently cached in `song_arrangement_cache` (cheap,
no PCO call, fits within the same `navStateTTL` cache window) rather than
a live PCO fetch. This makes the badge partial during a fresh org's
cold-start window (some songs' lyrics not synced yet) the same way the
title-word check has no such gap - worth noting in the eventual
implementation's doc comment, not a blocker.

### 6. Org scoping

No new decision needed: `song_arrangement_cache` (extended) stays
org-scoped exactly as it is today, for the same reason `song_tags`/
`song_vocal_ranges` are (`song_id` isn't a globally unique identifier
across different orgs' separate PCO accounts - two orgs' same-valued
`song_id` can be two unrelated real songs). `song_tag_categories` stays
shared vocabulary, unaffected by this plan - a lyric-derived suggestion
still targets one of the existing categories (most likely `theme`/
`idea`, mirroring the title-word suggestion form's default of `theme`).

### 7. Docs

- `docs/pco-api-risks.md`: add a short entry noting `fetchAndCacheArrangementInfo`'s
  captured field set grew (Lyrics/LyricsEnabled) with **zero new PCO
  calls** - same response, previously-ignored fields - and a note that
  the cached payload per song is now meaningfully larger (lyric text vs.
  a handful of scalars), which is a storage/row-size consideration, not a
  rate-limit one. No change to `songInfoSyncBatchPause`'s math.
- `README.md`: once built, note the extended Song Tag Suggestions page
  under whatever section documents song tags/setup pages.
- `docs/ideas.md`: strike the "Lyric-derived tags" bullet under "Sermon
  topic/passage matching" with a pointer to wherever this actually lands,
  per CLAUDE.md's struck-item convention, once shipped (not before).
- `CLAUDE.md`: likely no architectural-boundary change (this stays inside
  `internal/scheduling`/existing controllers/views), unless the eventual
  mechanism introduces a genuinely new package (e.g. an LLM-client
  package) - revisit once the mechanism is chosen.

## Open questions - real decisions, not defaults

These need actual back-and-forth before any extraction code gets written.
Laid out as options with tradeoffs, not a recommendation.

### A. What granularity/taxonomy does a suggestion target?

- **Freeform keyword/phrase extraction** - generalize `SongTitleWords`'
  approach to lyric text: pull out significant repeated words/phrases
  across a song's lyrics (or across the library, grouping songs sharing
  one), same shape as the shipped title-word feature. Lyrics are much
  longer and more repetitive than titles (a chorus repeated several
  times), so raw word frequency behaves very differently - common
  worship vocabulary ("Lord," "Jesus," "praise," "holy") would dominate
  every song's word list unless filtered much more aggressively than the
  existing 40-ish-word stopword list handles today.
- **A curated topic/theme dictionary** - a maintained mapping from
  specific words/phrases to a fixed topic vocabulary (e.g. "cross,"
  "blood," "sacrifice" -> "atonement"). More precise, more explainable,
  but real ongoing maintenance burden, and nothing else in this codebase
  currently maintains this kind of content list.
- **Scripture-reference detection within lyric text** - regex/pattern
  matching for things that look like "Romans 8" or "Psalm 23" appearing
  in lyrics. Related to, but explicitly distinct from,
  `docs/ideas.md`'s sibling bullet **"Scripture reference as a structured
  field"** (which is about a director *tagging* a song with a reference
  directly, not extracting one from lyric text). Worth an explicit
  decision on whether lyric parsing should attempt this lane at all, or
  stay purely thematic/keyword-based and leave scripture detection to
  that separate, not-yet-built structured-field idea.
- **Something else** - e.g. a fixed sentiment/mood axis, repetition-based
  "chorus phrase" extraction independent of any topic mapping, etc.

### B. What mechanism actually does the extraction?

- **Naive keyword/phrase matching** (generalized `SongTitleWords`) -
  zero cost, zero latency, fully explainable ("this word appears in these
  N songs' lyrics"), consistent with this app's existing "no AI" decision
  for the title-word case. Realistic accuracy risk: lyrics are much
  noisier than titles (see A above), so a naive port of the existing
  algorithm may produce far less useful groups than it does for titles,
  without real tuning.
- **A maintained synonym/topic dictionary** - meaningfully better
  precision than raw word matching (can map "cross"/"calvary"/"blood" to
  one "atonement" topic), but is ongoing curation work with no existing
  analog in this codebase to model it on, and someone has to keep it
  current as new songs/vocabulary show up.
- **An LLM call per song** - plausibly the highest accuracy at actually
  identifying a song's theme(s) the way a human skimming the lyrics
  would, including things no fixed dictionary would ever anticipate. Real
  costs worth weighing explicitly: per-song latency and dollar cost (this
  is a batch job over a whole library, not a one-off), a new external
  dependency this app has none of today (its own outage/rate-limit/cost
  profile, separate from PCO's), and a much weaker "why did it suggest
  this" explainability story than word-matching's literal "this word is
  in these songs."
- **Other NLP** (TF-IDF across the library, embedding similarity between
  songs, etc.) - more machinery than any of the above, unclear payoff
  over the simpler options without trying it.

This is the single biggest fork: `SongTitleWords`' own doc comment records
that lyric parsing's sibling feature (title words) made a deliberate
"no AI" choice. Whether that reasoning was specific to titles (short,
non-repetitive, low-noise) or was meant to be a standing project-wide
stance that should also rule out an LLM here is exactly the kind of thing
that needs a direct answer, not an inference.

### C. Confidence/review model - auto-applied, or always human-gated?

Every comparable flow already in this app is human-gated: title-word tag
suggestions require an explicit Apply click, and the Publish/Discard sync
model never pushes a change without a deliberate action. That's worth
noting as the house style, not as a forced conclusion - whether that
generalizes here (especially if a higher-confidence mechanism, like
explicit scripture-reference regex matches, is in play) is worth asking
directly rather than assuming "same as before."

### D. Where does the analysis actually run?

- **On page-open of Song Tag Suggestions**, recomputed live off whatever
  lyric text is already cached (cheap if the mechanism is naive
  matching, since the durable cache lookup above already resolves "no
  new PCO call" - this only matters for the *extraction* cost, not the
  fetch cost).
- **A background cron step** that pre-computes and persists candidate
  suggestion groups on a schedule, mirroring `triggerSongInfoBackgroundSync`'s
  shape - necessary if the chosen mechanism has real per-run cost (an
  LLM call), since recomputing on every page visit would be wasteful or
  slow.
- **An explicit "scan for lyric tags" button** - mirrors the *old*
  Song Info "Sync now" button this app deliberately moved away from
  (`docs/plans/shipped/pco-call-efficiency-rollout-plan.md` Phase 2,
  replaced by automatic queue-drain specifically because a manual button
  became a nag/bottleneck) - worth knowing that history before repeating
  the pattern, without treating it as an automatic disqualifier if the
  chosen mechanism's cost genuinely warrants an explicit, deliberate
  trigger (e.g. if it costs real money per run).

This cascades directly from B: a free/instant mechanism can run live on
every page load; an LLM-per-song mechanism almost certainly can't, and
needs its own persisted-result and freshness story that doesn't exist
yet.

### E. Lyrics-availability edge case

Not every arrangement has usable lyric text - `LyricsEnabled` can be
false, or true with an empty `Lyrics` string (confirmed by
`song-lead-sheet-attachments-plan.md`'s own finding that these are
optional free-text PCO fields). Whatever mechanism gets chosen must treat
"no lyric text for this song" as an expected, frequent, silently-skipped
case - not an error, and not something that should ever look like "we
checked and found nothing thematic," which the same "row exists = checked
successfully" convention `song_arrangement_cache` already uses elsewhere
handles fine as long as the extraction step itself checks `LyricsEnabled`/
`len(Lyrics) > 0` before doing any work.

## Verification (once built, whichever way the open questions land)

- `go build ./...` (empty output = success).
- `make test`.
- Live: confirm a song with real lyric text in PCO gets `lyrics`/
  `lyrics_enabled` populated in `song_arrangement_cache` after a sync
  (background cron, queue-drain, or the manual refresh button - whichever
  path is easiest to trigger in dev); confirm a song with no lyrics
  entered in PCO caches `lyrics_enabled = false`/`lyrics = ''` without
  erroring; confirm the Song Tag Suggestions page renders lyric-derived
  groups (or gracefully shows none) without a new PCO call - check
  `docs/pco-api-risks.md`'s claim of "zero new calls" against a real
  network trace, not just the code read.
