# Plan: redesign the Song Tags setup page

Written 2026-09-04, prompted by feedback on the just-shipped Build-page
"browse by tag" chips: the underlying complaint turned out not to be about
the Build page at all, but about `/scheduling/song-tags` (the setup page
where tags actually get created) - "the ability to look through and
organize things," called out as "a different subject entirely, a
redesign." Not built yet; written up for later, same spirit as
`docs/plans/shipped/pco-roster-cold-start-plan.md`/`docs/plans/not-started/song-lead-sheet-attachments-plan.md`.

## Current state (`internal/views/scheduling/song_tags.templ`,
`internal/server/controllers/song_tags.go`)

One page, two pieces:

1. An "add a tag" form: a plain `<select name="song_id">` listing every
   active song as an `<option>` ("Choose a song"), a category `<select>`,
   and a free-text value input. This exact same plain-`<select>`-of-every-
   song pattern is also used by Song Roles, Preferences, and Relationships
   (`song_roles.templ`, `preferences.templ`, `relationships.templ`) - all
   four setup pages share it, though this plan only scopes the Song Tags
   page itself; if the fix here works well, the others are candidates for
   the identical swap later, not part of this plan.
2. `SongTagsTable`: a single flat, sortable table (`components.SortableTableWrap`)
   with one row per **tag**, not per song - a song with three tags appears
   as three separate rows, each repeating its own title in the Song
   column. Edit/remove act on one row (one tag) at a time.

Pain points this plan is responding to:

- A `<select>` with hundreds of `<option>`s is a bad way to find a specific
  song by typing - most browsers only do first-letter-jump, not substring
  search, unlike a real search box.
- The flat one-row-per-tag table makes "what does this song already have
  tagged" require scanning/sorting the whole table for repeated title
  text, rather than seeing one song's tags together.
- Neither piece gives any sense of "how much of the library is tagged at
  all" - there's no way to browse the library itself here, only the tags
  already created.

## What already solves the "search a big list by typing" half elsewhere

`internal/views/scheduling/person_picker.templ` (`PersonPickerField`/
`PersonSearchResults`, `selectPerson` in `web/static/js/scheduling.js`) is
exactly this pattern, already built and in use on the Person
Relationships/Preferences pages: a text box + hidden id input + a live
results `<div>`, backed by `/scheduling/people-search` re-querying on
`hx-trigger="input changed delay:300ms"`. The person version searches PCO
live (`personSearchResultLimit`, since a People directory is often too big
to fetch in full) - songs are different: `fetchActiveSongs` already fetches
and in-process-caches the *entire* active library
(`scheduling.songCacheTTL`), so a song-picker's "search" endpoint has
nothing live to fetch at all - it would just filter the already-cached
slice in Go and re-render matches, no PCO call involved. Same UI/JS shape,
cheaper backend than the person version it's modeled on.

## Proposed redesign

Two independent pieces - build both, but they don't depend on each other:

### 1. Song picker for the "add a tag" form

Replace the `<select name="song_id">` with a `SongPickerField`-style
component mirroring `PersonPickerField`'s shape exactly (text box, hidden
`song_id` input, live results div), backed by a new endpoint (e.g.
`GET /scheduling/song-search`) that filters the cached `fetchActiveSongs`
list by title substring (same matching rule `candidateMatchesSearch`
already uses) and renders up to some small cap (mirror
`candidateSearchLimit`'s existing precedent rather than inventing a new
number). No PCO call added - purely filtering data already in the
in-process cache.

### 2. Group the tags table by song, not by tag row

Instead of `SongTagsTable`'s current one-row-per-tag layout, show one
entry per **song that has at least one tag**, each listing all of that
song's tags together (grouped by category, matching the category order
`ListSongTagCategories` already returns) - e.g.:

```
Amazing Grace
  theme: grace, hope
  scripture: Romans 8

How Great Thou Art
  theme: worship
```

Open questions to settle before building, not answered by this plan:

- **Layout mechanism** - a real nested structure (song as a heading, tags
  as sub-rows/chips underneath) rather than `SortableTableWrap`'s flat
  `<table>`, since "sortable by column" stops making sense once a row can
  hold a variable number of tags. Something closer to `components.Card`
  per song, or a `<details>`/disclosure per song (mirrors
  `collapsibleSection`'s existing idiom elsewhere in this app for
  "could get long" lists) - needs an actual mockup, not just prose,
  before committing.
- **Editing a tag in place** - today's edit flow
  (`data-edit-form`/`editRow`, `resetSetupForm`) targets one flat form
  reused for every row via `data-edit-fields`. A grouped-by-song layout
  still needs *some* way to edit/remove one tag without leaving the page -
  likely the same hidden-form-plus-`editRow` mechanism, just triggered
  from a chip/sub-row instead of a table row, but confirm this still works
  once rows aren't uniform table cells.
- **Should every song show, or only tagged ones?** Showing every active
  song (with "no tags yet" for the untagged ones) directly answers "how
  much of the library is tagged" but risks the exact "hundreds of songs on
  one page" problem this redesign exists to fix - probably wants the same
  live-search-driven approach as the Build page's own candidate list
  (nothing rendered until a search narrows it, see `CandidateSearchResults`'s
  doc comment for why), rather than dumping the whole library by default.
  Needs a real decision, not an assumption either way.
- **Does the flat "add a tag" form still make sense as a separate piece**
  once songs are browsable/groupable, or should adding a tag happen
  inline from within a song's own group (search for a song, see its
  existing tags, add one right there)? The latter is more cohesive but a
  bigger rebuild than swapping the picker alone.

## Suggested build order, once the open questions above are answered

1. `SongPickerField`/song-search endpoint (independent, low-risk, same
   shape as an existing pattern - safe to build first regardless of how
   the grouping question above gets resolved).
2. The grouped-by-song view, once its layout/editing/scope questions have
   real answers rather than open bullets.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`.
- Live: confirm the song picker filters correctly with a real multi-
  hundred-song library, confirm add/edit/remove still work against the new
  grouped layout, confirm no PCO call was added (check
  `docs/pco-api-risks.md` needs no update - this is local-DB-only).
