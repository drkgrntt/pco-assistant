# Plan: fix Team/Roster Builder's per-person PCO call volume

**Status: shipped.** Implemented as the local Postgres cache + paced
background sync described below (migration `00033`, `person_data_cache.go`)
- see `docs/pco-api-risks.md`'s changelog entry "Per-person blockout/
cross-team/frequency data cached in Postgres" for what actually shipped.
One deviation from this doc's original design: the `GetPersonPlanPeople`
merge-into-one-table fork (see "One design fork this unlocks" below) could
**not** be live-verified (blocked by sandbox tooling policy at the time),
so it shipped as the conservative two-table fallback this doc already
specified for that case - still worth a real live check per the two open
questions at the bottom of this doc. This doc is kept as-is below as the
original design record, not edited to match the final shipped shape.

Written 2026-09-02, revised same day after a correction: the first draft
of this plan proposed caching as *the* fix. Caching only helps a *second*
hit within its TTL - it does nothing for the very first render of a given
plan/team, which is exactly where the real risk is (see "The cold-start
problem" below). This revision reorders the work so the actual rate-limit
risk gets fixed first, with caching kept as a real but secondary
improvement.

Read `docs/pco-api-risks.md` in full before starting - this plan assumes its
vocabulary (`RunConcurrently`/`PCOFanOutLimit`, the `FetchActiveSongs`
cache pattern, the org-scoping convention, PCO's 100-requests/20-seconds
limit) and adds one new addendum to it once shipped rather than
duplicating that context here.

## Why this is the target

`buildRosterBuilderData` (`internal/server/controllers/rosters_builder.go`)
- the Team Builder tab's full render - makes three PCO calls **per distinct
person** involved in the roster, with no PCO batch endpoint available for
any of them:

- `pco.GetBlockouts(ctx, personID, ...)` - once per relevant person
  (`rosters_builder.go:356`).
- `crossTeamAssignmentsThisWeek(ctx, personID, ...)` - once per relevant
  person (`rosters_builder.go:421`, defined in
  `internal/server/controllers/rosters_warnings.go:201`).
- `otherAssignmentDates(ctx, personID, teamID, teamPositionName, ...)` -
  once per (person, team, position) combo where that person has a
  schedule preference set (`rosters_builder.go:437`, defined in
  `rosters_warnings.go:134`) - can exceed the person count.

That's roughly 2-3 PCO calls per distinct person on top of a handful of
fixed overhead calls. `docs/pco-api-risks.md`'s own estimate for "6 position
groups, ~40 people" is 100-200 calls, and it scales **linearly with the
size of the volunteer pool involved**, unlike everything else in the app
(which is capped, batched, or already cached).

## The cold-start problem (the actual rate-limit risk, fix this first)

**"person involved in the roster" is not "person assigned to the plan" -**
it's every person PCO says is *eligible* for any open position on the
team, whether or not they're actually scheduled. Confirmed in
`views/scheduling/roster_types.go`: `CandidatePersonView` (the "who could
be added" list shown per position) carries `FrequencyWarning`/
`BlockoutWarning`/`CrossTeamWarning`, computed **eagerly, for every
eligible candidate on every open position**, not just for people actually
on the roster. A plan only ever assigns a small, bounded number of
people - but a well-staffed volunteer church can have a large eligibility
pool *per position*, and that pool is exactly what scales with "hundreds
of people." This is the real driver of the 100-200+ call estimate above,
not the (small, bounded-by-the-plan) assigned roster itself.

Two things compound this into a real, not theoretical, rate-limit risk:

- **Concurrency doesn't reduce call count, only wall-clock time** - and
  compressing the *same* number of calls into less time is the wrong
  direction relative to a **rolling 20-second window** limit.
  `RunConcurrently` (bounded to `PCOFanOutLimit` = 10 in-flight) can throw
  a few hundred calls at PCO in a handful of seconds for a large
  candidate pool - well inside one 20s window, well over the 100-request
  ceiling within it.
- **The failure mode is silent, not loud**: per `docs/pco-api-risks.md`'s own
  risk assessment, a 429 mid-render is swallowed by the existing
  best-effort `if err == nil` handling - a director sees candidates with
  no warnings at all, indistinguishable from "no conflicts," rather than
  an error. A larger team doesn't get a slow page or a visible failure -
  it gets silently wrong (incomplete) warning data.

**Caching does not fix this.** A cache only pays off on a repeat hit
within its TTL; the first time any given plan/team is rendered, every one
of these calls still fires live, at the same volume as today. Caching
(below) is worth doing on its own merits, but it's not the fix for this.

### The fix: stop computing candidate warnings eagerly for the whole pool

Same shape as an already-shipped fix in this codebase: `sets_builder.go`'s
"Browse all songs" candidate list used to eagerly compute rich per-song
data (conflicts, tags, roles) for *every* active song on every render;
it's now search-driven (`BuildCandidatesSearch`), computed only for songs
actually matching what's typed, capped at 50 - see `docs/pco-api-risks.md`'s
"'Browse all songs' made search-driven" addendum for the exact precedent
and the one new risk it introduced (worth re-reading, since the caution
below mirrors it).

Apply the same move here:

1. **Eagerly compute warnings only for people actually assigned** to a
   position on this roster - `AssignmentView`'s own `FrequencyWarning`/
   `BlockoutWarning`/`CrossTeamWarning` (unlike `CandidatePersonView`'s,
   these are inherently bounded by how many people are actually on the
   plan, not the eligibility pool) keep working exactly as they do today.
2. **Stop eagerly populating `CandidatePersonView.FrequencyWarning`/
   `BlockoutWarning`/`CrossTeamWarning`** in `buildRosterBuilderData`/
   `buildRosterPositionGroupData` for the full candidate list. The
   candidate list itself (names, schedule preference) still renders -
   only the three PCO-backed warning fields are deferred.
3. **Fetch a candidate's own warnings on demand** instead - the natural
   trigger is whatever UI moment a director actually needs the warning to
   decide whether to add someone (expanding/hovering a candidate row, or
   the add-confirmation step itself), via a new small htmx endpoint that
   takes one `personID` (+ `teamID`/`teamPositionName`/`planID`) and
   returns just that person's three warnings. This bounds the *eager*
   cold-render cost to the assigned roster's own size (small, no matter
   how big the volunteer pool is) and only pays the per-candidate cost
   for candidates someone is actually looking at.

**Caveat to check before committing to the UI shape** (mirrors the "one
genuinely new risk" callout in the "Browse all songs" precedent): if
candidate rows are already all visible on the page (not behind a search
box the way songs are - team eligibility lists are typically much
shorter than a song library), an on-demand fetch triggered by e.g.
`mouseenter` on every row risks firing on scroll/hover for many rows in
quick succession, which could reintroduce a burst. Prefer a trigger that
requires deliberate intent (expanding a row, or only fetching once a
candidate search/filter narrows the list, if one gets added) over
anything that fires passively for every visible row. Verify the actual
current candidate-list UI (`roster_builder.templ` and friends) before
picking the trigger - don't assume it matches the song list's shape.

### If eligibility pools are still large even scoped to "visible enough to matter"

If live testing against a real large-volunteer-pool dev org shows even
the deliberate-intent trigger above still risks bursting (e.g. a director
who expands many rows quickly), add a paced/self-throttled fan-out as a
backstop - same pattern as `SyncSongInfo`'s background sync
(`internal/server/controllers/song_arrangement_cache.go`): batch requests
(`PCOFanOutLimit`-sized) with a short pause between batches, capping
sustained rate at a safe fraction of 100/20s. Don't build this
speculatively - only if real use shows the scope-reduction above isn't
enough on its own.

## Caching: a persisted table, not an in-memory TTL map

An earlier draft of this section proposed an in-process `map[key]
cachedEntry` with a 60s TTL, matching `fetchRecentPlanHistory`'s pattern
(`sets_builder.go`). That's the wrong shape for this data. Blockouts and
serving history don't change minute-to-minute the way "is this song still
active" does, so a 60s TTL barely improves the cold case - most renders
would still miss. The right precedent is instead
`song_arrangement_cache` (migration `00023_song_arrangement_cache.sql`,
`internal/scheduling/song_arrangement_cache.go`) - a **local Postgres
table**, backfilled by a paced background sync
(`internal/server/controllers/song_arrangement_cache.go`'s
`SyncSongInfo`), read through a cache-first resolver
(`resolveArrangementCacheInfo`, `scheduling.go`) that only calls PCO live
on an actual miss. Applying that same architecture to person data gets a
render down to **zero PCO calls** for anyone already cached, with
periodic (not per-render) refresh - directly what makes the "does this
scale to hundreds of people" case tractable, not just faster.

This still stacks on top of the scope-reduction fix above, it doesn't
replace it - caching changes *when* a fetch happens, not *who* it's for.
Warming this cache for the eager candidate pool (job-eligible everyone)
would still mean an enormous background sync for a big volunteer pool -
just moved off the request path instead of eliminated. Scope reduction
bounds the *set* worth tracking (assigned roster + actually-viewed
candidates); this section makes fetches for that bounded set cheap and
non-blocking.

### One design fork this unlocks: merge the two `GetPersonPlanPeople` calls

`crossTeamAssignmentsThisWeek`'s call (`Include: ["plan","team"]`, no team
filter) and `otherAssignmentDates`'s call (`Include: ["plan"]`, filtered
to one `TeamID`) hit the *exact same* PCO endpoint for the same person -
just different filters. A previous draft of this plan deferred merging
them as a stretch idea, reasoned about only as a call-count optimization.
Once this data is being persisted rather than fetched fresh per render,
storing two overlapping datasets per person in two separate tables is
just wasteful (double the sync cost, double the storage, for data that's
mostly the same rows) - so the merge stops being optional and becomes the
natural table design: **one cached table per person, covering all teams**,
that both `crossTeamAssignmentsThisWeek` and `otherAssignmentDates` read
from and filter client-side (by team/position, in `otherAssignmentDates`'
case) exactly as they already filter by date/label today.

**The caveat that made this a "verify live first" item still applies and
now blocks the schema, not just an optimization**: both calls cap at
`PerPage: 25`. Today `otherAssignmentDates` gets up to 25 *team-filtered*
rows; merged into one all-teams call, a person active on several teams
could see fewer relevant rows for any one team within the same 25-row
budget, quietly weakening the frequency warning's accuracy for exactly
the busiest volunteers. Before committing to a single merged table,
confirm live against the dev PCO account (per CLAUDE.md's own guidance -
don't assume): does `GetPersonPlanPeople` accept a `PerPage` above 25 (PCO's
usual per-page ceiling is 100), and if bumped, does that fully offset the
dilution for a genuinely multi-team volunteer? If it doesn't check out,
fall back to two tables (one all-teams, one per-team) instead of forcing
the merge - not worth risking warning accuracy to save one table.

### Proposed schema

No JSONB precedent exists anywhere in this codebase's migrations
(checked) - normalized rows, matching every other table's style, not a
blob column. List-valued data (unlike `song_arrangement_cache`'s scalar
fields) also can't reuse that table's "a row's mere existence means
checked" convention - zero blockouts and never-synced both look like zero
rows. So each cache needs a **separate sync-marker row per person**
(when it was last checked) plus zero-or-more **detail rows** (what was
found):

```sql
-- Blockouts
CREATE TABLE person_blockout_sync (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id)
);
CREATE TABLE person_blockouts (
    organization_id  TEXT NOT NULL,
    person_id        TEXT NOT NULL,
    pco_blockout_id  TEXT NOT NULL,
    starts_at        TIMESTAMPTZ NOT NULL,
    ends_at          TIMESTAMPTZ NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    repeat_frequency TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (organization_id, person_id, pco_blockout_id)
);

-- Plan-people history (all teams - see the merge decision above; add a
-- second, per-team pair instead if the merge doesn't check out live)
CREATE TABLE person_plan_history_sync (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id)
);
CREATE TABLE person_plan_history (
    organization_id    TEXT NOT NULL,
    person_id          TEXT NOT NULL,
    pco_plan_person_id TEXT NOT NULL,
    team_id            TEXT NOT NULL DEFAULT '',
    team_position_name TEXT NOT NULL DEFAULT '',
    plan_id            TEXT NOT NULL,
    sort_date          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, person_id, pco_plan_person_id)
);
```

On each sync for a person, replace their rows wholesale (delete + insert
in one transaction) rather than trying to diff - simplest correct option,
and this data is small per person. Follow whatever transactional
insert/replace pattern this codebase already uses elsewhere in
`scheduling.Store` (e.g. `ImportNewSongsFromPCO`/`RevertSet` in
`internal/scheduling/sets.go`) rather than inventing a new one.

### Read path: always serve from cache, never block a render on PCO

Mirrors `resolveArrangementCacheInfo`'s cache-first shape, with one
addition arrangement info doesn't need: a staleness check, since this
data (unlike bpm/meter, which is effectively permanent) actually goes
stale over real time.

- `frequencyWarning`/`blockoutWarning`/`crossTeamWarning` (the pure
  functions in `rosters_warnings.go`) don't change at all - they keep
  taking whatever data they're handed.
- `crossTeamAssignmentsThisWeek`/`otherAssignmentDates`/the blockout fetch
  become **pure reads of the local tables**, keyed on `(organizationID,
  personID[, teamID])` - no PCO call in the render path at all, cache hit
  or not.
- **On a miss (no sync-marker row for this person yet)**: render without
  that person's warning this one time (same best-effort tolerance this
  page already has for any failed fetch) rather than blocking - and queue
  that person for the background sync below. This is the piece that
  actually matters for the cold-start case: a brand-new org's very first
  Team Builder render never blocks on (or bursts) hundreds of live PCO
  calls; it just shows fewer warnings until the background sync catches
  up, typically within a couple of minutes.
- **On a stale hit** (`synced_at` older than `personCacheTTL`, a small
  number of hours - not 60s, and tune once real usage shows how often
  blockouts/commitments actually change): serve the stale row anyway
  (still correct almost all the time, per the caveat below) and also
  queue that person for a refresh. Never block the render waiting on the
  refresh.

### Fill/refresh: clone `SyncSongInfo`'s architecture, don't invent a new one

This is the same "paced background job, guarded against a duplicate
concurrent pass, progress visible by just re-rendering" shape as
`SyncSongInfo`/`songSyncInProgress`/`songInfoSyncBatchSize`/
`songInfoSyncBatchPause` (`song_arrangement_cache.go`) - reuse those
constants/patterns rather than reinventing pacing math:

- A queue of `(organizationID, personID)` pairs needing a sync (new
  misses + stale hits collected during renders, per above) gets drained
  by a background goroutine, batched (`PCOFanOutLimit`-sized) with a
  pause between batches, same 60%-of-budget reasoning
  `songInfoSyncBatchPause`'s doc comment already spells out.
- Guard against duplicate concurrent syncs the same way
  `songSyncInProgress` does (or literally extend that map to cover these
  two new sync kinds, if a fresh look at the code makes that the more
  natural fit rather than three separate guards).
- No new progress-tracking table needed, same reasoning as
  `buildSongInfoData`'s `CachedCount`/`TotalCount` - a page (or a smaller
  inline badge, since this isn't necessarily worth its own dedicated page
  the way Song Info is) can recompute "how many of the currently-relevant
  people have a synced_at within TTL" live from the two sync-marker
  tables on every render, for free.

### The unattended-cron question, revisited

The stronger version of this - a nightly job that keeps *every* org's
cache warm regardless of whether anyone's actively using the app - was
considered and deliberately deferred (see the conversation this plan
comes from): it needs a PCO access token with nobody logged in to carry
one, which means borrowing and refreshing some org's most-recent
session's token (`auth.Store`'s `sessions` table already has
`access_token`/`refresh_token`/`token_expires_at` per session, but
nothing today picks one for unattended use). That's new mechanism this
codebase doesn't have, and it introduces a subtler risk: a background
sync running *as* a real person now silently competes with that same
person's own live usage for their 100/20s budget. **Not needed for this
plan** - the render-triggered queue-and-batch approach above only ever
runs while someone's actually using a live session's token from a real
request, so it needs no new token-management mechanism at all. Revisit
the unattended version only if staleness genuinely proves to matter for
orgs that go a long time between visits - not speculatively.

## Testing

**Scope-reduction piece**: add/extend coverage confirming
`CandidatePersonView`'s three warning fields are no longer populated by
`buildRosterBuilderData`/`buildRosterPositionGroupData`'s main path, and
that the new on-demand endpoint returns the same values the old eager
path used to compute for a given candidate (a pure-logic comparison, no
live PCO needed - `frequencyWarning`/`blockoutWarning`/`crossTeamWarning`
are already unit-tested pure functions).

**Caching piece**: this is the first read-path feature in this codebase
needing a real DB-backed test, not just `httptest` - `docs/pco-api-risks.md`
already notes the publish-path concurrency fixes hit this same gap ("no
DB-backed test harness anywhere yet") and were verified by manual review
instead. Decide up front whether to invest in that harness now (probably
worth it - this plan's whole read path is now DB-shaped) or fall back to
manual verification the same way the publish-path fixes did; don't
silently skip testing because the existing `httptest`-only pattern
doesn't fit this shape. Whichever is chosen, prove:

1. A miss (no sync-marker row) never blocks the render on a live PCO call
   and queues the person for background sync instead.
2. A stale hit serves the stale row immediately and also queues a
   refresh, rather than blocking.
3. The background sync's batching/pacing math (reusing
   `songSyncBatches`-style helpers) is unit-testable exactly like
   `song_arrangement_cache_test.go` already does for Song Info, with no
   DB or live PCO involved.
4. The wholesale replace-on-sync (delete + insert) leaves exactly one
   sync-marker row and the correct detail rows per person, no
   duplicates/orphans across repeated syncs.

No change needed to the existing pure-logic tests for
`frequencyWarning`/`blockoutWarning`/`crossTeamWarning` - they test the
post-fetch filtering directly and don't touch the cache layer at all.

## Rollout

Real (non-trivial) code change - follow CLAUDE.md's standard sequence:
branch, commit, push, and **wait for explicit "merge and deploy"** before
merging into `main` and deploying - don't merge/deploy on your own
judgment just because it builds and tests pass. The scope-reduction fix
and the caching fix are independent and could ship as two separate
branches/PRs if that's easier to review - scope-reduction is the one that
actually matters for the rate-limit risk, so don't let it get blocked
waiting on caching to also be ready.

After shipping, update `docs/pco-api-risks.md` with a new addendum (matching
this doc's existing addendum style) describing both pieces: what was
made lazy/deferred (and why eager candidate-pool computation was the real
driver, not just an efficiency nit), and what was cached, with the TTL
chosen. This closes out the "Team/position lists and team-member names
remain uncached" gap's per-person half - team/position *lists* and
team-member *names* themselves are a separate, still-open item, so don't
mark that whole gap closed when only the per-person reads were addressed.

## Worth a live PCO check before finalizing the schema (not a blocker to start)

Two open questions, best answered by 10-15 minutes against the signed-in
dev PCO account before the caching section's schema is locked in - see
CLAUDE.md's standing guidance to verify against real PCO behavior rather
than assume:

1. **The merge-vs-two-tables fork above**: does `GetPersonPlanPeople`
   accept a `PerPage` above 25 (PCO's usual per-page ceiling is 100), and
   if so, does that fully offset the per-team dilution a merged all-teams
   query would introduce for someone active on several teams? Answers the
   open question in "One design fork this unlocks" above.
2. **A global/org-wide blockouts listing**: this plan assumes the only
   option is the per-person nested `services/v2/people/{id}/blockouts`
   resource (confirmed in `docs/pco-api-risks.md` to have no batch/`where[id]`
   option). Worth double-checking whether PCO's People product exposes
   blockouts at a top level with a batch filter the way `GetPeople` does
   (see `personNameMapForIDs`'s precedent) - if so, that would eliminate
   the per-person blockout call (and this plan's `person_blockout_sync`/
   `person_blockouts` tables) entirely rather than just caching it.
   Not confirmed either way yet.

Neither blocks starting the scope-reduction work above, which doesn't
depend on either answer.
