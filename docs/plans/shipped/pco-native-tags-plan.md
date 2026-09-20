# Plan: PCO native tags — a new, additive "PCO Tags" feature (not a migration)

**Status: shipped 2026-09-20.** Built end-to-end per this doc's Handoff
notes/build order and merged to `main` (`d4522c5`/`0184b36`): song-scoped
tag-group/tag vocabulary + candidate-list annotation
(`internal/scheduling/pco_song_tags.go`), the assign-picker on the Build
page's candidate detail card and browse/filter chips on the Songs tab
(`sets_builder.go`, `set_builder.templ`), the `AssignPCOSongTag` write
handler (`sets_detail.go`), and the "Song Tags" → "Song Labels" UI rename
this plan's final direction required. The "Verification note" below is
also done - a real `AssignSongTags` write was live-confirmed against the
dev PCO org via a separate, independent read-back
(see `docs/pco-api-risks.md`'s Changelog, 2026-09-20 entry). This doc is
kept as-is below as the original design record.

Prompted by a 2026-09-18 investigation session: PCO Services has its own
real `Tag`/`TagGroup` mechanism (org-editable groups, each `required`
and/or multi-select, attachable to People, Songs/Arrangements, Media, and
Files, filterable in PCO's own UI, and drivable as "Tag Teams" for
auto-filling a position from anyone holding a tag) - this wasn't accounted
for when `song_contexts`/`song_tags`/`song_roles` were designed, and looked
at first like it structurally duplicated a chunk of what those tables do.
**Direction as of 2026-09-19 (final): it doesn't turn out to be worth
unifying.** `song_contexts`/`song_tags` stay exactly as they are,
permanently; this plan now ships a new, separate, purely additive "PCO
Tags" feature (read + assign only, no local table) alongside them. See the
top entry of "Decisions made so far" for why, and the Handoff notes for
what's actually being built. `go-pco`'s read/assign wrapping (v0.25.0) is
done and reused as-is; nothing about that work was wasted by this reversal.

## Decisions made so far

- **2026-09-19 (final): `song_contexts`/`song_tags` stay local permanently
  - not migrated to PCO, not merged with PCO tags at all.** This
  supersedes the 2026-09-18 "both move to option 1" decision and the
  2026-09-19 page-merge decision below (both kept in place for their
  original reasoning, not as current direction). Two reasons, not one:
  1. **Confirmed OAuth-level CRUD block** (next decision below) - this app
     could never offer an in-app "create a new tag" workflow for real PCO
     tags, only for the local tables. Migrating would have been a
     permanent capability downgrade from what the Setup page already does
     today, not a temporary one pending a future unblock.
  2. **Empirical proof the vocabularies don't actually overlap.** A
     purpose-built match-only report (`cmd/tagmatchreport`, built, run
     directly against Resonance Online's real data, then deleted once it
     had answered this question - see the retrospective note under
     Handoff notes) found **0 of 57** real local rows (21 `song_contexts` +
     36 `song_tags`) name-matching any real PCO tag in that org. This
     org's actual PCO tags (`Instruments Played`, etc.) serve a genuinely
     different purpose than this app's local song-planning categories
     (`Big Church`/`Youth`; `Theme`/`Idea` values like `Grace`/`Holy
     Week`/`Cross`). Merging would have either silently orphaned real
     local data or required manually recreating an org's entire local
     vocabulary as PCO tags by hand (17 tags, in this one org's case)
     before this app could keep doing what it already does today.
  - **What actually ships instead**: `song_contexts`/`song_tags` are
    completely unaffected by this plan - same tables, same Store methods,
    same Setup pages, forever (pending some future, unrelated reason to
    touch them). A new, separate, purely additive **"PCO Tags"** feature
    reads and assigns real PCO song-scoped tags from the Build page, live,
    with no local table - a new lens onto real existing org data this app
    currently can't touch at all, not a replacement for anything.
  - **One UI-copy change**: the existing local "Song Tags" page
    (`song_tags`/`song_tag_categories`) gets its **display label renamed
    to "Song Labels"** (page title/nav/breadcrumb text only - the
    `song_tags`/`SongTag`/`song_tag_categories`/`SongTagCategory` table
    and Go identifiers are unchanged; this is copy, not a schema/rename
    refactor) so "tag" doesn't mean two different things in the same app.
    `song_contexts`'s "Song Contexts" label doesn't collide with anything
    and needs no change.
- **2026-09-19: tag-group/tag CRUD is deferred entirely from this build
  pass - superseding the 2026-09-18 "best-effort" framing below - and
  it's now confirmed to be an OAuth-app-level restriction, not a
  per-user permission tier.** `POST /services/v2/tag_groups` 403'd
  ("cannot create a TagGroup") for two separate real PCO accounts on two
  different days (New Life Church 2026-09-18, Resonance Online
  2026-09-19). The second attempt ruled out every softer explanation in
  turn, live: (1) not a network/probe fluke - re-run directly in the
  person's own shell, same result; (2) not a missing-role problem - the
  person confirmed they hold the Services **Administrator** role in this
  exact org; (3) not an empty-org/no-data quirk - the org already has
  several real tag groups (PCO's own default person-scoped ones); (4)
  not a wrong-endpoint guess - creating the *same* tag group by hand
  works fine in PCO's own Services UI as that admin, and the People
  product's namespace has no such resource at all
  (`GET /people/v2/tag_groups` 404s, confirming `/services/v2/tag_groups`
  - which 403s, not 404s - is the right resource, just not writable this
  way); a further check for a *nested*, taggable-type-scoped collection
  (`/services/v2/songs/tag_groups`, `/services/v2/people/tag_groups`,
  `/services/v2/arrangements/tag_groups`, etc. - PCO's own Services UI
  visually splits tag management by type, so this was a real
  possibility, not a wild guess) also came back 404 on every candidate,
  for both `GET` and `POST` - confirming the flat top-level collection
  really is the only real endpoint, and the type split is a UI/attribute
  presentation over `tags_for`, not a different API resource per type.
  That combination - creatable by hand, rejected for every OAuth
  caller regardless of role - is the same shape as `go-pco`'s existing
  documented **Needed Positions** quirk (creatable in PCO's UI, 500s for
  every OAuth-authenticated create attempt tested) - Tag Groups looks
  like the same category of "structural org setting PCO deliberately
  keeps off the public OAuth write surface," not a solvable permission
  question. Just as importantly: **`go-pco` has no
  `CreateTagGroup`/`CreateTag`/`Update*`/`Delete*` functions to call at
  all** - the write-side `go-pco` change deliberately stopped at
  `AssignSongTags` and left creates unwrapped rather than guess a body
  shape from JSON:API convention (the same convention that already
  produced two wrong guesses for `assign_tags` before its real shape was
  found - see Handoff notes). **This build pass therefore ships read +
  assign only: view a song-scoped PCO TagGroup's tags, and
  assign/unassign them on a song - no create/rename/delete of a tag
  group or tag from inside this app**, and this isn't expected to become
  buildable without PCO changing what their API allows, unlike the
  earlier "needs a higher-permission session" framing implied.
- **Superseded 2026-09-19 (later the same day) - see the final decision
  at the top of this list.** Kept for the reasoning that led here, not as
  current direction: ~~`song_contexts` and `song_tags` merge into one app
  concept ("Song Tags"), not two.~~ Once both read straight from PCO,
  there's no PCO-side property distinguishing "a context" from "a tag
  category" - both are just a `TagGroup` scoped to `Song`. Keeping two
  setup pages mapped to different fixed TagGroup names (e.g. always
  "Context" for contexts, always Theme/Scripture/Idea/Seasonal for tags)
  would leave any *other* song-scoped TagGroup an org actually has
  permanently invisible to this app - concretely true today: New Life's
  only real song-scoped group is called "Type" (Chorus/Hymn/
  Instrumental/...), which matches neither bucket. The merged page lists
  every song-scoped TagGroup the org has, each its own section, each
  respecting its own `allow_multiple_selections` - "Type" (or whatever
  groups a real org already has) just shows up, no extra code. This
  replaces the "tag groups get CRUD here too" decision immediately above
  it as this plan's page-shape answer; that decision's CRUD half is
  superseded by the deferral above regardless of page shape.
- **Superseded 2026-09-19 (later the same day) - see the final decision
  at the top of this list.** Kept for the reasoning that led here: ~~the
  auto-migration decision (below) degrades to match-only for this pass,
  for the same CRUD-deferral reason~~ - it can match an existing local
  `song_context`/`song_tag` row to an already-existing real PCO tag by
  name, but can't create a missing PCO-side tag group/tag to hold an
  unmatched one. Running that match-only report against real data is
  exactly what produced the 0/57 result the final decision above cites -
  this decision's own reasoning is what led to building the tool that
  disproved the premise it was built to support.

- **Superseded 2026-09-19 (later the same day) - see the final decision
  at the top of this list.** Kept for the reasoning that led here: ~~2026-09-18:
  `song_contexts` and `song_tags` both move to option 1~~
  (read-through/write-through to PCO's real tag groups, no local table) -
  made once open question 1 below confirmed the batching concern that
  would have disqualified it doesn't hold.
- **2026-09-18: `song_roles` stays local (option 3, unchanged)** - its
  fixed, developer-curated vocabulary is load-bearing (first-song anchors
  reference `RoleOpener`; occasion-reservation warnings reference
  `communion`/`baptism`; onboarding copy assumes these codes exist), and
  that stability is worth more here than the org-extensibility a PCO tag
  group would add. `song_role_types`/`song_roles` are unaffected by this
  plan.
- **2026-09-18: writes are immediate, no draft/Publish step** - a song's
  context/tag assignment is a standing library property edited from the
  Setup page, not a per-plan pick the way order/key/notes are, so there's
  no "set" for it to be staged against or discarded from.
  `schedule_preference` is the precedent this follows.
- **2026-09-18: existing local data auto-matches to PCO tags by name**,
  row-by-row with a tracked success/failure report rather than
  all-or-nothing, and the local tables stay in place until that report
  confirms a clean migration - see open question 3 below for the full
  reasoning (given so little is actually live, manual reconciliation
  wasn't worth the extra step, but a *silent* all-or-nothing migration
  was rejected as the one way this could actually lose data).
- **2026-09-18: the picker enforces `allow_multiple_selections`**
  (single-select groups replace instead of adding) - concrete, live
  evidence this matters: New Life's own "Style" group is single-select,
  and not enforcing it risks the exact silent this-app-says-X/
  PCO-actually-has-Y mismatch the friction-bar framing warns about.
  **`required` enforcement is deferred**, not decided either way, pending
  a live check of whether PCO's API actually rejects a write that leaves
  a required group empty or whether it's purely a PCO-web-UI nag.
- **2026-09-18: tag *groups* get CRUD here too, not just tag
  assignments** - not really new scope: the Setup page already lets an
  org create/rename/delete a `song_context_type` locally, and under
  option 1 that operation naturally becomes create/rename/delete a real
  PCO tag group, so skipping this would regress existing functionality
  (someone needing a new category would have to bounce to PCO's own tag
  admin instead). For `song_tags` specifically this is a capability
  upgrade - today's 4 categories are fixed/developer-curated; PCO-backed
  groups let an org add their own. **Delete needs stronger confirm copy
  than today's** `DeleteSongContextType` gives, though - a PCO tag group
  is a shared, org-wide object (potentially visible to People/Media
  tagging too, and to anyone else at the org using PCO's own tag admin),
  a bigger blast radius than "this deletes every song's local assignment"
  alone communicates.

  **Superseded 2026-09-19**: see the CRUD-deferral and page-merge
  decisions above - this pass ships no tag-group/tag CRUD at all, so the
  delete-confirm-copy question below doesn't apply yet either. Kept here
  for the original reasoning on *why* CRUD matters product-wise, once a
  real Administrator-tier session makes it buildable.

## Current-state comparison

| This app | Shape | PCO equivalent | Verdict |
|---|---|---|---|
| `song_contexts`/`song_context_types` (org-editable: Big church, Youth, other campus, ...) | one flat org-editable vocabulary, single-select in practice | A `TagGroup` on Song, exactly | Reimplements PCO's own mechanism, locally, invisibly to PCO |
| `song_tags`/`song_tag_categories` (theme/scripture/idea/seasonal) | 4 fixed categories, freeform values within each, multi-select | 4 `TagGroup`s PCO already supports natively on Song/Arrangement | Same duplication, plus PCO's tags are explicitly managed objects (create/delete a `Tag`) where ours are implicit/derived from whatever's typed (`DistinctSongTagValues` just distincts existing rows - there's no tag-vocabulary management UI, only usage) |
| `song_roles`/`song_role_types` (opener/response/communion/prayer/reflection/baptism) | fixed, developer-curated vocabulary, multi-select, no Go-side algorithmic special-casing (see that file's own doc comment) | Could be a `TagGroup` too - structurally no different from the two above | Partial overlap; deliberately more rigid (org can't rename/add codes) |
| `person_relationships` (works_well/mentorship/avoid_pairing, optionally directional) | pairwise edge between two specific people | **None** - PCO tags are attributes on one object, not edges between two named objects | Real gap in PCO, not a duplication |
| `song_relationships` (flows_well/same_theme/avoid_pairing) | pairwise edge between two specific songs | **None**, same reason | Real gap in PCO, not a duplication |

`song_roles`/`song_contexts`/`song_tags` share one more thing worth
noting: none of the three tags ever becomes visible or usable from PCO's
own song browser, filter panel, or Tag Teams - a director who wants "every
song tagged Christmas" only gets that view inside this app's Build page.
That's the concrete instance of the friction CLAUDE.md's "Reducing
friction vs. Planning Center" discipline warns about: a second UI pattern
for something PCO already does, with no round-trip back to the system of
record.

**This table's tidy structural alignment turned out not to predict real
overlap** - see the final decision above: 0 of 57 real local rows in
Resonance Online matched any real PCO tag by name. Structural
duplication (same shape) and actual duplication (same org, same data,
same purpose) are different questions - this table answers the first one,
the match-only report answered the second, and the second is the one that
decided the outcome for `song_contexts`/`song_tags`. `song_roles`' row
was never tested the same way (it stays local either way, per its own
decision below), so this caveat is specific to the two rows above it.

`person_relationships`/`song_relationships` need no integration path and
this plan doesn't propose touching them - PCO's data model has no concept
of a pairwise edge between two named records, only attributes on one
record, so there's no PCO mechanism to align with. This is real,
defensible differentiation under docs/ideas.md's "protect the
team"/"protect the service" promises and should stay exactly as it is.

## Why this wasn't obvious going in

`internal/scheduling/song_relationships.go`'s own doc comment frames
`song_tags`/`song_relationships` as the reference pattern for "PCO data
that has no field for this, so store it locally" (see also
docs/ideas.md's "Not in PCO's data model at all - would need local
storage, same pattern as `song_tags`/`song_relationships`" bullet, ~line
278). That framing is still correct for what it's citing (capo notes,
instrumentation needs, "who typically leads this song" genuinely have no
PCO field) - but it quietly assumed `song_tags` itself was also filling a
PCO gap, when it turns out PCO's own tag mechanism is the thing `song_tags`
happens to duplicate. Worth a short annotation in docs/ideas.md pointing
here once this plan's direction is decided, per CLAUDE.md's "Keeping the
docs current" section.

## `go-pco`'s current blocker

`SongData` (go-pco `songs.go`) has no `Relationships` field at all - its
own doc comment says related data "like arrangements/attachments/tags
[are] exposed as action links instead, under paths this SDK doesn't wrap
yet." Nothing in `go-pco` today calls a tag or tag-group endpoint. Any of
the options below needs new `go-pco` wrapping work first (new
resource/response types plus whatever `Get*`/`Create*` calls the chosen
option needs) - not just new code in this app.

`docs/pco-api-risks.md`'s own exhaustive `can_include` sweep (2026-09-04)
confirms `Songs#index`/`Song#show` have **no `can_include` at all** -
consistent with tags being action links, not a sideloadable relationship,
so this isn't a case of sideloading a song's tags into the same request
that fetches the song list. That turned out not to matter in practice -
see open question 1 below, confirmed live: `Songs#index` supports
`where[song_tag_ids]=...` filtering directly, which is the actual cheap
path (filter by tag, not fetch per song).

## Design options, per table

Not a single yes/no decision - each of the three tables can move
independently, and "leave it local" is a legitimate answer for any of
them, not just a placeholder until this plan is finished. **Decided**:
`song_contexts`/`song_tags` → option 1, `song_roles` → option 3 - see
"Decisions made so far" above. Options kept below for the reasoning, not
because the choice is still open.

1. **Read-through, write straight to PCO (no local table at all).** The
   picker reads PCO's real tag groups/tags live (cached the way
   `FetchActiveSongs` caches the song list - session-stable, 60s TTL);
   assigning a tag from the Build page writes straight to PCO immediately,
   same "no draft concept" treatment `PersonTeamPositionAssignment.
   schedule_preference` already gets per CLAUDE.md's Sync model section.
   Removes `song_context_types`/`song_contexts` (and/or
   `song_tag_categories`/`song_tags`) as local tables entirely. Biggest
   friction win - the tag becomes visible/usable in PCO's own UI too - but
   the most new plumbing, and depends on the rate-limit open question
   below not being disqualifying.
2. **One-time/periodic import, keep local storage.** Seed
   `song_context_types`/`song_tag_categories` (and matching `song_tags`/
   `song_contexts` rows) from PCO's existing tag groups on connect or via
   a manual "Import from PCO tags" action, then keep behaving exactly as
   today (local-only, no write-back). Lower risk, no new draft/Publish
   semantics to design, but doesn't fix the "invisible from PCO afterward"
   problem - a director still can't see this app's *new* tag assignments
   inside PCO. Mainly a friction win at first-setup time for orgs that
   already used PCO tags before adopting this app.
3. **Leave as-is.** Legitimate for `song_roles` specifically: it's
   deliberately more rigid than an org-editable tag group (fixed,
   developer-curated vocabulary the app's onboarding/anchor-adjacent
   copy assumes), and migrating it to a real PCO `TagGroup` would hand
   orgs the ability to rename/remove codes this app's own help text
   currently treats as stable. Worth deciding per-table rather than
   assuming all three move together.

## Open questions (real decisions needed, not answered here)

1. ~~**Does a PCO tag/tag-group endpoint batch**~~ - **confirmed live**
   2026-09-18 against the dev org (New Life Church, 933 songs, 15 real
   tag groups already in use) with a throwaway read-only probe (GET-only,
   deleted after use):
   - `GET /services/v2/tag_groups?include=tags` returns every tag group
     across every taggable type (song/arrangement/person/media) plus each
     group's own tags, sideloaded, in one call - the vocabulary side is
     fully batchable, confirming the cheap half of this question outright.
   - There's no single global tag-*assignment* endpoint - `GET
     /services/v2/tags` 404s, and a song's own tags are only reachable
     nested (`GET /services/v2/songs/{id}/tags`, confirmed working, one
     call per song).
   - **But `Songs#index`'s own `can_query_by` (confirmed live on the same
     probe) includes `song_tag_ids`/`song_tag_group_ids`/
     `arrangement_tag_ids`/`arrangement_tag_group_ids`** - meaning
     `where[song_tag_ids]=<id>` filters the *existing* `GET
     /services/v2/songs` call (the one `FetchActiveSongs` already makes)
     directly. This flips the batching direction from what this question
     assumed: instead of fetching per-song assignments to build a
     candidate-list annotation, the cheap shape is filtering the song list
     by tag - and an org typically has far fewer tags (15, here) than
     songs (933, here), so "does this song have any of these N tags" is N
     filtered list calls, not one call per song. A `song_tag_group_ids`/
     `song_tag_ids`-keyed cache (mirroring `FetchActiveSongs`'s own
     cache-aside shape) is the shape to build against, not a per-song
     fetch loop.
   - Also noted in passing: each Song response carries an `assign_tags`
     action link (`.../songs/{id}/assign_tags`) - the likely write
     endpoint for option 1's write side, not yet probed.
   - This resolves the part of this question that could have disqualified
     option 1 outright (an unbatchable per-song cost) - it doesn't.
2. ~~Are PCO tag-group writes immediate or does this need Publish/Discard
   treatment?~~ - **decided 2026-09-18: immediate, no draft** - unlike
   order/key/notes, a song's context/tag assignment isn't scoped to one
   plan under construction: it's edited from the dedicated Song
   Tags/Contexts *setup* page as a standing property of the song in the
   library, not picked mid-Build-session as part of assembling one
   specific set. There's no "set" for a tag change to belong to or be
   discarded from, which makes `schedule_preference` (a standing property
   of a *person*, already immediate-write) the analogous precedent to
   follow, not the Build page's per-occurrence fields.
3. ~~What happens to existing local data on migration~~ - **moot as of
   2026-09-19: there is no migration.** The auto-match-by-name approach
   this question originally settled on is exactly what got built
   (`cmd/tagmatchreport`) and run - it's what produced the 0/57 result
   that led to keeping `song_contexts`/`song_tags` local permanently
   instead (see the final decision at the top of this doc). Kept below
   for the historical reasoning, not as something still to build.
4. ~~Does `song_roles` move at all~~ - **decided 2026-09-18: stays local**
   (see "Decisions made so far" above).
5. ~~Multi-select/required semantics~~ - **decided 2026-09-18**: the
   picker enforces `allow_multiple_selections` (single-select groups
   replace on pick, matching PCO's own field name, confirmed live in open
   question 1's probe). `required` enforcement stays undecided/deferred
   pending a live check of whether PCO's API actually rejects a write
   that leaves a required group empty. See "Decisions made so far" above
   for the full reasoning (this doesn't need a new local concept - once
   option 1 ships, PCO's real `TagGroup` objects, fetched live, become
   this app's category concept outright, replacing
   `song_context_types`/`song_tag_categories`).
6. **Tag-group CRUD's delete confirm copy** - decided that create/rename
   need no new design (they follow the existing Setup-page pattern), but
   delete's confirmation needs to say more than today's
   `DeleteSongContextType` copy does, since a PCO tag group is a shared,
   org-wide object rather than a purely local one. The actual wording is
   implementation detail, not decided here.

## Suggested build order, once the open questions above have real answers

1. ~~Resolve open question 1 live against the dev PCO org first~~ -
   **done** (2026-09-18, see above) - option 1 is feasible at
   candidate-list scale via `where[song_tag_ids]=...` filtering on the
   existing Songs#index call, not a per-song fetch.
2. ~~Wrap the tag/tag-group endpoints in `go-pco`~~ - **done**
   (2026-09-19): `GetTagGroups`/`GetTagGroup`/`GetTags`/`GetSongTags`/
   `AssignSongTags`, plus `SongsParams`'s new `SongTagIDs`/
   `SongTagGroupIDs`/`ArrangementTagIDs`/`ArrangementTagGroupIDs`
   filters, shipped in `go-pco` v0.25.0 (merged to `main`, tagged, pushed
   - this app's `go.mod` already bumped to it). `Create`/`Update`/
   `Delete` for TagGroup/Tag were deliberately left out - see Handoff
   notes below for why this isn't just "not yet built."
3. ~~Pick per-table for `song_contexts`/`song_tags`/`song_roles`~~ -
   **decided 2026-09-18, superseded 2026-09-19**: originally both moved
   to option 1; `song_roles` stays local either way. See the final
   decision at the top of "Decisions made so far" for the current answer
   (`song_contexts`/`song_tags` stay local permanently).
4. ~~Settle the draft/immediate-write question before writing any
   tag-assignment code~~ - **decided 2026-09-18**: immediate, no draft.
   See "Decisions made so far" above.

## Verification note

Before calling any part of this shipped: confirm live (not from docs
alone) that a `Tag`/`TagGroup` write from this app's OAuth session
actually appears in PCO's own tag UI for a real test org, the same way
`song-leader-plan.md`'s `ItemAssignment` sync was confirmed live rather
than trusted from documentation - PCO's behavior has contradicted its own
docs before (see CLAUDE.md's PCO-verification guidance). Also update
`docs/pco-api-risks.md`'s Changelog and Current-state table with whatever
this adds to the app's PCO call volume, per its own standing discipline.

## Handoff notes for implementation

All decisions above are made (as of 2026-09-19); nothing below is a new
open question, just what the next session/agent(s) need to actually
start building. This section was substantially rewritten 2026-09-19 -
the app-side build hadn't started yet as of that rewrite, `go-pco`
wrapping had (see "Suggested build order" above).

**A process note for future live-PCO-verification sessions in this
project**: during today's write-side probing, a probe run through this
session's own sandboxed shell got a generic CloudFront edge block
(`403`, HTML body, "We can't connect to the server for this app...")
against an org where the *same* code, run directly in the account
owner's own shell moments later, worked normally (a real JSON response,
whether success or a real PCO-level error). Don't treat a raw
CloudFront-branded HTML error as a real PCO API response - if one shows
up, retry from the account owner's own shell (the `!` prefix) before
concluding anything about the account, token, or permissions.

**What's live-verified, final state as of 2026-09-19:**

- Read side (2026-09-18): tag-group listing + sideloaded tags
  (`GET /tag_groups?include=tags`), per-song tag listing
  (`GET /songs/{id}/tags`), and `Songs#index`'s
  `where[song_tag_ids]=...`/`where[song_tag_group_ids]=...` filtering -
  all confirmed live, all now wrapped in `go-pco` v0.25.0.
- `assign_tags` request shape (2026-09-18): `POST
  /services/v2/songs/{id}/assign_tags` with body
  `{"data": {"type": "TagGroup", "relationships": {"tags": {"data":
  [{"type": "Tag", "id": "<tag id>"}, ...]}}}}` → `204 No Content`. Not
  the shape that seemed likely from the read side - two more natural
  guesses (`attributes.tag_ids`, a bare array) both failed with distinct
  errors before this one was found. Now wrapped as `go-pco`'s
  `AssignSongTags`.
- `assign_tags` is a **full replace per call, not additive**, confirmed
  live and documented on `AssignSongTags` itself - every call must carry
  the *complete* desired tag-id set for whichever group is being
  changed, multi-select groups included, since there's no incremental
  add/remove call. An empty slice clears the group's assignment
  entirely. Not yet tested: mixing tag ids from two different groups in
  one call - no second song-scoped group was available either day to
  test against; treat "one call per group being changed" as the working
  assumption.
- `required` enforcement: **still genuinely untested** - no
  `required: true` song-scoped group has existed in any org probed so
  far. Since tag-group CRUD is now confirmed blocked at the OAuth level
  (next bullet), there's no way to create one to test with either - this
  stays an open unknown indefinitely, not just "not yet gotten to."
  Build the picker without relying on client-side required-field
  validation; if PCO's API silently accepts an empty required group
  (plausible, given it may only be a PCO-web-UI nag), that's fine
  either way.
- **Tag-group/tag CRUD (create/rename/delete) is confirmed blocked at
  the OAuth-app level, not a permission tier this app could ask a user
  to obtain** - see the CRUD-deferral decision at the top of this doc
  for the full chain of live evidence (two accounts, two days, a
  confirmed Administrator, ruled-out empty-org/wrong-endpoint/nested-path
  explanations). `go-pco` has no `Create`/`Update`/`Delete` functions
  for TagGroup/Tag at all, by design - don't add them by guessing a body
  shape. This is the reason the build below is read + assign only.

**Retrospective: the match-only report that changed direction.** Built as
`cmd/tagmatchreport` (mirroring `cmd/dbseed`'s `APP_ENV=local` guard +
`auth.NewStore(...).MostRecentSession(ctx)` pattern), run directly against
Resonance Online's real data (21 `song_contexts` rows, 36 `song_tags`
rows), checking two different matching rules (context-type labels against
any real PCO Tag name; in-use tag-category labels against real PCO
TagGroup names, then their values against that matched group's own tags).
Result: **0 of 57 rows resolved.** That real number is what's cited in
the final decision above, and it's the reason the plan changed from "merge
into PCO" to "keep both, add PCO Tags as a new lens." The tool has since
been **deleted** - it was migration-preflight tooling for a migration
that isn't happening, so it has no further use; its finding lives on here
and in the top decision instead.

**Architecture for the new, additive "PCO Tags" feature.** Nothing about
`song_contexts.go`, `song_tags.go`, their controllers, or their templates
changes structurally - they're a different, complete, working feature
that happens to sit next to this one. The only touch to that side is the
"Song Labels" display-copy rename noted in the final decision above.

- New file `internal/scheduling/pco_song_tags.go` (naming mirrors
  `pco_songs.go` - a Store file whose methods are thin passthroughs to
  live PCO data, not local-DB queries).
- `scheduling.PCOTagGroup{ID, Name string, AllowMultipleSelections,
  Required bool}` - one PCO TagGroup with `TagsFor == "song"`.
- `scheduling.PCOTag{ID, GroupID, Name string}` - one PCO Tag (either a
  group's available option, from listing a group's sideloaded tags, or
  one currently assigned to a song, from `ListPCOSongTags` - same shape
  either way, since PCO's own `Tag` resource doesn't distinguish
  "available" from "assigned"). Deliberately **not** named `SongTag`/
  `SongTagGroup` - those identifiers stay exactly as they are for the
  local feature; the `PCO` prefix disambiguates in Go the same way "PCO
  Tags" disambiguates in the UI.
- `Store.ListPCOTagGroups(ctx, organizationID) ([]PCOTagGroup, error)` -
  every song-scoped TagGroup, cached ~60s per org (mirrors
  `FetchActiveSongs`'s cache-aside shape - session-stable, same as a song
  library).
- `Store.ListPCOTagGroupTags(ctx, groupID) ([]PCOTag, error)` - a group's
  own available tags, sideloaded via `GetTagGroups`' `include=tags` rather
  than a second call if `ListPCOTagGroups` already fetched it.
- `Store.ListPCOSongTags(ctx, songID) ([]PCOTag, error)` - a song's
  currently-assigned tags. **Not cached** - this must stay live for
  correctness the same way the roster's team-membership cache gets a
  much shorter TTL than `FetchActiveSongs` (see CLAUDE.md's PCO API calls
  section) - assigning a tag is the exact action this needs to reflect
  back immediately.
- `Store.AssignPCOTagGroup(ctx, songID, groupID string, tagIDs []string)
  error` - thin wrapper over `go-pco`'s `AssignSongTags`, `groupID`
  purely for the caller's own bookkeeping (`AssignSongTags` itself infers
  the group from the tag ids). Doc comment must repeat the full-replace
  warning at this layer too - every call site needs to compute the
  complete desired set before calling this, not just forward one delta.
- No `Create`/`Update`/`Delete` - nothing in `go-pco` to call, and
  vocabulary management for this feature always happens in PCO's own UI,
  not this app's.

**File-by-file, additive only - no deletions, no renames of existing
local-feature code beyond the one display-copy change above:**

- `internal/scheduling/pco_song_tags.go` - new file, the Store methods
  above.
- `internal/server/controllers/sets_builder.go` - **add** new fields to
  `candidateLookups` alongside the existing, untouched `contextsBySong`/
  `tagsBySong` (local) fields: `pcoTagGroups []scheduling.PCOTagGroup`,
  `pcoTagsBySong map[string][]scheduling.PCOTag`. Populate using open
  question 1's confirmed cheap shape (`where[song_tag_group_ids]=...`
  filtering on the existing `Songs#index`/`FetchActiveSongs` call) rather
  than a per-song fetch loop - re-check `docs/pco-api-risks.md`'s
  batching guidance before picking the exact call shape, don't assume.
- `internal/server/controllers/sets_detail.go` - **add** a new handler
  (e.g. `AssignPCOSongTag`) alongside the existing, untouched
  `QuickAddSongTag`/`QuickAddSongContext` - computes the full desired
  tag-id set for whichever PCO group changed, then calls
  `store.AssignPCOTagGroup`.
- `internal/server/controllers/scheduling.go` - **add** routes for the
  new assign action; no removals, no changes to existing `/song-tags`/
  `/song-contexts` routes.
- `internal/views/scheduling/set_types.go` - **add** `PCOTagGroups
  []scheduling.PCOTagGroup` to `SetBuilderData` and a per-candidate PCO
  tags field to `CandidateSongView`, alongside the untouched local
  `ContextTypes`/`TagCategories`/`Contexts`/`TagValuesByCategory` fields.
- `internal/views/scheduling/set_builder.templ` - **add** a new, clearly
  labeled "PCO Tags" filter-chip section and a "PCO Tags" block in
  `candidateDetailExtras`, next to (not replacing) the existing local
  Context/Tag chip rows and `Contexts` block. Same single-vs-multi-select
  pill behavior as originally designed (`allow_multiple_selections`
  drives radio-like vs. checkbox-like toggling) - no dedicated
  multi-select component exists in `internal/views/components` to reach
  for, build this by hand the way `SongTagCard`/`SongTagFilterChips`
  already do.
- `internal/views/scheduling/song_tags.templ` +
  `internal/server/controllers/song_tags.go` - **display-copy-only
  rename** to "Song Labels" (page title, nav link text, breadcrumb, in-page
  headings) - no
  structural or behavioral change, and note this in the same change that
  touches `internal/server/middleware/nav.go`'s nav link text if that's
  where the label lives.
- `cmd/dbseed/main.go` - unaffected; optionally (not required) could also
  assign a couple of real PCO tags to seeded songs so the new feature has
  something to show out of the box - a nice-to-have, not required for v1.
- `docs/pco-api-risks.md` - still mandatory once this ships: new call
  pattern (group/tag vocabulary cached ~60s per org; decide
  cached-vs-not for the per-song assigned-tags fetch once the real shape
  chosen for `sets_builder.go` is measured), Changelog entry.
- No new migration, no table drops - there's nothing to drop.

**Build order for what's left:**

1. Build the additive Store methods + Build-page wiring (the file list
   above) as one agent working through it - `sets_builder.go`/
   `set_builder.templ` are still shared touch points with the untouched
   local feature, so it's not split across parallel agents, but the risk
   profile is much lower than the abandoned merge plan: this is pure
   addition, no deletion or rewrite of existing, working local-feature
   code.
2. Update `docs/pco-api-risks.md` once the real per-song fetch shape is
   chosen and measured against a real org's song count.
3. Move this plan doc to `docs/plans/shipped/` once done, fix every
   reference to its filename across the repo if it moves (per CLAUDE.md's
   own convention).
