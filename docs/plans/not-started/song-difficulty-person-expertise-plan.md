# Plan: song difficulty (per position) + person expertise (per position)

Not built yet - written up for review before anyone picks it up. Prompted
by Derek's worship pastor asking about a way to mark a song's difficulty
and a person's expertise, floating something as simple as t-shirt sizes
(XS-XL) - with both Derek and the worship pastor recognizing up front that
this differs **per position** (a song can be vocally simple but
rhythmically/instrumentally hard; a person can be strong on one
instrument/role and a novice on another). Derek is not ready to build
this - this is a planning/design pass only, in the same "needs a real
decision, not a default" style as this file's siblings.

## Current state

**Nothing today represents a difficulty or skill axis at all**, song-side
or person-side. Confirmed by grep across `internal/scheduling` and
`docs/ideas.md` - no `difficulty`/`expertise`/`skill` table, column, or
helper exists anywhere. The closest analogues:

**The vocal-range precedent** (`docs/plans/shipped/vocal-range-plan.md`,
fully shipped) is the structurally nearest thing, and the ask explicitly
names it as the model to look at. Its shape, exactly:

- `person_vocal_ranges` (migration `00036`): one row per `(organization_id,
  person_id)`, storing an absolute pitch-class+octave pair, self-entered
  by (or on behalf of) the person via a standalone setup page.
- `song_vocal_ranges` (migration `00041`, corrected by `00043`): one row
  per `(organization_id, song_id)`, storing an absolute pitch-class+octave
  pair plus the reference key it was measured against, entered by a
  director/admin via a standalone setup page.
- A **mismatch check** at Build-page render time (`sets_builder.go`'s
  `vocalRangeWarningForSong`), narrowed as of 2026-09-12 to check
  specifically the song's assigned *leader* against the roster, surfaced
  as a `components.Disclosure` badge on the Songs tab, plus a dedicated
  matrix view (`vocal_range_matrix.go`) cross-tabulating people × songs.
- Both sides are keyed by **one axis only** - a song has exactly one
  range, a person has exactly one range - because voice is inherently one
  axis (one instrument: the human voice). There is no position dimension
  anywhere in this schema, because vocal range doesn't need one.

**The "hard" tag brainstorm entries** (`docs/ideas.md` ~L326-341,
"Lead-time warning for new/complex songs" and "Rehearsal complexity
budget") gesture at a coarse, single boolean-ish "hard" tag on a song,
explicitly reusing the existing tag mechanism ("Every input already
exists somewhere in this file's data (hard tag, tracked_new_songs,
key/tempo)"). This is song-side only, not per-position, not person-side,
and not really "shipped" as a concept - it's cited as an input a future
feature could consume, not a thing that exists as a distinct table today.
It's the coarsest possible version of "song difficulty," and worth noting
as a much cheaper fallback design (see fork 5 below).

**The dynamic tag-category pattern** (`internal/scheduling/song_tags.go`,
`song_tag_categories` table, seeded by migrations `00016`/`00039`) is the
fully org-definable mechanism CLAUDE.md/ideas.md point to for "already
possible with zero code changes": a `song_tag_categories` row defines a
category (`theme`, `scripture`, `idea`, `seasonal`, ...), and `song_tags`
holds free-text `value`s under it, scoped per org, with zero code changes
needed to add a new category or value - an org can already invent a
"difficulty" category today and tag songs "Easy"/"Hard" with it.
Crucially, though: `song_tags.value` is **unordered free text** - there's
no `sort_order`/rank column on values the way `song_tag_categories` itself
has one for categories. Nothing today lets the app compare two tag values
("is 'Hard' greater than 'Medium'?") the way a difficulty/expertise
mismatch check fundamentally needs to. That's a real gap this pattern
doesn't close for free - see fork 4.

**The Position/Team model**: there is **no local `positions` table at
all**. Team/position data lives entirely in PCO and is fetched live via
`go-pco` (`pco.PersonTeamPositionAssignmentData`,
`pco.GetPersonTeamPositionAssignments`, `pco.NeededPositionData`).
Everywhere this app groups by "position" today (`rosters_builder.go`'s
`positionGroup`/`groupNeededPositions`), it keys by the composite
`(team_id, team_position_name)` - `team_position_name` is free-text an
org defines inside PCO itself, not a stable local integer id, resolved
fresh from PCO on each relevant render/fetch. There is no synthetic local
position-id table to foreign-key against. Every local table that's ever
needed to represent "a fact scoped to a specific position" (there's
exactly one example: `PersonTeamPositionAssignment.schedule_preference`,
which is itself a PCO-native field, written straight through to PCO, not
stored locally at all) has sidestepped this problem by not needing a
local join key - this feature would be the first local table that
actually needs one.

## Why this is harder than vocal range, specifically

Vocal range is a **1-to-1** axis: one voice, one range, on both the song
side and the person side. That's exactly why `song_vocal_ranges`/
`person_vocal_ranges` could each be a bare `UNIQUE (organization_id,
subject_id)` row.

Difficulty/expertise is **not** 1-to-1 on either side - it's **(song,
position) ↔ (person, position)**, qualitatively wider:

- **Song side**: a song doesn't have one difficulty, it has N difficulty
  ratings, one per position that's relevant to it ("vocally simple,
  rhythmically hard" in the ask's own example means at minimum a
  vocal-position rating and an instrumental-position rating on the same
  song, independently). Its difficulty table can't be keyed `UNIQUE
  (organization_id, song_id)` the way vocal range is - it needs `UNIQUE
  (organization_id, song_id, position_key)`.
- **Person side**: symmetrically, a person has N expertise levels, one
  per position they actually serve in (strong on acoustic guitar, novice
  on drums). Same shape: `UNIQUE (organization_id, person_id,
  position_key)`.
- **The join key itself is the hard part.** There is no local, stable
  position id to hang either table off of - the only thing that
  identifies a position anywhere in this codebase today is the
  live-fetched `(team_id, team_position_name)` pair, and
  `team_position_name` is arbitrary per-org text, not an enum. That
  means either this feature stores its own `position_key` as literally
  `(team_id, team_position_name)` text (mirroring `positionGroup`'s
  existing convention, at the cost of a rating silently orphaning itself
  if an org ever renames a position in PCO, since nothing here would
  notice or migrate it), or it invents a genuinely new local concept - a
  locally-defined, org-managed "role/position category" independent of
  PCO's own position names (closer to how `song_roles`/`RoleOpener` are
  already purely descriptive local tags) - which sidesteps the
  rename-orphaning problem but means a director now maintains two
  position vocabularies that have to be kept mentally in sync by a human,
  not the software.
- **The mismatch check is also structurally new, not just wider.** Vocal
  range's mismatch check is a single scalar comparison. A
  difficulty/expertise mismatch check has to first correctly identify
  *which* position a given roster assignment fills
  (`RosterAssignment.TeamPositionName`, already present on every
  assignment) and then look up *that specific* song-rating and *that
  specific* person-rating for that same position key before comparing -
  three lookups joined on a key that doesn't exist as a first-class thing
  in the schema today. Nothing in this codebase currently crosses
  song-level data and position-level data this way; the closest thing
  (`vocalRangeWarningForSong`'s roster-assignment cross-reference) crosses
  song data against *person* data directly, with position only used
  incidentally (to find the leader), never as a join key into the song's
  own data.

Concretely, for storage: a song plausibly needs N difficulty ratings (one
per position an org cares to rate it for, not necessarily every position
that appears on its plan), and a person plausibly needs N expertise
ratings (one per position they actually serve in), and the position
identifier those rows are keyed on is a design fork in itself (fork 5
below), not something to assume answered by analogy to vocal range.

## Design forks needing a real answer (not a default)

### Fork 1: scale representation

- **Numeric (1-5, or similar)** - simplest to store and compare
  (`SMALLINT`, direct `<`/`>` comparison), but the coldest, most "graded"
  feeling of the three - a volunteer being told they're a "2" on drums
  reads more like a school score than a size label does. No existing
  numeric-scale precedent in this codebase.
- **T-shirt sizes (XS-XL)** - the option the worship pastor actually
  floated. Warmer/softer framing than a raw number, still trivially
  orderable if stored as an enum with an implicit rank. No existing
  precedent, but it's a small, closed, self-explanatory vocabulary every
  org understands the same way without needing customization.
- **Dynamic `song_tag_categories`-style org-definable levels** - reuses
  the existing pattern rather than inventing a third vocabulary, most
  consistent with this codebase's own conventions and CLAUDE.md's
  friction-reduction discipline ("no new UI pattern to learn where one
  already works") - but as noted above, `song_tags`' *values* have no
  ordering today, only categories do. This fork isn't free even if the
  underlying mechanism is reused - it would need a genuinely new
  `sort_order`-bearing values table, not literally reusing `song_tags` as
  it stands. "Ride the existing tag pattern" and "build a bespoke
  ordered-scale table" converge to nearly the same amount of new schema
  once ordering is factored in.

### Fork 2: who sets each side, and by what mechanism

Song difficulty is presumably set by a worship leader/admin - same actor,
same mechanism shape as song vocal range's entry page. Person expertise is
the harder call: vocal range's own precedent is self-reported (a physical
fact about oneself), but a self-reported "I'm an XL on this instrument"
is a different social act than a self-reported range - people are
notoriously bad at rating their own skill accurately, and asymmetrically
reluctant to rate themselves generously. The alternative,
leader-assigned expertise, avoids the self-assessment-accuracy problem
but reintroduces fork 3's "someone is grading me" tone risk, more
directly since it's not even the person's own claim. This might
genuinely need **two different mechanisms** (leader sets song difficulty;
person self-reports their own expertise, possibly leader-overridable)
rather than one symmetric setup page the way vocal range's two sides
could stay mechanically identical.

### Fork 3: tone/product-fit - the volunteer-team concern (take this seriously; not a technical call)

The fork most worth flagging loudly, because it's the one place this
feature differs in *kind*, not just scope, from every warning this app
already ships. "Avoid pairing" is a relational fact (says nothing about
either person's competence). The vocal-range mismatch is a physical fact
(no more a judgment of skill than needing reading glasses is). Neither
implies "this person isn't good enough." A difficulty/expertise mismatch
is different: surfaced as a loud warning, "Bob is rated Novice on this
song's Hard instrumental part" reads unavoidably as a **competence
judgment**, on a **volunteer** team where nobody is being paid to have a
bad week. `docs/ideas.md`'s "protect the team" promise exists precisely
to guard against this kind of friction. A mismatch surfacing with the
same visual weight as the existing hard-conflict badges risks
discouraging/shaming a volunteer who learns they were flagged as "not
qualified," and risks making the *setup* step itself unpleasant - a
leader may be reluctant to honestly rate a volunteer's skill at all,
knowing it produces a visible red flag, which would quietly undermine the
feature's own usefulness (an under-populated or deliberately-inflated
data set is worse than none).

Against that: a softer treatment could serve the same underlying
protect-the-team goal (avoiding a mis-scheduled volunteer walking into a
Sunday they're not ready for is its own form of protecting them) without
the shaming framing - informational only, visible to the scheduler alone
(not roster/candidate-facing views a volunteer might see their own name
against), worded as "may need extra rehearsal time" rather than "not
qualified"/"mismatch." That reframe dovetails with the existing
"Rehearsal complexity budget" brainstorm entry, which already treats
"hard" content as a rehearsal-time-load signal rather than a suitability
gate - see fork 4.

**Not resolved here.** This is a product-tone call for Derek and the
worship pastor, not a technical one - whichever way it lands changes both
the UI treatment (badge severity, who sees it) and arguably the schema
itself (does a "may need more prep" framing even need a strict ordered
scale with a pass/fail comparison, or just a looser signal?).

### Fork 4: where it surfaces - its own feature, or folded into "Rehearsal complexity budget"?

Three live options, not mutually exclusive:

1. **A Team-tab candidate filter/sort** - like the existing
   frequency/blockout warning treatment, surfacing "this candidate's
   expertise on this position" while building a roster.
2. **A Songs-tab per-song badge** - like the vocal-range mismatch badge,
   flagging a difficulty/expertise gap once both a song and its
   leader/assignee are known.
3. **Folded entirely into "Rehearsal complexity budget"**
   (`docs/ideas.md` ~L332-341) instead of being a standalone
   mismatch-warning feature - that idea already names a "hard tag" as one
   of its scoring inputs alongside `tracked_new_songs`, key changes, and
   first-time pairings, and scores a *whole set's* rehearsal load rather
   than flagging one specific person/song pairing. This sidesteps fork
   3's shaming risk almost entirely (a set-level "this week's rehearsal
   load is heavy" signal names no individual), at the cost of losing the
   specific "is *this person* ready for *this* part" signal the original
   ask was actually about - a real scope reduction, not just a UI
   relocation.

### Fork 5: is a bespoke schema even needed, or is there a cheaper path?

A full bespoke (song, position) × (person, position) schema is not the
only path, and probably isn't the cheapest one to start with. Two
materially different tiers exist:

- **Cheap tier**: an org-definable `song_tag_categories` "difficulty"
  category (song side only, zero schema change beyond fork 1's ordering
  gap) plus the coarse "hard tag" `docs/ideas.md` already gestures at,
  feeding straight into "Rehearsal complexity budget" - no per-position
  axis, no person-side expertise concept, no new join key. Close to the
  "zero code change" bar CLAUDE.md/ideas.md hold up for tag categories.
- **Full tier**: the bespoke (song, position) and (person, position)
  tables sketched below - real new schema, a genuinely new
  position-keyed join nothing today does, two new setup pages, and a
  mismatch check.

Whether the full tier is worth building depends entirely on how fork 4
resolves - this is the same "which promise does this serve, and how
directly" test CLAUDE.md's Product direction section names for every
feature, worth explicitly running the full-tier design past once fork 3's
tone question is settled, rather than assuming per-position granularity
is self-evidently worth its schema cost.

## Schema sketch (not a commitment)

Illustrative only, to make the forks above concrete. Assumes (for
illustration) fork 1 resolves to t-shirt sizes and fork 5 resolves to the
full per-position tier - neither is actually decided.

```sql
-- Shared, ordered vocabulary (like song_tag_categories/song_relationship_types)
-- so both song difficulty and person expertise compare against the same scale.
CREATE TABLE skill_levels (
    code       TEXT PRIMARY KEY,   -- 'xs', 's', 'm', 'l', 'xl'
    label      TEXT NOT NULL,      -- 'Beginner', 'Developing', ... (org-facing wording, tone-dependent per fork 3)
    sort_order SMALLINT NOT NULL UNIQUE
);

-- Song-side: N ratings per song, one per position, keyed on the
-- (team_id, team_position_name) pair the rest of the app already uses to
-- identify a position - this is the position_key fork 5's "cheap" path
-- avoids needing at all.
CREATE TABLE song_position_difficulty (
    id                 BIGSERIAL PRIMARY KEY,
    organization_id    TEXT NOT NULL,
    song_id            TEXT NOT NULL,
    team_id            TEXT NOT NULL,
    team_position_name TEXT NOT NULL,
    skill_level        TEXT NOT NULL REFERENCES skill_levels(code),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, song_id, team_id, team_position_name)
);

-- Person-side: N ratings per person, one per position they actually serve.
CREATE TABLE person_position_expertise (
    id                 BIGSERIAL PRIMARY KEY,
    organization_id    TEXT NOT NULL,
    person_id          TEXT NOT NULL,
    team_id            TEXT NOT NULL,
    team_position_name TEXT NOT NULL,
    skill_level        TEXT NOT NULL REFERENCES skill_levels(code),
    self_reported      BOOLEAN NOT NULL DEFAULT true, -- placeholder for fork 2's mechanism question
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, person_id, team_id, team_position_name)
);
```

Notes, all explicitly open per the forks above:

- `skill_levels` unscoped/shared (`song_tag_categories`'s pattern) vs.
  per-org-customizable (a new `organization_id`-scoped variant) is itself
  an unresolved sub-question of fork 1 - sketched here as shared/fixed
  for simplicity, not a recommendation.
- The `(team_id, team_position_name)` composite key is the "ride PCO's
  existing position naming" branch of the join-key problem above; the
  alternative (a locally-defined position vocabulary independent of
  PCO's own team-position names) would replace those two columns with a
  single FK into a new local table, and would need its own setup page to
  maintain that vocabulary.
- No `subject_key`-per-plan concept is needed here the way
  `dismissable-warnings-plan.md`'s sketch needs one - like vocal range,
  both of these are standing facts about a song/person, not per-plan
  state.
- A mismatch-detection function would follow `vocalRangeWarningForSong`'s
  exact shape: given a roster assignment's `(TeamID, TeamPositionName,
  PersonID)` and the song on that row, look up the song's rating and the
  person's rating for that same position key, compare `sort_order`, and
  only render a badge (or, per fork 3, a softer informational line) when
  the person's level sorts below the song's.

## Suggested phasing, if the forks above get resolved

Mirroring `vocal-range-plan.md`'s own cheapest-first, standalone-first
sequencing:

1. **Resolve the product-tone fork (fork 3) first, before any schema
   work** - unlike vocal range's own phasing, where the tone question
   never blocked Phase 1/2, this feature's tone call plausibly changes
   the schema itself (does a loose "informational" signal even need an
   ordered scale, or something coarser), so it isn't safe to sequence
   after storage.
2. **Resolve forks 1/2/4/5 together** - they're interdependent (fork 5's
   "is the full tier worth it" answer depends on fork 4's surfacing
   decision; fork 1's scale choice depends on fork 3's tone decision).
3. If the full per-position tier is chosen: **song difficulty first**
   (standalone, admin-entered, useful immediately even with zero person
   data - same "useful alone" property vocal range's Phase 1 had, just on
   the song side this time since person self-reporting is the more
   socially loaded of the two per fork 2/3).
4. **Person expertise second**, once fork 2's self-report-vs-leader-
   assigned mechanism is settled.
5. **The mismatch/informational signal last**, once both sides have real
   data to compare - like vocal range's Phase 3 came after both Phase 1
   and Phase 2 shipped, not before.
6. If instead the cheap tier (fork 5) is chosen: this collapses to "add a
   difficulty `song_tag_categories` entry (plus, if fork 1 needs
   ordering, a small migration adding `sort_order` to tag values
   generally, which would also benefit every other tag category) and
   fold the signal into the existing Rehearsal complexity budget
   brainstorm item" - a materially smaller build than steps 1-5 above,
   and arguably the one worth trying first regardless of which way forks
   3/4 land, since it's cheap enough to not foreclose the fuller build
   later.

## Key files referenced above

- `docs/ideas.md` - Vision/positioning section (the protect-the-team/
  protect-the-service test) and the "hard"-tag brainstorm entries this
  feature would extend or fold into.
- `docs/plans/shipped/vocal-range-plan.md` - the closest structural
  precedent for schema shape, phasing, and its own unresolved "needs a
  real decision" section's phrasing convention.
- `internal/scheduling/song_vocal_range.go` and
  `internal/scheduling/person_vocal_range.go` - the concrete `Store`
  method/table pattern to mirror for the song-side and person-side
  halves of this feature.
- `internal/scheduling/song_tags.go` - the dynamic, org-definable
  `song_tag_categories`/`song_tags` pattern to evaluate as the cheaper
  alternative (fork 5), including the unordered-values gap it would need
  to close.
- `internal/server/controllers/rosters_builder.go` -
  `positionGroup`/`groupNeededPositions`/`positionID`, the only existing
  precedent for what "a position" means structurally in this app's data
  model (`(team_id, team_position_name)`, live from PCO, no local
  positions table).
