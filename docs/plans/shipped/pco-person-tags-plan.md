# Plan: PCO person tags for roster candidate matching

**Status: shipped 2026-09-20 (Option A only).** Option A (read-only
"Player Tags" surfacing) is built and merged to `main` (`7e9b248`/
`2bfc1c2`) - see "Option A implementation spec" near the end for the exact
file list. Options B/C/D remain research-only, deliberately not committed
- this doc stays in `shipped/` for Option A's own design record, but
revisit it (not `not-started/`) if B/C/D ever get picked up, since most of
their own research (live-verified PCO facts, open questions) already
lives in this same file. Prompted by a 2026-09-19 follow-on to
`docs/plans/shipped/pco-native-tags-plan.md` (the Song-scoped "PCO
Tags" feature): once that plan confirmed PCO's real `Tag`/`TagGroup`
mechanism also covers **People** (`TagsFor: "person"` - live-confirmed,
see that plan's Handoff notes; a real example already seen in the wild is
an "Instruments Played" group with tags like "Electric Guitar"), the
obvious next question was whether this app could use a person's
*existing* real PCO tags to help fill a team position, the same "protect
the service" way the Song-side feature helps fill a set.

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

1. ~~**Does `People#index` support `where[tag_ids]=...`/
   `where[tag_group_ids]=...` filtering**~~ - **live-verified 2026-09-20**
   with a throwaway read-only probe (`cmd/persontagsprobe`, built, run
   against the signed-in dev org, deleted after use - same
   build-run-delete lifecycle as the sibling plan's `cmd/tagmatchreport`).
   Result is more nuanced than a yes/no, and **doesn't generalize from the
   Song-side finding the way it looked like it would**:
   - **`people/v2/people?where[tag_ids]=...` (the People product's own
     index) silently ignores the filter** - confirmed by requesting a tag
     nobody real holds and getting back the full unfiltered result set
     rather than zero rows. Don't build against this endpoint for
     filtering.
   - **`services/v2/people?where[tag_ids]=...` (the Services product's own
     People index, same domain `blockouts.go`/`teamMembers.go` already
     nest under) is real and does filter** - but takes PCO's JSON:API
     bracket-array query syntax (`where[tag_ids][]=<id>`), not a
     comma-joined value the way `Songs#index`'s `where[song_tag_ids]`
     accepts; the comma-joined form 400s outright ("Tag ids must be an
     array"). Confirmed both directions: a tag nobody holds correctly
     returned 0 rows, and the one real assigned tag in the probed org
     correctly returned exactly the one person holding it. **This is the
     endpoint/param-shape Option B needs to build against** -
     `services/v2/people`, not `people/v2/people`, and go-pco's future
     `PeopleParams`-equivalent for this will need its own array-encoding
     path (can't reuse `SongsParams.SongTagIDs`'s comma-join helper
     as-is).
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
4. ~~**How well-populated is a real org's person-tag vocabulary in
   practice?**~~ - **live-verified 2026-09-20**, same probe as above,
   against the dev org tied to the currently-signed-in session (a small
   org - 5 people total, so treat this as a shape check, not a
   statistically meaningful adoption number for real production orgs):
   real, well-formed person-scoped tag vocabulary exists (`Instruments
   Played`: Acoustic/Bass/Electric Guitar, Drums, Keys, Organ, Percussion,
   Piano; `Vocal Part`: Alto/Bass/Soprano/Tenor; `Audio/Visual`:
   Audio/Camera/Lights/Producer/Video/Switcher - all `allow_multiple:
   true`, none `required`), but only 1 of the 5 people sampled actually
   had any tag assigned. Consistent with the doc's own framing: Option A
   has zero downside either way (renders nothing for an untagged person),
   but this is a real signal that a given org's actual adoption will vary
   a lot and Option B/C's payoff is genuinely adoption-dependent, not a
   given.
   - **Bonus confirmation, unblocks Option A directly**: the same probe
     hit `GET /services/v2/people/{id}/tags` (the path Option A's
     `GetPersonTags` needs, mirroring `GetSongTags`'s
     `/services/v2/songs/{id}/tags`) and got a real, correct response back
     (the one tagged person's real assignment) - this was previously only
     inferred by analogy to `blockouts.go`/`teamMembers.go`'s path
     nesting, not confirmed; it's confirmed now.

## Naming note

**Decided, 2026-09-20: "Player Tags."** The sibling plan's Song-scoped
feature already owns "PCO Tags" unqualified on the Build page, so this
needed a visually distinct label. "PCO Person Tags" (the other candidate)
reads accurate but redundant next to "PCO Tags" in the same app; "Player
Tags" fits this app's existing scheduling vocabulary (people being
scheduled onto teams/positions), reads naturally next to real observed
tag data ("Instruments Played: Vocals" - "player" isn't instrumentalists-
only), and can't be confused with the Song feature's label at a glance -
same reasoning `VocalRangeDisplay`'s "Vocal range:" label doesn't need a
"PCO" prefix either, since surrounding context already makes the source
clear.

## Suggested phasing

1. ~~**Live-verify open question 4 first**~~ - **done 2026-09-20**, see
   Open Questions above.
2. ~~**Build Option A**~~ - **done 2026-09-20**, see "Option A
   implementation spec" below for exactly what shipped.
3. **Live-verify open question 1** - **done 2026-09-20**, see Open
   Questions above (result: `services/v2/people`, bracket-array
   `where[tag_ids][]=...`, not `people/v2/people`) - then build **Option
   B** (ad hoc filter) using that confirmed shape.
4. **Revisit Option C only if B shows real recurring demand** for a
   specific position/tag pairing - and only after accepting the
   rename-orphan tradeoff the sibling plan already documents.
5. **Investigate Option D (Tag Teams) independently, whenever someone
   has time to probe it live** - not blocking on A-C, since it's a
   completely separate question (an OAuth-surface question, not a
   design question).

## Key files referenced above

- `docs/plans/shipped/pco-native-tags-plan.md` - the Song-scoped
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

## Option A implementation spec (2026-09-20 - built, matches this spec)

File-by-file, read-only, additive - no local table, no assignment, no
touch to `rosters.go`'s existing `person_relationships` matching. Options
B/C/D are explicitly out of scope for this spec.

**Built as of 2026-09-20**, matching this spec with only cosmetic
differences (UI label ended up "Player Tags:" as planned;
`scheduling.PCOTag` is defined fresh in the new file below rather than
reused from a Song-side sibling file, since that sibling hadn't landed in
the checkout this was built against - trivial to dedupe if/when both
land in the same tree). Actual files touched: `go-pco`'s new
`people_tags.go` (`GetPersonTags`, tagged/pushed as v0.26.0),
`internal/scheduling/pco_person_tags.go` (new - `PCOTag`,
`ListPCOPersonTags`), `internal/server/controllers/rosters_builder.go`
(eager `personTagsByPerson` fan-out alongside `blockoutsByPerson`),
`internal/server/controllers/rosters_candidate_warnings.go`
(unconditional on-demand fetch in `computeCandidateWarnings`, ahead of
its plan-date early return), `internal/views/scheduling/roster_types.go`
(`PCOPersonTags` field on both `AssignmentView`/`CandidatePersonView`),
`internal/views/scheduling/person_detail_card.templ` (new
`personTagsLine` component, shared by both call sites),
`internal/views/scheduling/roster_builder.templ`
(`CandidateWarningsFragment` renders it after the conflict badges).
`go build ./...`/`go vet ./...`/`go test ./...` all clean.

**New `go-pco` function** - new file `people_tags.go` (mirrors
`blockouts.go`'s file-per-Services-concern convention, not `tags.go`,
since this is a Person-scoped path):

```go
// personTagsPath is scoped under services/v2/people, not the People app's
// own people/v2/people (peoplePath in people.go) - mirrors blockoutsPath's
// own doc comment (blockouts.go): Services exposes its own "people"
// sub-resource keyed by the same person id, and tags only exist on that
// side of the API for this purpose. Live-confirmed 2026-09-20 (see
// docs/plans/not-started/pco-person-tags-plan.md's Open Questions).
func personTagsPath(personID string) string {
	return fmt.Sprintf("services/v2/people/%s/tags", personID)
}

// GetPersonTags lists personID's currently-assigned tags across all of its
// tag groups - the Person equivalent of GetSongTags (tags.go), reusing
// TagListResponse as-is since PCO's Tag resource doesn't vary by taggable
// type. Live-confirmed 2026-09-20 against a real org.
func GetPersonTags(ctx context.Context, personID string) (response TagListResponse, err error) {
	url := fmt.Sprintf("%s/%s", baseURL, personTagsPath(personID))
	response, err = NewRequest[TagListResponse](ctx, "GET", url, nil)
	return
}
```

No `AssignPersonTags` - not needed for Option A, and open question 2
(write shape) stays unverified.

**New `scheduling.Store` method** - new file
`internal/scheduling/pco_person_tags.go` (mirrors
`pco_song_tags.go`'s "thin passthrough, no local table" framing; reuse
that file's `PCOTag{ID, GroupID, Name}` type as-is once the Song-side
feature is merged, rather than redefining it):

```go
func (s *Store) ListPCOPersonTags(ctx context.Context, personID string) ([]PCOTag, error)
```

**Deliberately NOT cached, and NOT fetched eagerly per-candidate** -
two different reasons stacked, not one:
- Same reasoning `ListPCOSongTags` (sibling file) gives for its own
  uncached choice: must reflect a write back immediately. Moot for
  Option A specifically (no write here yet) but keeps the shape
  consistent for when Option B/D might add one.
- The real reason: a roster's *candidate* pool (unlike the Song
  feature's `FetchActiveSongs`-bounded, cached, org-wide song library)
  has no equivalent size bound - it's exactly the scaling risk
  `docs/pco-api-risks.md` already flags for Team Builder eligibility
  pools, and the reason `rosters_candidate_warnings.go`'s
  `CandidateWarnings` is an on-demand endpoint rather than an eager
  per-render fetch. Don't build a `pcoTagsBySong`-style eager map for
  candidates here; fetch one person's tags only when actually needed
  (see controller touch points below). Caching wouldn't fix this - it
  would just spread an N-candidate fetch storm over a TTL window instead
  of eliminating it.

**Controller touch points** - two call sites, treated differently because
of that same eligibility-pool distinction:

1. **Assigned people (bounded by roster size) - eager**, in
   `internal/server/controllers/rosters_builder.go`'s
   `buildRosterBuilderData`: fan out `ListPCOPersonTags` over the
   assigned-only person id list via `scheduling.RunConcurrently`
   (mirrors how `blockoutsByPerson` is already built there), best-effort
   (a failed fetch just leaves that person's tags empty, same tolerance
   as blockouts). Thread the result into `AssignmentView`.
2. **Candidates (unbounded pool) - on-demand only**, piggybacked on the
   existing "Check for conflicts" click in
   `internal/server/controllers/rosters_candidate_warnings.go`'s
   `computeCandidateWarnings` - do not add a second button for this (see
   CLAUDE.md's friction discipline); fold `ListPCOPersonTags` into that
   function's existing per-click, best-effort lookups alongside the
   blockout/frequency/cross-team checks it already runs.

**View touch points**:
- `internal/views/scheduling/roster_types.go` - add
  `PCOPersonTags []scheduling.PCOTag` to both `AssignmentView` and
  `CandidatePersonView`.
- A small "Player Tags: X, Y" text line, rendering nothing when empty
  (same convention as existing optional detail lines on the roster's
  person-detail card component) - shown for an assigned person's card
  always (eager data), and for a candidate's card once "Check for
  conflicts" has been clicked (on-demand data). No new filter-chip UI,
  no new `internal/views/components` component - Option A is display
  text only.

**What's still open before this is fully trustworthy** (distinct from
the plan-level open questions above, which are now resolved for Option
A's purposes):
- Whether "tags only appear after a click" for candidates specifically
  is the right UX tradeoff (vs. always-visible) is a real product call,
  not resolved here - it's the direct consequence of avoiding the
  eligibility-pool fetch-storm risk, not an accident.
- Once built, confirm the `services/v2/people/{id}/tags` call actually
  renders correctly end-to-end (not just via the standalone probe) before
  considering Option A shipped.
