# PCO API risk assessment & mitigation reference

Read this before any change that touches how often or how heavily this app
calls PCO (a new hot-path call, a fix to an existing one, a newly-discovered
risk) - see CLAUDE.md's "PCO API calls" callout. Update it the same way.

This file used to be a pure append-only narrative (one "Addendum" section
per fix, in full paragraphs, growing forever). Restructured 2026-09-04 into
a reference (facts and current state, near the top, kept short) plus a
compact changelog (near the bottom, one entry per fix, no blow-by-blow). If
you're deciding whether a new call site is safe, you want the reference
sections. If you're trying to remember *why* something was built a
particular way, check the changelog before re-deriving it from scratch -
several of these fixes have a non-obvious reason baked in (see "Design
decisions" below especially). Full narrative detail for anything compacted
away lives in git history/blame on this file and the commits it references.

## Why this is a real risk, not a theoretical one

**100 requests / 20 seconds, per authenticated user token** (PCO's docs:
https://api.planningcenteronline.com/docs/overview/rate-limiting). 429 on
breach, with a `Retry-After` header. It's scoped **per token, not
per-process** - every interactive request authenticates as the signed-in
person (`middleware.RequestContext` -> `pco.WithAccessToken`), never a
shared PAT, so one user's heavy page can't rate-limit another org or
another user in the same org. The risk is concentrated: one user, one page
load, can burn a large chunk of their own budget.

Two facts every mitigation below has to account for:
- **Concurrency only compresses wall-clock time, not call count** within
  PCO's rolling 20s window. Fanning out sequential calls out of a slow page
  load is real and worth doing, but it doesn't reduce rate-limit exposure
  by itself - it can even make a burst *more* likely if it crams the same
  total count into a shorter window (this bit `SyncSongInfo`, see
  changelog).
- **A 429 fails silently, not loudly.** Most PCO-backed fetches in this app
  use best-effort `if err == nil` handling (reasonable for graceful
  degradation generally) - so a 429 mid-render doesn't crash the page, it
  silently blanks whatever hadn't fetched yet. That's worse than an error:
  it looks like "nobody has a conflict" when PCO actually cut the app off
  partway through. This is why proactive prevention (caching, batching,
  throttling) matters more here than it would if a failure were at least
  visible.

## Reference pattern for a new cache

CLAUDE.md's Architecture section already documents `FetchActiveSongs`
(`internal/scheduling/pco_songs.go`) as the reference implementation - read
that first. Facts worth adding here that aren't in CLAUDE.md:
- 60s TTL, matching `middleware.navStateTTL`'s reasoning - short enough
  that staleness is never a real complaint, long enough to kill the
  N-times-per-render problem.
- **Fail-safe convention followed by every cache in this app**: a
  *failed* fetch is never cached, only a *successful* one (including a
  legitimate empty/zero-value result) - so a transient PCO blip can't pin a
  stale-or-empty result in for the rest of the TTL window.
- For data that's list-valued rather than a single scalar row (e.g.
  blockouts, plan history), "a row exists" can't mean "already checked" the
  way it does for `song_arrangement_cache`'s one-row-per-song shape (zero
  results and never-synced both look like zero rows). The pattern there is
  two tables: a `*_sync` marker row (when this was last checked) plus
  zero-or-more detail rows (what was found) - see `person_blockout_sync`/
  `person_blockouts` etc., migration `00033`.

## Current state: what's mitigated, what's still open

| Area | Status | Mechanism |
|---|---|---|
| Plan page (Songs/Team tabs both built on every load) | fixed | only the active tab builds; the other lazy-loads via htmx `load once` |
| `personNameMapForIDs` (1 call/person) | fixed | batched `where[id]=...`, 100/call |
| Cross-team/frequency checks (1 call per person-per-group) | fixed | deduped to 1 call per person, then per (person, team, position) |
| Sequential per-item loops (candidates, blockouts, item notes, ...) | fixed | `scheduling.RunConcurrently`, bounded to `PCOFanOutLimit` (10) |
| Song Info bulk sync (~200 calls for a 100-song library) | fixed | detached to a background goroutine, paced batches (10/batch, 10s pause, ~60% of budget) |
| Team Builder candidate eligibility pool (scales with volunteer pool, not the roster) | fixed | candidate warnings computed on-demand (button click), not eagerly for every candidate |
| Team Builder per-person blockout/cross-team/frequency (live call every render) | fixed | local Postgres cache (migration `00033`) + paced background sync, same architecture as Song Info |
| Roster shared-fetch duplication (`GetTeamMembers`/`GetNeededPositions`/`GetTeams` fetched twice per click) | fixed | `rosterSharedPCOData` - fetch once, pass to both scoped builders |
| `GetServiceTypes` (called independently by 3 pages) | fixed | cached `FetchServiceTypes`, 60s TTL, same shape as `FetchActiveSongs` |
| `fetchRecentPlanHistory` (uncached, would've fired per debounced keystroke once candidates went search-driven) | fixed | cached per service type, 60s TTL |
| Home page (`buildHomeData`, sequential per-service-type) | fixed | `RunConcurrently` fan-out |
| Publish/Refresh writes (sequential per song/assignment) | fixed | `RunConcurrently` fan-out, with ordering/error-collection made goroutine-safe (see Design decisions) |
| Sustained-rate ceiling across all traffic on one token | enabled (prod) | go-pco `ConfigureThrottle`, `PCO_THROTTLE_ENABLED=true`, 60 req/20s default, background syncs tagged low-priority |
| A 429 that gets through anyway | enabled (prod) | go-pco `ConfigureRetry`, `PCO_RETRY_ON_429_ENABLED=true`, waits `Retry-After`, 1 retry |
| Visibility into PCO failures | shipped | `pco.SetResponseHook` logs every failure (never a success) through this app's logger |
| **Team/position lists and team-member names** | **still open** | fetched live, uncached, on every roster render - no reported symptom yet, not fixed speculatively |

## Live-verified PCO API facts (worth not re-checking)

- People#index (`GetPeople`) supports batched `where[id]=1,2,3` -
  `PeopleParams.IDs` in go-pco `v0.15.0`+.
- Services v2 blockouts (`services/v2/people/{id}/blockouts`) has **no**
  batch/`where[id]` option - `can_query_by` is just `group_identifier`, so
  concurrency/caching are the only levers, batching isn't available.
- `GetPersonPlanPeople` is called with `PerPage: 25` in both existing call
  sites; whether it tolerates a higher `PerPage` (PCO's usual ceiling is
  100) without losing per-team accuracy for a multi-team volunteer is
  **not yet confirmed** - would let `crossTeamAssignmentsThisWeek` and
  `otherAssignmentDates` share one cached table instead of two if it checks
  out (`docs/pco-roster-cold-start-plan.md` has the full design fork).
- Whether PCO exposes a global/batch blockouts listing (vs. only the
  per-person nested resource above) is also **not yet confirmed** - would
  eliminate the per-person blockout cache entirely if it exists.
- Songs#index supports server-side `where[hidden]` filtering - confirmed
  `true`/`false` return different, correct `total_count`s.
- **Arrangements#index/#show supports `include=keys`** - confirmed live
  against a real song/arrangement (`meta.can_include` lists `keys`,
  `sections`; `?include=keys` returns the arrangement's Key resource(s)
  sideloaded in `included`). Not yet used:
  `fetchAndCacheArrangementInfo` (`scheduling.go`) still makes two
  sequential calls per song (`GetArrangements` then `GetKeys`) - one
  `include=keys` call would do both, halving Song Info's whole-library
  sync cost (the "~200 calls for a 100-song library" figure). Needs a
  small go-pco change first: `ArrangementsParams` has no `Include` field
  today, unlike `PeopleParams`/`TeamMembersParams`/
  `PersonPlanPeopleParams`, which already support one.
- **Items#index supports `include=item_notes`** (also `song`,
  `arrangement`, `key`, among others - confirmed live via `meta.can_include`
  on a real plan's items) - **used** (2026-09-04, Phase 1c): `buildSetBuilderData`
  (`sets_builder.go`) now calls `GetItems(..., &pco.ItemsParams{Include:
  []string{"item_notes"}})` and `renderServiceOrder`/`buildSongNoteViews`
  read every published song's notes out of a `map[itemID][]pco.ItemNoteData`
  built once from that response's `Included` array
  (`itemNotesFromIncluded`), instead of one `GetItemNotes` call per
  published song. `buildSongNoteViews` is now a pure function with no PCO
  call of its own; `renderServiceOrder`'s `RunConcurrently` fan-out is kept
  regardless, since `resolveArrangementCacheInfo`'s cache-miss path
  (`GetArrangements` + `GetKeys`, still two sequential calls per song as of
  this change - Phase 1b not yet landed) remains a real per-row PCO call
  worth parallelizing.
- **NeededPositions#index and TeamMembers#index both support
  `include=team`** (confirmed live) - not yet used:
  `fetchRosterSharedPCOData` (`rosters_scoped.go`) makes exactly 3 calls
  per plan (`GetTeamMembers`, `GetNeededPositions`, `GetTeams`), and
  `GetTeams`' only use anywhere in this codebase is resolving bare team
  IDs to names for the other two. Sideloading `team` on the
  `NeededPositions` call already being made eliminates the standalone
  `GetTeams` call - on every Team Builder render and every roster
  add/remove/preference-change mutation (`rosters_detail.go`).
  `NeededPositionsParams` has no `Include` field today either.
- **Checked and ruled out**: `TeamMembers#index`'s `include=person` looked
  like it might replace `personNameMapForIDs`' batched name lookup, but
  that lookup already spans IDs beyond just a plan's team members
  (candidates, local roster assignments too) and is already one batched
  `where[id]=` call - `include` doesn't beat a call that's already
  collapsed via batching.
- JSON:API `include` sideloading is real and already used once
  (`GetPersonPlanPeople`'s `include=plan,team` - see `person_data_cache.go`)
  but has a real limit, worth stating precisely after the two finds above:
  it only sideloads a resource's *own* relationships in the same request,
  it can't merge two genuinely independent top-level list endpoints into
  one call. That's why it can shrink the three-call roster fetch above to
  two (`team` is a relationship of `needed_positions`) but can't merge
  `team_members` and `needed_positions` themselves into one call (neither
  is a relationship of the other) - the still-open Team/position/member
  gap's real fix stays caching, this only trims it from 3 calls to 2.
- **`include` swept exhaustively across every PCO endpoint this app calls**
  (not just the hot paths above) - 2026-09-04, prompted by "is there
  really nothing else." Checked live (`meta.can_include`) against every
  `pco.Get*` this codebase uses. Nothing beyond the three finds above:
  `GetTeamPositions` (`can_include`: `tags`, `team`) has no redundant
  call next to it to cut - its job is resolving a position *name* to an
  *id*, which nothing else fetched carries (`NeededPositionAttributes.
  TeamPositionName` is plain text, not a relationship - see that
  struct's own comment). `GetPersonTeamPositionAssignments` (`person`,
  `team_position`) hits the same "already batched" dead end as
  `TeamMembers`/`person` above - `personNameMapForIDs` right after it
  already covers a broader ID set in one `where[id]=` call.
  `People#index` (`emails`, `tags`, `team_leaders`), `Plans`/`Plan#show`
  (`contributors`, `my_schedules`, `plan_times`, `series`), and
  `ServiceTypes#index` (`time_preference_options`) all have real
  `can_include` values, but none of those relationships are consumed
  anywhere in this app - nothing to gain because nothing downstream
  wants the data. `Songs#index`/`Song#show`, `GetBlockouts`,
  `GetOrganization`, `GetItemNoteCategories` have no `can_include` at
  all; `GetKeys`' is moot once the arrangement fix above ships (that
  call goes away entirely). Don't re-sweep this without a new call site
  appearing first.
- No JSONB precedent anywhere in this codebase's migrations - every table,
  including the caches above, uses normalized rows, not a blob column.

## Design decisions worth knowing before extending this area

- **The throttle is keyed per access token, not one global bucket.** PCO's
  limit is per-token; a process-wide limiter would let one busy org slow
  down another org's unrelated traffic even though each has its own
  independent budget with PCO. Full reasoning in `docs/pco-throttle-plan.md`.
- **No anti-starvation in the throttle, deliberately.** A background sync
  (tagged low priority) losing out to interactive traffic on the same
  token indefinitely is the *intended* outcome under sustained load, not a
  bug - "it'll just take longer" is the accepted tradeoff. Don't "fix"
  this without a real observed case of a background job never completing.
- **The 429 retry only ever retries a 429** - never a bare 5xx (a
  different, broader problem involving write-idempotency nothing has asked
  for) or a 422 (retrying a bad request just wastes a call).
- **The response hook logs failures, never successes.** A single
  legitimate Build page render can be 100-200 calls; logging every one
  would flood the log for no benefit. Only a non-2xx gets a line, with a
  429 getting its own distinct, grep-able message.
- **Concurrent publish/write loops preserve ordering structurally, not by
  convention**: each fan-out (e.g. "all deletions") fully completes before
  the next phase starts, since `RunConcurrently` blocks until its whole
  batch returns - `PublishSet`'s "deletions before creates" and similar
  invariants hold exactly as they did sequentially.
- **No DB-backed test harness exists in this codebase** - the publish-path
  concurrency fixes (writes, not reads) are the one mitigation here with no
  automated test; they were verified by manual review + a live smoke test
  instead. Worth building that harness before the next write-path change
  in this area, not silently re-skipping testing again.

## Changelog

Written 2026-08-31, prompted by the Plan Builder page being slow.
Root cause: `go-pco`'s `NewRequest` makes one synchronous HTTP call per
API call, no batching/concurrency built in, and every loop in
`build.go`/`sets_builder.go`/`rosters_builder.go` was a plain sequential
`for`. A moderately staffed plan (~6 position groups, ~40 people) worked
out to 100-200 sequential round trips per load.

- **Dedupe cross-team/frequency checks per person, not per (person,
  group)** - `crossTeamAssignmentsThisWeek`/`otherAssignmentDates` were
  being called once per position group a person appeared in; deduped to
  once per person (then per person+team+position).
- **Batch `personNameMapForIDs`** - switched from one `GetPerson` call per
  id to one `where[id]=...` batch call (go-pco `v0.15.0`).
- **Only build the active tab's data** - `BuildPlanPage` built both Songs
  and Team tabs on every hit; now only the active one builds, the other
  lazy-loads via htmx. The trigger design went through five iterations
  (hover/focus/click combinations, each fixing a real bug the last one
  caused - full history in git blame on `build_tabs.templ`/
  `scheduling.js`) before settling on plain `load once` plus a
  client-side visibility resync (`syncBuildTabVisibility`) that makes
  hidden-state self-healing regardless of whether a click or a
  still-in-flight background fetch lands last.
- **go-pco request timeout + `Retry-After` surfaced** (`v0.17.0`) -
  `NewRequest` had no `http.Client` timeout at all, so a hung PCO response
  could hang a render indefinitely; fixed with a fixed 30s timeout.
  `RequestError.RetryAfter` added at the same time so a 429's wait hint is
  visible to a caller, though nothing consumed it until the retry feature
  much later. A proactive self-throttle from PCO's own rate-count response
  headers was considered and explicitly declined at this point - no real
  symptom yet to justify it (later built anyway, see the throttle entries
  below, once a real 429 happened).
- **`FetchActiveSongs` cache** - see "Reference pattern" above. Root cause
  was a ~900-song PCO library (mostly archived) pulled live/unfiltered on
  nearly every page view.
- **`RunConcurrently` fan-out** (`scheduling.RunConcurrently`,
  `PCOFanOutLimit` = 10) applied to every remaining independent per-item
  loop found: roster candidate pools, per-person blockout fetches,
  cross-team/frequency warm-ups, per-song item notes. Doesn't reduce call
  count or rate-limit exposure by itself (see "Why this is a real risk"
  above) - purely a wall-clock fix, layered on top of the count-reducing
  fixes above.
- **Song Info sync detached from the request** - `SyncSongInfo` used to
  walk every song needing a cache backfill fully sequentially, inside the
  HTTP request (~200 calls for a 100-song library, over budget in a single
  request). Simply wrapping it in `RunConcurrently` would have made it
  *more* likely to trip a 429, not less, by cramming the same call count
  into a shorter window. Fixed by launching a background goroutine instead
  (own `context.Background()`-derived context, independent of the
  request), paced in batches of 10 with a 10s pause between (caps
  sustained rate at ~60% of budget). Guarded against a duplicate
  concurrent pass per org (`songSyncInProgress`).
- **Roster shared-fetch duplication fixed** - `buildRosterPositionGroupData`
  and `buildRosterLightData` were each independently calling
  `GetTeamMembers`/`GetNeededPositions`/`GetTeams` for the same plan, and
  three handlers called both functions back-to-back on every add/remove
  click (6 calls where 3 would do). Fixed via `rosterSharedPCOData` - fetch
  once, hand the same struct to both.
- **Three residual sequential loops found by a later audit** -
  `personNameMap` (Preferences page's own name resolver, a near-duplicate
  of the already-fixed `personNameMapForIDs`), `combinedPlansForServiceType`
  (Build page's plan picker, up to 20 sequential calls/page), and
  `fetchTeamPositions` (position-preferences page, one call/team) - all
  fanned out via `RunConcurrently`, same pattern as the fix above, just in
  loops that earlier passes hadn't reached yet.
- **`GetServiceTypes` cached** - three pages called it independently with
  no caching between them; unified behind `FetchServiceTypes`, same shape
  as `FetchActiveSongs`.
- **Publish-path writes parallelized** - `PublishSet`/`RefreshSongInfo`/
  `PublishRoster`'s write loops were sequential (self-throttling by
  accident, but slow). Fanned out with the ordering/safety guarantees
  described under "Design decisions" above. The one mitigation in this
  file with no automated test coverage (see that section).
- **"Browse all songs" made search-driven** - the candidate list used to
  compute a full `CandidateSongView` (conflicts, tags, roles, arrangement
  info) for every active song on every render, whether the section was
  open or not. Made search-driven (`BuildCandidatesSearch`, 50-row cap,
  computed only for actual matches). Caught along the way: turning this
  into a per-keystroke (debounced) fetch would have multiplied
  `fetchRecentPlanHistory`'s previously-rare live call - caching that
  (per service type, 60s TTL) shipped in the same change rather than as a
  separate follow-up.
- **App-wide re-audit (2026-09-01)** found one more gap after "nothing
  else is open": `home.go`'s `buildHomeData` ran `GetPlans`/`planStatus`/
  `rosterStatus` sequentially per service type (up to ~15 calls on every
  home page visit for a multi-service-type church). Fixed with the same
  `RunConcurrently` fan-out pattern as everything above.
- **Team Builder's cold-start risk identified as the candidate pool, not
  the roster** - "person involved in the roster" meant every person
  eligible for any open position, not just who's actually assigned; a
  well-staffed church's eligibility pool (not the small, plan-bounded
  assigned roster) is what actually scales with org size, and it was being
  computed eagerly for every candidate on every render. Fixed by scoping
  eager warning computation to assigned people only, with a candidate's
  own warnings fetched on-demand via a "Check for conflicts" button
  (deliberately a real click, not a passive hover/on-visible trigger -
  `positionGroupCard`'s auto-expand-when-short-staffed behavior would
  otherwise re-trigger the same burst this fix removes, just moved from
  render-time to open-time).
- **Per-person blockout/cross-team/frequency data cached in Postgres**
  (`docs/pco-roster-cold-start-plan.md`, shipped) - closes the *per-person*
  half of the still-open gap above. Three new tables (migration `00033`,
  sync-marker + detail rows per the "Reference pattern" section), backed
  by a background sync cloning `SyncSongInfo`'s architecture (paced
  batches, 10/batch, 10s pause - one call per item here instead of two, so
  ~30% of budget, more headroom than Song Info's). A cache miss renders
  without that person's warning once and queues a sync; a stale hit
  (`personCacheTTL`, 4 hours) serves the stale row and also queues a
  refresh - the render never blocks on PCO either way. A render-time "still
  gathering data" notice (`PersonDataSyncPending`) covers the UX gap for a
  brand-new org's first-ever render, so zero-synced doesn't read as
  zero-conflicts. **Team/position lists and team-member names themselves
  remain the one deliberate open gap** - this only closed the per-person
  half.
- **Two manual "refresh this one item now" actions** added (Song Info's
  detail card, Team Builder's person detail card) - both cache designs
  above are read-first/background-sync-second, so a single item's real-world
  correction (a re-keyed song, a newly added blockout) only shows up once
  its turn comes up in the paced queue, which can be minutes away for a
  large org. Each action is a synchronous, one-item, explicit-click fetch -
  bounded cost, same reasoning as every on-demand fetch above. Deliberately
  **not built**: a bulk/"nuclear" reset for either cache - no natural bound
  on its cost, belongs on a settings page if one ever needs it.
- **Login-time `GetServiceTypes` probe** added to `auth.go`'s OAuth
  callback - blocks sign-in for an account with zero PCO Services access
  (a `PerPage: 1` probe, response body discarded, only the 401/403-vs-not
  distinction used). Fails **open** on an ambiguous error (network blip,
  PCO 5xx) - an ambiguous failure shouldn't lock out a legitimate admin.
  Known ceiling, not revisited: PCO exposes no permission-level field, so
  this can only tell "some access" from "none," not Viewer from
  Administrator.
- **Priority-aware throttle shipped** (`docs/pco-throttle-plan.md`, go-pco
  `v0.18.0`) - a per-token priority queue gated by a token bucket, off
  unless opted into (`PCO_THROTTLE_ENABLED=true`). Both background syncs
  tagged low-priority so they never delay interactive traffic once
  enabled. **Enabled in prod 2026-09-03** after a real 429: a user paging
  rapidly through plans via Prev/Next stacked multiple full-page-load call
  bursts inside one rolling 20s window and hit 110/100 requests.
- **A 429 got through the throttle anyway, ~8 minutes after enabling it**
  (same user, a fresh Prev/Next session). Root cause, best understanding:
  the throttle bounds *sustained* rate via a token bucket that starts full
  and refills continuously - correct for its purpose, but not a hard
  rolling-window ceiling in every timing pattern; a burst that drains the
  bucket right at one window's start and catches a full refill at the
  next's can still clear admission and still get a real 429. Investigating
  this also surfaced that **this app logged nothing about PCO failures
  anywhere** - confirmed by grepping every controller for
  `StatusCode`/`RequestError`/`429`. Fixed in go-pco `v0.19.0` (the one
  choke point every call funnels through, so this couldn't be fixed
  per-call-site without touching the ~100+ sites the mitigations above
  were written specifically to avoid touching): `pco.SetResponseHook`
  (logs every failure, never a success - see "Design decisions") and
  `pco.ConfigureRetry` (`PCO_RETRY_ON_429_ENABLED=true`, waits
  `Retry-After`, capped, one retry by default - 429 only, see "Design
  decisions"). **Confirmed resolved 2026-09-03**: the affected user
  reported no further issues; prod logs show the retry actually firing and
  succeeding. No third occurrence.
- **Item notes sideloaded instead of fetched per song** (2026-09-04, Phase
  1c of `docs/pco-call-efficiency-rollout-plan.md`, go-pco `v0.21.0`'s new
  `ItemsParams.Include`). `buildSetBuilderData`'s one `GetItems` call now
  passes `Include: []string{"item_notes"}`; `renderServiceOrder` no longer
  fans a `GetItemNotes` call out per published song via `RunConcurrently` -
  `buildSongNoteViews` reads from a `map[itemID][]pco.ItemNoteData` built
  once (`itemNotesFromIncluded`, same `Included`-array parsing convention
  as `teamNamesFromIncluded`/`planSortDatesFromIncluded`). Kept
  `RunConcurrently` in `renderServiceOrder` rather than collapsing it to a
  sequential loop: `resolveArrangementCacheInfo`'s cache-miss path
  (`GetArrangements` + `GetKeys`) is still a real two-call-per-song fetch
  as of this change (Phase 1b, which would fold that into one `include=
  keys` call, hadn't landed yet) - the fan-out still has genuine
  per-row PCO latency to parallelize.
