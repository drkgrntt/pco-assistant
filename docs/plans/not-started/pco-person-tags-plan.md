# Plan: PCO person tags for roster candidate matching

Not started - written up for review before anyone picks it up. Prompted
by a 2026-09-19 follow-on to `docs/plans/in-progress/pco-native-tags-plan.md`
(the Song-scoped "PCO Tags" feature): once that plan confirmed PCO's real
`Tag`/`TagGroup` mechanism also covers **People** (`TagsFor: "person"` -
live-confirmed, see that plan's Handoff notes; a real example already
seen in the wild is an "Instruments Played" group with tags like "Electric
Guitar"), the obvious next question is whether this app could use a
person's *existing* real PCO tags to help fill a team position, the same
"protect the service" way the Song-side feature helps fill a set. This
doc is that question's first real look - research only so far (a
read-only recon pass, no live PCO probing of the Person-specific
endpoints yet), not a committed design.

## Why this is a different, better-shaped problem than it might look

There's a superficially similar, much harder feature already on the
shelf: `docs/plans/not-started/song-difficulty-person-expertise-plan.md`
(a graded, per-position skill/difficulty rating, both song- and
person-side, entirely new local schema). Read that plan before assuming
this one duplicates it - **it doesn't, and the two aren't in competition
for the same slot**:

- That plan's whole schema-design hard part (see its "Why this is harder
  than vocal range" section) is the **join key**: there's no stable local
  position id anywhere in this codebase, only the live-fetched
  `(team_id, team_position_name)` composite, and a graded rating
  fundamentally needs to be keyed on *which position* it's rating.
- PCO person tags don't need that join key to deliver real value, because
  they're not graded and not position-keyed at all - a tag is just "this
  person plays Electric Guitar," full stop, already true and already
  maintained in PCO by whoever manages that org's People product, before
  this app ever touches it. The cheapest real version of this feature
  (Option A below) needs **zero new local schema, zero position-matching
  logic, and zero new "who's allowed to set this" design fork** - it's
  read-only surfacing of data that already exists.
- The two features answer genuinely different questions: PCO tags answer
  "can this person do this at all" (binary, coarse, already populated);
  the sibling plan answers "how good are they at this specific part"
  (graded, per-position, would need to be built from scratch and
  maintained by someone). If this feature ships and turns out to have
  good real-world tag coverage, it's worth revisiting that sibling plan's
  Fork 5 ("is a bespoke schema even needed, or is there a cheaper path")
  with this as a data point - but that's a future cross-reference, not a
  reason to design them together now.

## What's already confirmed (2026-09-19 recon pass, read-only)

- **Roster candidate matching today does no attribute/skill filtering at
  all.** `rosters_builder.go`'s candidate pool comes purely from PCO's own
  `GetPersonTeamPositionAssignments` eligibility list; the only other
  "matching" logic is `rosters.go`'s pairwise `person_relationships`
  (`works_well`/`mentorship`/`avoid_pairing` edges between two named
  people) - never a person *attribute*. There is nothing today for this
  feature to conflict with or replace.
- **No local "person skill" concept exists anywhere** - confirmed by grep
  across `internal/scheduling`. `schedule_preference` and the pairwise
  relationship tables are the only standing person-scoped facts this app
  stores, and neither is a skill/capability concept.
- **`docs/pco-api-risks.md`'s own 2026-09-04 `can_include` sweep already
  spotted this**: `People#index` has a real, includable `tags`
  relationship (confirmed live back then), noted at the time as "nothing
  downstream wants the data" - that's no longer true once this plan
  exists.
- **`GetTagGroups` (go-pco, already shipped in v0.25.0) already returns
  person-scoped groups today, for free** - `TagGroupAttributes.TagsFor`
  is a plain string observed as `"song"` and `"person"` both; the
  existing call doesn't filter by type server-side, this app's own Song
  feature just filters the response down to `"song"` client-side. Listing
  an org's person-scoped tag groups/tags needs **no new `go-pco` work at
  all** - only fetching a *specific person's currently-assigned* tags and
  (if ever needed) *assigning* a tag to a person are unwrapped.
- **`docs/ideas.md`'s closest existing brainstorm item** (~L688-699,
  "Stage density / band-balance check") already proposed something in
  this shape ("a per-position/per-person capability tag") but assumed a
  brand-new local concept would be needed - it wasn't aware PCO already
  has this data. Worth annotating once this plan's direction is decided,
  per CLAUDE.md's "Keeping the docs current" section.
- **PCO's own "Tag Teams" feature** (auto-filling a position from anyone
  holding a tag, mentioned in the Song-tags plan's intro as an aside) is
  **completely uninvestigated** - no mention anywhere in
  `docs/pco-api-risks.md` or go-pco's `docs/services.md` beyond that one
  aside. Not known: whether it's API-accessible at all, or a
  Services-web-UI-only feature (plausible, given this project's existing
  pattern of PCO features that are hand-usable but not OAuth-writable -
  see Needed Positions and Tag Group/Tag creation, both already confirmed
  blocked at the OAuth level). Treat as a real unknown, not something to
  design around yet - see Option D below.

## Design options

Not mutually exclusive - **A is the recommended starting point**, B is a
natural next increment, C and D are real forks that need their own
decisions before building, not defaults.

### Option A: read-only surfacing (recommended first step)

Show a candidate's real PCO person tags (e.g. "Instruments Played:
Electric Guitar, Vocals") somewhere in the roster-builder candidate list
or detail view - purely informational, no filtering logic, no position
matching, no assignment. A scheduler reads it the same way they'd glance
at someone's name; the app does nothing with it algorithmically.

- **Zero design forks.** No join key, no "who sets this" question (PCO's
  own People admin already owns that), no tone risk (it's the org's own
  existing description of someone's abilities, not a new rating this app
  invented).
- **New `go-pco` work**: one new function, `GetPersonTags(ctx, personID)`
  mirroring `GetSongTags` exactly (`tags.go`'s existing shape, just
  swapping the path segment) - `AssignPersonTags`/`assign_tags` on a
  Person is **not needed** for this option at all.
- **App-side work**: a `scheduling.ListPCOPersonTags(ctx, personID)`
  Store method (mirrors the Song-tags plan's `ListPCOSongTags`), and a
  small addition to the roster-builder candidate view to render them.
  Cache-vs-not follows the same reasoning as the Song feature's
  `ListPCOSongTags` - not cached, must stay live for correctness.

### Option B: ad hoc filter (natural second step)

Let the scheduler pick a specific PCO tag or tag group live (from the
already-fetched org vocabulary, same `GetTagGroups` call Option A's
sibling feature already needs) to narrow the candidate list while filling
one position - "show me only people tagged Electric Guitar" as a one-off,
manual filter action, not a saved/remembered setting. No persistent
mapping between a position and a tag is stored anywhere - the director
makes that judgment call fresh each time, which sidesteps the join-key
problem entirely (there's nothing to key against a position, because
nothing is being stored per-position).

- **New `go-pco` work**: confirm live whether `People#index` supports
  `where[tag_ids]=...`/`where[tag_group_ids]=...` the same way
  `Songs#index` does (see Open Questions below - **not yet confirmed,
  don't assume it generalizes** just because it worked for Songs).
- **App-side work**: a filter-chip UI in the roster builder, same
  interaction pattern as the Build page's existing tag filter chips.

### Option C: persistent position → tag mapping (real fork, not a default)

A new local table remembering "this team position expects this tag" (or
tag group), so the candidate list auto-filters/auto-suggests without the
director re-picking a tag every time a position needs filling.

**This reintroduces exactly the join-key problem the sibling
song-difficulty-person-expertise plan already identified as the hard
part of that whole feature** - there is no stable local position id, only
the live `(team_id, team_position_name)` composite, which silently
orphans a mapping if an org renames a position in PCO. Don't re-derive
this design fork independently - read that plan's "Why this is harder
than vocal range" section and its schema sketch's own notes on the
`(team_id, team_position_name)` key before building this option; the
tradeoff is the same one, just applied to a mapping table instead of a
rating table.

Not recommended as a first build - only worth it once Option B proves
real scheduler demand for this specific shortcut, given the rename-orphan
risk it takes on.

### Option D: PCO's native "Tag Teams" auto-fill (unknown - needs investigation, not design, first)

Genuinely unresearched. Before this can even be scoped as an option,
someone needs to find out - live, directly, per CLAUDE.md's
live-PCO-verification guidance, not from assumption - whether PCO's Tag
Teams mechanism has any OAuth-accessible surface at all, or whether it's
a Services-web-UI-only feature (the pattern this project has already hit
twice: Needed Positions and Tag Group/Tag creation are both usable by
hand in PCO's UI but rejected for every OAuth caller). If it turns out to
be UI-only, this option is dead and Options A-C are the whole feature.

## Open questions needing live verification (none of this is assumed true from the Song-side work)

1. **Does `People#index` support `where[tag_ids]=...`/
   `where[tag_group_ids]=...` filtering** the way `Songs#index` does
   (confirmed live for Songs in the sibling plan's Open Question 1)? Not
   yet tested for People - needed for Option B, not Option A.
2. **Does `assign_tags` work the same way for a Person as it does for a
   Song** (`POST /services/v2/people/{id}/assign_tags`, same body shape)?
   Not yet tested, and **not needed for Options A/B at all** - only
   relevant if a future "assign this tag to this person from inside the
   app" workflow gets designed (which would inherit the exact same
   full-replace-per-call behavior and OAuth-CRUD-block constraints the
   Song-side `AssignSongTags`/tag-group-creation findings already
   established - re-verify the shape, don't assume it needs re-deriving
   the *behavior*).
3. **What is PCO's Tag Teams feature, mechanically** - see Option D.
   Completely open.
4. **How well-populated is a real org's person-tag vocabulary in
   practice?** Unlike the Song-side feature (where the match-only report
   proved local data and PCO data were unrelated, but PCO's own tag data
   existed regardless), this feature's entire value depends on people
   actually being tagged in PCO already, consistently enough to be worth
   surfacing. Worth a quick real-data check (how many of an org's active
   team members have any person-scoped tag at all) before investing past
   Option A - a low-adoption org would still see zero downside from
   Option A (it just shows nothing), but Option B/C's value scales
   directly with real tag coverage.

## Naming note

The sibling plan's Song-scoped feature is UI-labeled "PCO Tags" on the
Build page. If this ships too, the two need visually distinct labels so
a scheduler doesn't conflate "tags on a song" with "tags on a person" -
e.g. "PCO Tags" (song) vs. "Player Tags" or "PCO Person Tags" (people).
Exact wording is implementation detail, not decided here - just flagging
that reusing "PCO Tags" unqualified for both would be confusing.

## Suggested phasing

1. **Live-verify open question 4 first** (a quick, direct check against a
   real org - are people actually tagged today) - if the answer is "barely
   anyone," that's worth knowing before writing any code, not after.
2. **Build Option A** (read-only surfacing) - cheapest possible version,
   no forks, immediately useful if question 4 comes back positive.
3. **Live-verify open question 1**, then build **Option B** (ad hoc
   filter) if it checks out.
4. **Revisit Option C only if B shows real recurring demand** for a
   specific position/tag pairing - and only after accepting the
   rename-orphan tradeoff the sibling plan already documents.
5. **Investigate Option D (Tag Teams) independently, whenever someone
   has time to probe it live** - not blocking on A-C, since it's a
   completely separate question (an OAuth-surface question, not a
   design question).

## Key files referenced above

- `docs/plans/in-progress/pco-native-tags-plan.md` - the Song-scoped
  sibling feature this plan reuses `go-pco`'s Tag/TagGroup mechanism,
  CRUD-blocked finding, and full-replace-per-call `assign_tags` behavior
  from. Read its Handoff notes before touching `go-pco` for this feature.
- `docs/plans/not-started/song-difficulty-person-expertise-plan.md` - the
  harder, graded, per-position sibling feature this plan deliberately
  does not try to replace or converge with; read its "Why this is harder
  than vocal range" section before considering Option C.
- `internal/server/controllers/rosters_builder.go` - where Option A/B's
  candidate-list rendering/filtering would actually live.
- `internal/scheduling/rosters.go` - the existing pairwise
  `person_relationships`-based matching this feature sits alongside,
  not inside.
- `/home/drkgrntt/projects/go-pco/tags.go`/`tagGroups.go` - the exact
  shape to mirror for `GetPersonTags` (and, later, `AssignPersonTags` if
  ever needed).
- `docs/pco-api-risks.md` - the 2026-09-04 `can_include` sweep that
  already spotted `People#index`'s `tags` relationship as includable.
- `docs/ideas.md` (~L688-699) - the "Stage density / band-balance check"
  brainstorm entry this feature would fulfill/inform, worth annotating
  once this plan's direction is decided.
