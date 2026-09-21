# Plan: standalone song and person detail pages

**Status: in progress.** Phases 1-2 (song library + song detail page,
people library + person detail page) are built, on branch
`song-person-detail-pages`. Phase 3 as originally written below (a
"retirement pass" deleting the eight flat setup pages this doc
originally proposed replacing) was reconsidered and dropped before being
attempted - see "Disposition of the 10 existing flat pages" for why, that
section rewritten in place to reflect the reversal rather than left to
contradict what actually shipped. What happened instead of a retirement
pass: a lightweight nav-link addition for the two new pages, sitting
alongside the existing ten links rather than replacing them.

Originally written up for review before anyone picked it up, same spirit
as `docs/plans/shipped/song-roles-bucket-redesign-plan.md` (a redesign of
one of the same setup pages this plan also touches, shipped since this
plan was first written - see "Interaction with the Song Roles bucket
redesign" below). Prompted by the same underlying complaint that drove
`docs/plans/shipped/song-tags-setup-redesign-plan.md`: that doc fixed
"the ability to look through and organize things" for exactly one of
seven song-related setup pages (Song Tags). This plan generalizes that
fix's card-per-song/person *browsing* pattern into a genuinely new
capability this app never had - a standalone page for browsing the song
library or the people roster, and a single aggregate page for everything
about one song or person - **as a second, complementary way in, not a
replacement for the existing flat per-relationship-type setup pages**
(see "Disposition" below for why that distinction turned out to matter).

## The problem

This app's own "setup" data - song/person metadata it owns, distinct
from PCO's own records - is spread across ten separate pages (see
`internal/server/controllers/scheduling.go`'s `RegisterRoutes` and the
matching `internal/views/scheduling/*.templ` files), each showing one
*type* of data across the *whole* library/roster:

- Song-related: `/scheduling/song-relationships`, `/scheduling/song-preferences`,
  `/scheduling/song-roles`, `/scheduling/song-contexts`,
  `/scheduling/song-tags`, `/scheduling/song-tag-suggestions`,
  `/scheduling/song-vocal-range`.
- Person-related: `/scheduling/person-relationships`,
  `/scheduling/vocal-range`, `/scheduling/position-preferences`.

To fully understand or configure *one* song or person, someone has to
visit most of these pages and re-find that same song or person on each
one. There is also no standalone "browse the song library" or "browse
the people roster" page today - songs/people are only ever browsed
*inside* something else: the Build page's Songs/Team tabs (plan-scoped),
or each flat setup page's own picker.

## Two precedents this plan builds on, not from scratch

1. **`docs/plans/shipped/song-tags-setup-redesign-plan.md`** already
   solved this pain for one page: it replaced Song Tags' flat
   one-row-per-tag table with a persistent `components.Card` per song
   (every song's tags as chips, an inline add-form scoped to that song).
   That doc explicitly named Song Roles, Preferences, and Relationships
   as "candidates for the identical swap later." **Song Contexts already
   got the same swap too** (`internal/views/scheduling/song_contexts.templ`'s
   `SongContextCard`, built on the shipped Song Tags pattern per its own
   doc comment) - so two of the seven song pages are already
   card-per-song today, not zero. This plan's job is the remaining five
   song pages, the two person pages that fit the same shape, and - the
   genuinely new part - a page that shows *everything* about one song or
   person together, not just one facet at a time.
2. **The Build page's own detail cards already aggregate everything
   about one song/person into a single card** -
   `internal/views/scheduling/song_detail_card.templ` (`songDetailCard`)
   and `internal/views/scheduling/person_detail_card.templ`
   (`personDetailCard`), sharing `internal/views/scheduling/build_detail_card.templ`'s
   shell - just scoped to one plan occurrence. See "What's actually
   reusable from the Build detail cards" below for exactly how much of
   each is library-level vs. occurrence-bound.

## What's actually reusable from the Build detail cards

### Song side - most of it is library-level already

`songDetailCard`'s content splits cleanly:

- **Library-level (same data for every occurrence, already sourced from
  an org-wide cache, not a per-plan value)**: `ArrangementInfo`,
  `Structure`, `HasChordChart`, `Attachments` all come from
  `scheduling.Store.SongArrangementInfo`/`SongAttachments` (org-wide
  maps, `internal/scheduling/song_arrangement_cache.go`/
  `song_attachment_cache.go`) - `SetSongView`'s own doc comments say so
  explicitly ("Detail-card only... isn't shown on the row"). Roles
  (`song.Roles` badges) come from `song_roles`, also library-level.
  `transitionNotesSection` is explicitly documented as "keyed to the
  song(s), not to this `set_songs` row or this plan id" and deliberately
  rendered even when the plan is read-only - it is, in the data sense,
  already a library-level feature wearing a plan-scoped URL (see
  "Transition notes need new routes" below).
- **Occurrence-specific (do not belong on a library page)**:
  `Published`/`Moved` badges, `keyForm`/`leaderForm`/`descriptionForm`
  (this week's key/leader/note - a `set_songs` row value), the
  per-category `songNoteForm`s (a PCO `ItemNote` on *this plan's* item),
  and the Duplicate/Remove/Replace actions (they only make sense against
  a specific set).

So a song's library-wide page can legitimately show arrangement info,
structure, chord chart, attachments, roles, and transition notes with
**zero** new data-fetch work - `scheduling.FetchActiveSongs` plus the
existing `Store.SongArrangementInfo`/`SongAttachments`/`ListSongRoles`/
`ListTransitionNotesForSong`-style calls cover it, the same org-wide,
cheap, local-DB-or-cached-PCO reads the Build page already makes.

### Person side - almost none of it is library-level

`personDetailCard` is nearly the opposite: `TeamName`/`TeamPositionName`
(this position, on this plan), `statusBadge`, the `Published` badge, and
`FrequencyWarning`/`BlockoutWarning`/`CrossTeamWarning` are all
meaningless without a specific plan date - a blockout/frequency
conflict is computed *against* an occurrence, not a fact about the
person in the abstract. `Notes` is read-only PCO item-assignment text
tied to this plan. **Only `personVocalRangeLine` is genuinely
library-level** - its own doc comment already says a person's range
"only ever changes via the standalone `/scheduling/vocal-range` setup
page." A person's library-wide page therefore reuses almost none of
`personDetailCard`'s markup - it's built from the person-side setup
pages' own data instead (relationships, vocal range, preferences), not
from the Build sidebar.

### Transition notes need new routes, not new data

`QuickAddTransitionNote`/`DeleteTransitionNote`
(`internal/server/controllers/transition_notes.go`) take
`service_type_id`/`plan_id` URL params but only use them to know which
Build-page fragment to re-render afterward - the actual write
(`scheduling.CreateTransitionNoteParams`) is keyed by song id(s) alone,
and `transitionNotesBySong` is already a plan-agnostic per-song grouping
helper. A song detail page needs its own thin
`AddSongDetailTransitionNote`/`DeleteSongDetailTransitionNote` handlers
that call the same store methods and re-render the song detail page/
section instead of the Build fragment - no change to the existing
Build-page handlers or routes.

## The other real asymmetry: songs are a cached library, people are not

`scheduling.FetchActiveSongs` pages through and in-process-caches the
*entire* active song library (`scheduling.songCacheTTL`) - that cheap,
complete fetch is what made Song Tags' "render every song unconditionally"
viable. **Nothing equivalent exists for people.** Every existing person
lookup in this app is either a live fuzzy search capped at
`personSearchResultLimit` (8) (`PeopleSearch`/`PersonPickerField`), or a
batched lookup for an *already-known* set of ids
(`personLookupMapForIDs`/`personNameMapForIDs`,
`internal/server/controllers/people.go`, batched at `personIDBatchSize`
(100) per call). Paging through PCO's whole People directory the way
`FetchActiveSongs` pages through songs would be a new, unbounded PCO
cost this app has never paid anywhere - a real risk per
`docs/pco-api-risks.md`, not a detail to wave past.

**Resolution**: scope the People library index to *people this app
already has at least one local record for* - the distinct set of person
ids already referenced across `person_relationships`,
`song_person_preferences`, and `person_vocal_range` (three cheap local-DB
queries, `UNION`-style, same shape as `HasAnySongTags`'s existing
existence checks), resolved to display names/avatars via the existing
`personLookupMapForIDs` batch call - plus the existing
`PersonPickerField` search box (already on every person-related setup
page) as the way to reach someone with zero local records yet and create
their first one, which is what makes them show up in the index from then
on. This mirrors exactly how every person-related setup page already
works today (search PCO, then the record you create is the local list
entry) rather than inventing a "browse the whole church database" feature
this app has never offered and PCO's own People directory already does
better.

## Concrete architecture

### Routes (new file `internal/server/controllers/song_library.go`)

```
group.Get("/songs",              sc.SongLibraryPage)          // index
group.Get("/songs/:song_id",     sc.SongDetailPage)           // detail
group.Post("/songs/:song_id/roles",             sc.AddSongDetailRole)
group.Delete("/songs/:song_id/roles/:id",       sc.DeleteSongDetailRole)
group.Post("/songs/:song_id/tags",              sc.AddSongDetailTag)
group.Delete("/songs/:song_id/tags/:id",        sc.DeleteSongDetailTag)
group.Post("/songs/:song_id/contexts",          sc.AddSongDetailContext)
group.Delete("/songs/:song_id/contexts/:id",    sc.DeleteSongDetailContext)
group.Post("/songs/:song_id/relationships",     sc.AddSongDetailRelationship)
group.Delete("/songs/:song_id/relationships/:id", sc.DeleteSongDetailRelationship)
group.Post("/songs/:song_id/preferences",       sc.AddSongDetailPreference)
group.Delete("/songs/:song_id/preferences/:id", sc.DeleteSongDetailPreference)
group.Post("/songs/:song_id/vocal-range",       sc.SetSongDetailVocalRange)
group.Delete("/songs/:song_id/vocal-range/:id", sc.DeleteSongDetailVocalRange)
group.Post("/songs/:song_id/transition-notes",  sc.AddSongDetailTransitionNote)
group.Delete("/songs/:song_id/transition-notes/:id", sc.DeleteSongDetailTransitionNote)
```

`/scheduling/song-tags/bulk` and `/scheduling/song-contexts/bulk`
(`BulkCreateSongTag`/`BulkCreateSongContext`) **stay exactly where they
are, unchanged** - see "Bulk operations" below for why they need to move
conceptually (onto the new index page) but not literally (their route/
handler signature doesn't change, only which view function the handler
renders at the end).

### Routes (new file `internal/server/controllers/person_library.go`)

```
group.Get("/people",              sc.PersonLibraryPage)        // index
group.Get("/people/:person_id",   sc.PersonDetailPage)         // detail
group.Post("/people/:person_id/relationships",     sc.AddPersonDetailRelationship)
group.Delete("/people/:person_id/relationships/:id", sc.DeletePersonDetailRelationship)
group.Post("/people/:person_id/vocal-range",       sc.SetPersonDetailVocalRange)
group.Delete("/people/:person_id/vocal-range/:id", sc.DeletePersonDetailVocalRange)
group.Post("/people/:person_id/preferences",       sc.AddPersonDetailPreference)
group.Delete("/people/:person_id/preferences/:id", sc.DeletePersonDetailPreference)
```

Both new controller files own only page assembly and the song/person-
scoped mutation-response rendering; the actual `scheduling.Store` reads/
writes are the same `ListSongRelationships`/`CreateSongRelationship`/etc.
methods the flat pages already call (most already accept a `songID`/
`personID` filter - e.g. `ListSongRelationships(ctx, orgID, songID)`,
`ListSongPersonPreferences(ctx, orgID, songID, personID)`,
`ListPersonRelationships(ctx, orgID, personID)` - so no store changes are
needed at all, just calling them with a real id instead of `""`).

### Views

- `internal/views/scheduling/song_library.templ` - `SongLibraryIndex`
  (lightweight per-song rows/cards: title, a handful of summary badges -
  role chips, context chips, tag count - each linking to
  `/scheduling/songs/:id`), plus the relocated bulk-add-tag/bulk-add-
  context forms and the title/tag/context filter box (adapting
  `filterSongTagCards`/`filterSongContextCards`,
  `web/static/js/scheduling-song-tags-contexts.js`, to the new lighter row
  markup's own `data-search` attributes).
- `internal/views/scheduling/song_detail.templ` - `SongDetailPage` (the
  full aggregate: arrangement info/structure/chord chart/attachments,
  roles as chips + inline add, tags as chips + inline add
  (`SongTagCard`'s markup, minus its bulk-select checkbox, which moves to
  the index), contexts as chips + inline add (`SongContextCard`, same
  trim), relationships to other songs + inline add (song B picked via a
  plain `<select>` of `fetchActiveSongs`, matching every existing setup
  page and the Song Roles bucket plan's own recommendation - no
  `SongPickerField` exists and neither prior redesign needed one), which
  people are preferred/discouraged for this song + inline add
  (`PersonPickerField` for the person side), stored vocal range + inline
  add/edit (reusing `SongVocalRanges`' form fields), and the transition-
  notes/debrief section (adapted from `transitionNotesSection`). A small
  "Open in Planning Center" deep link is a natural, cheap addition here,
  following `pcoPlanURL`'s precedent (`internal/views/scheduling/links.go`)
  - a `pcoSongURL` helper alongside it.
- `internal/views/scheduling/person_library.templ` - `PersonLibraryIndex`
  (lightweight rows over the local-data-derived person set described
  above, plus the `PersonPickerField` search box to reach anyone new).
- `internal/views/scheduling/person_detail.templ` - `PersonDetailPage`:
  relationships to other people + inline add, stored vocal range + inline
  add/edit (`personVocalRangeLine`'s markup, the one genuinely reusable
  piece of `personDetailCard`), and which songs this person is preferred/
  discouraged for + inline add (song side picked from a plain `<select>`
  of `fetchActiveSongs`). Deliberately **not** attempting to show
  assignment status, conflict warnings, or team/position info - those are
  inherently plan-date-bound (see "Person side - almost none of it is
  library-level" above); a page that faked them in without a real
  occurrence to check against would be actively misleading, not just
  incomplete.

## Disposition of the 10 existing flat pages

**Revised after Phases 1-2 shipped: none of the ten flat pages retire.
All ten stay, permanently, alongside the two new aggregate pages.**

The paragraph and per-page call below are what this section originally
said, before that reversal - kept rather than deleted, since the reasoning
in "why they'd retire" is still correct for the use case it was solving,
just incomplete about a second use case that argues the other way. Tested
against CLAUDE.md's friction-reduction question - "is this the fast,
obvious way to do the thing" - visiting up to seven separate pages to
fully understand *one* song clearly fails that test once a single
aggregate page exists. That's still true, and is exactly what the two new
pages are for. What that framing missed: a flat page solves a different,
equally real task the aggregate pages make *worse*, not better -
**sweeping through the whole library/roster assigning one relationship
type at a time** ("go set every song's difficulty role," "go mark every
person's vocal range"), especially during initial setup, when there's a
batch of everything to fill in at once rather than one thing to look up.
A flat page is one continuous scan-and-fill pass with zero navigation
between entries; the identical task done one song/person at a time via
the new detail pages means clicking into dozens or hundreds of individual
pages in sequence - by CLAUDE.md's own friction-reduction question,
*that's* the slow, non-obvious way to do a bulk pass. Retiring the flat
pages would have fixed the single-entity-lookup friction this doc set out
to solve by introducing a worse friction for the batch-entry case, net
negative rather than net positive. Resolution: keep both - the flat pages
as the batch-entry tool, the two new pages as the single-entity aggregate
view, permanently coexisting rather than one superseding the other. The
Setup nav dropdown now carries all twelve links (the original ten plus
Songs/People) rather than the four links a full retirement would have
left.

Per-page call, id of what would have changed (now moot - every one of
these keeps its existing flat page exactly as-is, additionally gaining an
inline section covering the identical data on the new song/person detail
page; both write through the same `scheduling.Store` methods, so there's
no "which one is authoritative" concern - neither is a draft or cache of
the other):

- **Song Relationships, Song Roles, Song Vocal Range, and the "Song"
  half of Song/Person Preferences** - would have been **retired**, fully
  replaced by inline sections on the new song detail page. No
  bulk-operation precedent exists on any of these three today, so there
  was no tension to resolve for them either way.
- **Song Tags, Song Contexts** - would have been **subsumed, not just
  retired**: their existing card-per-song content
  (`SongTagCard`/`SongContextCard`) also appears on the song detail page
  as two of its sections now, same as the others above - but their
  pages' bulk-add forms stay exactly where they are (see "Bulk
  operations" below - the relocation described there never happened).
- **Person Relationships, Vocal Range (person), and the "Person" half of
  Song/Person Preferences** - would have been **retired**, replaced by
  inline sections on the new person detail page.
- **Song Tag Suggestions** - **kept standalone, unchanged**, same call
  as originally, independent of the reversal above - it was never a
  retirement candidate. It's not a
  "look up one song's data" page at all - it's a cross-song discovery
  queue (shared-title-word grouping across the whole library, surfaced
  conditionally in the nav only when something's pending, same
  "actionable inbox" idiom as elsewhere in this app). Forcing it onto a
  per-song page would break the thing that makes it useful: seeing
  several songs that share a word *together*, in one place. (Once the
  song detail page exists, a per-song "Apply" affordance could
  additionally appear there for a song already known to have a pending
  suggestion - a nice-to-have worth a follow-up note, not a requirement
  of this plan.)
- **Position Preferences** - **kept standalone, explicitly out of
  scope**, same call as originally, independent of the reversal above -
  it was never a retirement candidate either. It fails the "this is the
  same shape as the other nine" test on two independent grounds: (1) it's
  not local data at all - `PersonTeamPositionAssignment` lives entirely
  in PCO, which is why `buildOnboardingChecklist` already marks it
  `Trackable: false`; (2) there is no cheap way to ask PCO "this person's
  preferences across every position" - `GetPersonTeamPositionAssignments`
  is scoped by team+position, not by person, so surfacing it on a
  person's detail page would mean fanning out across every team/position
  in the org on every page load, a materially different and riskier PCO
  cost than the local-DB reads every other page in this plan makes. Left
  as an open question below rather than silently dropped.

Net: **all ten pages stay**, unchanged, permanently. The two new pages
are purely additive - a second, complementary way in for the
single-entity lookup/edit case, not a replacement for the flat pages'
batch-entry case.

### Bulk operations

`BulkCreateSongTag`/`BulkCreateSongContext` are the only bulk-apply-to-
many-songs workflows in this codebase (confirmed - no other setup page
has one), and per the reversal above, they stay exactly where they are -
on the existing Song Tags/Song Contexts flat pages, unchanged. The
original version of this section (see git history if curious) had planned
to relocate them onto the new song library index page, reasoning that Song
Tags/Song Contexts already prove bulk-ops and a card-per-entity layout
coexist on one page, so the same pattern could relocate onto the index.
That relocation never happened: Phase 1's build (confirmed in its own
report) deliberately left `song_tags.go`/`sets_contexts.go` untouched and
gave the new library index no bulk-add UI of its own - a user who wants
to bulk-apply a tag/context to many songs at once still uses the existing
Song Tags/Song Contexts page for that; the library index and detail page
cover the single-song browse/edit workflow only, consistent with the
"flat pages stay for batch work" resolution above.

## Interaction with the Song Roles bucket redesign

`docs/plans/shipped/song-roles-bucket-redesign-plan.md` has since shipped:
the flat Song Roles setup page was reshaped into one card per role bucket
(Opener, Response, ...) rather than one row per tag, in
`internal/views/scheduling/song_roles.templ`
(`SongRoles`/`SongRoleBuckets`/`SongRoleBucketCard`) and
`internal/server/controllers/sets_roles.go`
(`songRolesByRoleCode` groups a flat `[]scheduling.SongRole` by role code -
`ListSongRoleTypes`/`ListSongRoles` under the hood, unchanged). If this
plan proceeds, that page's own bucket-per-role cards are the direct
building block for the song *library index* page's "browse by role" view
(which needs exactly the same "by role, not by song" grouping) - reuse
`songRolesByRoleCode`/`SongRoleBucketCard` rather than duplicating that
grouping logic. This plan still retires the flat/bucketed Song Roles setup
page itself once the library index replaces it, same as originally
planned - only the shape of what's being retired changed (buckets rather
than a flat table), not the retirement itself.

## Suggested build order (phased, mirroring how multi-part plans in
`docs/plans/shipped/` get sequenced)

1. **Song library index + song detail page** - shipped. Fully additive -
   new routes/views/controllers only, nothing existing touched or
   removed.
2. **People library index + person detail page** - shipped. Same additive
   approach, including the local-data-derived index population described
   above.
3. **Retirement pass** - **dropped, not attempted**, per the reversal in
   "Disposition of the 10 existing flat pages" above. What this step
   originally specified (repointing `navMenuContent` at the two new pages
   instead of the eight now-not-retiring links, updating
   `buildOnboardingChecklist` hrefs, collapsing the eight-page Help-dialog
   chain, repointing `BulkCreateSongTag`/`BulkCreateSongContext`'s render
   call, deleting the eight flat pages' routes/handlers/templ files/JS)
   did not happen and should not happen - all ten flat pages, their Help
   dialogs, their bulk forms, and `buildOnboardingChecklist`'s existing
   hrefs stay exactly as they were before this plan. **What actually
   shipped in this step's place**: a small, purely additive nav-link
   change (`internal/views/layout.templ`'s `navMenuContent`) adding
   "Songs" and "People" links pointing at the two new pages, alongside -
   not instead of - the original ten links.
4. Update `README.md`'s feature list per CLAUDE.md's "Keeping the docs
   current" section (the two new pages are a real user-facing feature) -
   **not yet done**, pending sign-off that the two new pages are staying
   as shipped.

## Open questions

- **Position Preferences** - confirmed out of scope above for a real
  technical reason (no per-person PCO query exists), but worth a
  deliberate call on whether a person's detail page should at least
  *link out* to Position Preferences pre-filtered/scrolled to that
  person, versus leaving it fully disconnected. No default picked here -
  still open, independent of the retirement reversal above.
- **Exact visual shape of the library index rows** - **resolved**: card
  grid, visually consistent with the detail page's own cards and with
  this app's existing card-per-entity setup pages. Built this way in both
  Phase 1 and Phase 2.
- **Whether the onboarding checklist's per-item hrefs should deep-link to
  a specific section of the new pages** - **moot now**: since the flat
  pages the checklist already links to aren't retiring, there's no
  pressure to repoint those hrefs at all. Still an open, low-priority
  product-polish idea (the checklist *could* additionally point at the
  new pages for some items) but no longer something blocking anything.

## Verification (once built)

- `go build ./...` (empty output = success) - passing as of both phases
  shipping.
- `make test` - passing.
- Live: confirm the new song/person detail pages' create/edit/delete
  actions work correctly and match what the corresponding flat page does
  for the same data; confirm the *existing* flat pages, their Help
  dialogs, and their bulk-add-tag/bulk-add-context forms are still fully
  functional and unchanged (there should be zero behavior difference on
  any of the original ten pages - this plan added two pages, it didn't
  modify any existing one); confirm Build's quick-add actions
  (tag/relate/role/vocal-range) are untouched; confirm no new PCO call
  was introduced for the People index (check `docs/pco-api-risks.md`
  needs no update - the whole point of the local-data-derived scoping
  above is that it stays PCO-call-free beyond the existing batched
  name/avatar lookup).
