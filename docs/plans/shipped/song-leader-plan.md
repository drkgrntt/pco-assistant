# Plan: song leader (per-occurrence), synced into the existing vocal notes

**Status: shipped 2026-09-07.** Migrations `00036` (`set_songs.
leader_person_id`) and `00037` (`service_type_vocal_note_categories`);
`scheduling.Store.UpdateSetSongLeader`/`VocalNoteCategoryID`/
`SetVocalNoteCategory` (`internal/scheduling/sets.go`,
`vocal_note_category.go`); the song detail card's leader
`PersonPickerFieldWithValue` and the Songs tab's "Song leader sync
setting" (`internal/views/scheduling/song_detail_card.templ`,
`build.templ`, `set_builder.templ`); the publish-time prefix-line merge
(`composeLeaderNoteContent`/`syncLeaderNote`,
`internal/server/controllers/leader_note.go`/`sets_publish.go`, with
table-driven tests in `leader_note_test.go`) wired into `PublishSet`/
`RefreshSongInfo`'s existing write loop.

Written 2026-09-06, prompted by a conversation about vocal-range work
(`docs/plans/not-started/vocal-range-plan.md`) that surfaced a related but
distinct gap: this app has no structured concept of "who's actually
leading this song," even though that information already exists informally
today - directors type a name into the "Vocals" Item Note box (see
README's Song notes description) by hand. Derek's own framing: it should
be "a first class citizen here that can feed into vocal notes, that way we
know which person is actually singing the song" - not scraped out of free
text, and not replacing that free text either, but a real local field this
app owns that pushes a line into it.

This plan is independent of `vocal-range-plan.md` and doesn't block or get
blocked by it - either can be built first. The one connection point is
noted in "Relationship to vocal range" below.

## Current state

**No leader/lead-vocalist concept exists anywhere in this codebase
today.** The closest thing is Item Notes (`pco.ItemNoteCategory`/
`ItemNote`, go-pco v0.14.0) - real, per-service-type-configured note
categories (whatever the org named them in PCO: "Audio/Visual," "Band,"
"Vocals," etc.), one free-text box per category on a published song's
detail card (`internal/views/scheduling/song_detail_card.templ`), saved
straight to Planning Center **immediately** via `UpdateSongNote`
(`internal/server/controllers/sets_detail.go` ~L277) - not part of the
draft/Publish cycle at all. `pco.UpdateItemNote`/`pco.CreateItemNote`/
`pco.DeleteItemNote` are the underlying calls; content is a single opaque
string per category, no structure of any kind.

**Item Notes require a published item.** `UpdateSongNote` operates on
`itemID` (the song's real PCO plan item id) - an unpublished song has none
yet (`song_detail_card.templ`'s own copy: "Publish this song first to add
notes here"). Any mechanism that writes a leader's name into this note
category inherits the same constraint.

**The existing precedent for "local draft field, applied on next
Publish/Refresh"** is `set_songs.key_override`/`set_songs.description`
(migrations `00028`/`00029`, `internal/scheduling/sets.go`
`SetSong.KeyOverride`/`.Description`, `UpdateSetSongKeyOverride`/
`UpdateSetSongDescription`) - a plain column on the local draft row,
edited freely pre-publish with no PCO call at all, tracked by a shared
`info_pending` flag (`ClearInfoPending`) that's cleared only once
`PublishSet`/`RefreshSongInfo` actually pushes it. This is the shape a
per-occurrence "who's leading this song" field should copy - it's a fact
about *this song in this plan*, not a persistent fact about the song
itself (a different worship leader can lead the same song next month),
exactly the same reasoning that puts `KeyOverride`/`Description` on
`set_songs` rather than on a song-level table.

**Migration numbering conflict, flagged up front**: the latest applied
migration is `00034_song_arrangement_structure.sql`.
`vocal-range-plan.md` (also not started) already tentatively claims
`00035` for its own `person_vocal_range` table. Whichever of these two
plans is built first gets `00035`; the other renumbers to the next
available number at build time - not a design conflict, just a
bookkeeping note so neither implementer assumes their number is fixed.

## Which product promise this serves

Per CLAUDE.md's "Product direction" test: primarily **protect the
service** - it's operational clarity (who's actually driving each song),
the same register as set-flow/repetition concerns, not a burnout/
preference axis. It has a secondary **protect the team** angle too: it
removes a small, recurring point of friction/ambiguity (hunting through
free text or asking "wait, who's singing lead this week?") - but the
primary justification is service-side clarity, not team-relational.

## What "first class" means here, concretely

**Storage**: a new nullable column on `set_songs`, `leader_person_id
TEXT`, following `key_override`'s exact pattern - local, freely editable
pre-publish, no PCO call on its own. Migration (numbering per the
conflict note above):

```sql
-- +goose Up
ALTER TABLE set_songs ADD COLUMN leader_person_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE set_songs DROP COLUMN leader_person_id;
```

Empty string means "no leader set," same "empty means nothing set yet"
convention `Description`/`KeyOverride` already use - not modeled as a
nullable/NULL column, to match the existing column style in this table.

**New `Store` method**: `UpdateSetSongLeader(ctx, setID, rowID,
personID string) error` in `internal/scheduling/sets.go`, identical shape
to `UpdateSetSongKeyOverride` (sets the column, marks `info_pending =
true`).

**UI placement**: the Build page's song detail card
(`song_detail_card.templ`), alongside the existing per-occurrence Key
override and Description controls - **not** a new setup page. This is a
deliberate contrast with `vocal-range-plan.md`'s *song* vocal range (which
correctly lives on its own setup page, because a song's range is a
persistent fact reused across every plan) - a song's leader is the
opposite: a per-occurrence fact, re-entered (or left blank) every time the
song comes up in a new plan, exactly like key override and the
per-occurrence note. A `PersonPickerField` (the same component
`preferences.templ`/`roster_builder.templ` already use) replaces a plain
text name field, so "leader" is a real person id, not a name typed twice
in two different places that can drift out of sync with each other.

## Publish-time sync into the existing vocal notes category

This is the part that needs a real decision, laid out here rather than
guessed:

**Which note category is "the vocal one"?** Categories are configured
per service type, named freely by the org in PCO ("Vocals," "Singers,"
whatever) - there's no fixed enum this app can pattern-match reliably
(the same reasoning `vocal-range-plan.md`'s Phase 3 already applies to
"who counts as a vocalist," rejecting a team/position-name heuristic for
the identical reason). Two real options:

1. **Fuzzy name match** (e.g. category name contains "vocal," case-
   insensitive). Simple, zero new configuration, but silently does
   nothing for an org that named their category "Singers" or "BGVs" -
   fails quietly exactly where a director would most want it to work.
2. **Explicit one-time designation.** A small per-service-type setting -
   "which note category should the song leader sync into?" - picked once
   from that service type's actual configured categories (already fetched
   for the Item Notes feature, `noteCategoriesForServiceType`-equivalent
   in `sets_builder.go`), stored locally (a new small table, or a column
   if this app already has a per-service-type settings row - check before
   adding a new table). More setup friction (one dropdown, once, ever, per
   service type) for correctness that doesn't silently fail.

**Recommendation: option 2.** This app already leans toward "let the org
configure the org-specific text once" over "guess at their naming" -
option 1's silent-failure mode (a leader gets set locally, nothing ever
shows up in PCO, and nobody notices until someone asks "why isn't this
working") is a worse failure shape than a one-time setup step, especially
given CLAUDE.md's own point about PCO reads: a 429 or a missed sync fails
silently already, and this shouldn't be one more place that happens by
design.

**How the write is composed, not just where.** The target category's
content is free text a human might also be editing by hand (that's the
entire point of Item Notes) - naively overwriting the whole field on every
publish would clobber anything else typed there. Two options, again not
guessed past:

1. **Reserved prefix line.** This app owns and manages a single
   recognizable first line (e.g. `Leading: Jane Smith`) - on
   publish/refresh, detect and replace an existing line matching that
   exact pattern at the start of the category's content, leaving
   everything after it untouched; if no such line exists, prepend one; if
   the local leader is cleared, remove the line rather than leaving stale
   text. Satisfies "shows up in the same familiar box people already
   look at," at the cost of a small amount of fragile string-matching
   logic living in this app.
2. **A second, dedicated note category reserved entirely for this** (the
   org creates/renames a category solely for auto-generated leader lines,
   never hand-edited). Zero string-parsing risk, but doesn't satisfy "the
   same vocal notes slot people already use" - it's a new box, not the
   existing one, which works against the stated motivation for this
   feature in the first place.

**Recommendation: option 1**, since "feeds into vocal notes" was the
explicit ask - a second box defeats the point. Flagging the
prefix-line-matching logic as the one piece of this plan that needs
careful, tested edge-case handling (empty content, content that already
has a differently-formatted leader line from before this feature existed,
a category with no note yet at all) rather than a quick regex.

**Write timing**: same as `KeyOverride`/`Description` - applied by
`PublishSet` and `RefreshSongInfo` (`internal/server/controllers/
sets_publish.go`), in the same per-song write loop, using the same
`pco.UpdateItemNote`/`pco.CreateItemNote`/`pco.DeleteItemNote` calls
`UpdateSongNote` already uses (find-or-create the target category's note
for this item, apply the prefix-line merge above, write it). Marks
`info_pending` handling the same way the existing fields do - a leader set
on an already-published song shows as pending until the next
Publish/Refresh actually pushes it, not immediately.

## Relationship to vocal range

Once both this and `vocal-range-plan.md`'s Phase 3 (mismatch warning)
exist, the leader is a strictly better answer to "who counts as a
vocalist to check" than that plan's own fallback rule ("anyone assigned to
the roster with a stored range") - the leader is *the* person whose range
actually matters most for a given song, not just any vocalist who happens
to be on the roster. This plan doesn't require reopening that section now
(vocal-range-plan.md's fallback rule is a reasonable default with zero
dependency on this plan existing), but a future refinement once both ship
should prioritize/highlight the leader specifically in that warning's
copy ("Jane is leading and her stored range doesn't cover this key")
rather than treating every vocalist on the roster equally. Noted here so
whichever plan is built second doesn't have to rediscover the connection.

Explicitly **not** in scope here: splitting a song between multiple
leads/vocalists (verse-by-verse "sharing" a song) - that's a materially
bigger assignment-model question, worth its own future `docs/ideas.md`
bullet rather than folding into this plan's single-leader-per-occurrence
scope.

## Suggested build order

1. Migration + `Store.UpdateSetSongLeader` + `SetSong.LeaderPersonID`.
2. UI: `PersonPickerField` on the song detail card, wired to the new
   store method, following `KeyOverride`'s existing form/handler shape in
   `sets_detail.go`.
3. The per-service-type "vocal notes category" designation (option 2
   above) - its own small setting, surfaced wherever this app's other
   per-service-type/org settings live today (check `account.go`/whatever
   backs the Setup nav's org-level settings before inventing a new
   settings surface).
4. The publish-time sync itself: prefix-line merge logic (with tests
   covering the edge cases named above) wired into `PublishSet`/
   `RefreshSongInfo`'s existing write loop.

## Verification (once built)

- `go build ./...` clean after each step.
- `make test` - table-driven tests for the prefix-line merge (no existing
  content, existing content with no leader line, existing content with a
  stale leader line to replace, clearing a leader removing the line
  cleanly, content that happens to start with text coincidentally similar
  to the reserved prefix).
- Live: set a leader locally, publish, confirm the Vocals-designated
  category's content in real Planning Center shows the leader line intact
  alongside any pre-existing manual text; change the leader and
  publish/refresh again; confirm the old line is replaced, not
  duplicated; clear the leader and confirm the line disappears.
- No new hot-path PCO call is introduced (this rides the existing
  publish/refresh write loop) - `docs/pco-api-risks.md` doesn't need an
  update for this plan.
