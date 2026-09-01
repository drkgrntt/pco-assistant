# PCO API risk assessment: rate limiting & Plan page performance

Written 2026-08-31, prompted by the Plan Builder page being slow. Captures a
risk assessment before doing any of the actual mitigation work, so the
findings survive even if we don't get to the fixes in one sitting.

## The rate limit itself

Per PCO's docs (https://api.planningcenteronline.com/docs/overview/rate-limiting):
**100 requests / 20 seconds, per authenticated user token.** 429 on breach,
with a `Retry-After` header.

Two things already work in our favor:
- Every interactive request authenticates as the signed-in person
  (`middleware.RequestContext` -> `pco.WithAccessToken`), never the shared
  PCO PAT - confirmed no controller path uses `PCO_CLIENT_ID`/`PCO_SECRET`.
  So one user's heavy page can't rate-limit another org, or another user in
  the same org.
- No background cron job hits PCO today (`jobs.NewPCOJobs().Register` is an
  empty placeholder) - no shared-token batch risk yet. Worth remembering if
  a PCO-hitting cron job is ever added: it would need its own budget, since
  it'd be a single shared bucket across every org (unlike interactive
  traffic).

The risk is concentrated: **one user, one page load, can burn a large chunk
of their own 100/20s budget.**

## Why the Plan page is slow: sequential N+1, not one slow call

`go-pco`'s `NewRequest` (`pco.go:243` in the go-pco repo) makes a fresh
synchronous `http.Client{}.Do` per call - no batching, no concurrency, no
retry/backoff, and **no timeout** (relies entirely on the request context,
which has no deadline here - a single hung PCO response can hang the whole
render). Every helper in `build.go`/`sets_builder.go`/`rosters_builder.go`
is a plain sequential loop, not goroutines.

`BuildPlanPage` (`internal/server/controllers/build.go:220-221`) builds
**both** tabs unconditionally on every visit, even though only one is shown.

Worst offenders:

- **`personNameMapForIDs`** (`internal/server/controllers/people.go:122`) -
  one `pco.GetPerson` call *per distinct person*, in a loop. Every assigned
  + every eligible-candidate person across every open position gets its own
  round trip.
- **`otherAssignmentDates` + `crossTeamAssignmentsThisWeek`**
  (`internal/server/controllers/rosters_warnings.go:134`, `:201`) - called
  once *per person, per position group* they appear in
  (`rosters_builder.go:335-337, 368-370`), inside the
  `for i, g := range groups` loop. `crossTeamAssignmentsThisWeek` doesn't
  even depend on the group (no team/position args) - a person eligible for
  3 positions triggers the identical call 3 times. **This is the single
  biggest source of both waste and slowness**: a team with 6 position
  groups x ~8 people each is ~96 calls just for these two warnings, almost
  all of it avoidable duplication.
- **`buildSongNoteViews`** (`internal/server/controllers/sets_builder.go:749`)
  - one `pco.GetItemNotes` call per published song in the set order.
- **`resolveArrangementCacheInfo`** - on any DB-cache miss, a live
  `GetArrangements` + `GetKeys` per song (mitigated somewhat by the local
  `song_arrangement_cache` table already existing).

Rough total: a moderately staffed plan (say 6 position groups, ~40 distinct
people involved) is plausibly **100-200 sequential PCO round trips per page
load**, each 100-400ms - this explains both the multi-second-to-tens-of-
seconds load time *and* real proximity to that user's own 100-req/20s
ceiling within a single render.

It's not just page load, either: every htmx add/remove action re-runs
`buildRosterBuilderDataWithErr` from scratch, so a user doing several quick
edits stacks multiple of these bursts back to back.

Because most of these calls are wrapped in silent best-effort `if err ==
nil` handling (a reasonable design for graceful degradation generally), a
429 mid-render today doesn't crash the page - it silently blanks out
warnings/candidates for whatever hadn't been fetched yet. That's a worse
failure mode than an error: it looks like "nobody has a conflict" when
actually PCO just cut the app off partway through.

## Mitigations, ranked by effort vs. impact

1. ~~**Dedupe cross-team/frequency checks per person, not per (person, group)
   pair**~~ - done in `rosters_builder.go`'s `buildRosterBuilderData`:
   `crossTeamAssignmentsThisWeek` is now memoized per personID
   (`crossTeamWarningsFor`) and `otherAssignmentDates` per
   `(personID, teamID, teamPositionName)` (`otherAssignmentDatesFor`), both
   as lazily-filled maps scoped to one `buildRosterBuilderData` call -
   cheapest fix, directly attacks the worst multiplier.
   `crossTeamAssignmentsThisWeek(personID, planDate)`'s result is identical
   everywhere a person appears; memoize it once per `buildRosterBuilderData`
   call (a `map[string][]string` keyed by personID, filled lazily) instead
   of recomputing per group. `otherAssignmentDates` is per-position so needs
   `(personID, teamID, positionName)` as the key, same idea.
2. ~~**Batch `personNameMapForIDs`**~~ - confirmed live against the signed-in
   dev PCO account that People#index's `can_query_by` includes `id` and a
   comma-joined `where[id]=1,2,3` returns exactly those people in one call.
   Added `PeopleParams.IDs` to `go-pco` (`GetPeople` wires it to
   `where[id]`), released as `go-pco` `v0.15.0`, and switched
   `personNameMapForIDs` to batch through it (`personIDBatchSize` = 100,
   matching PCO's per_page ceiling) instead of one `GetPerson` call per id.
   This repo's `go.mod` now pins that real tag - fully shipped, not just
   locally tested.
3. **Only build the active tab's data**, lazy-loading the other via htmx
   when the user actually clicks it, instead of both on every
   `BuildPlanPage` hit - halves the worst case immediately for the common
   "just checking the songs" visit.
4. **Add a client-side rate-limit guard in `go-pco`** - read
   `X-PCO-API-Request-Rate-Count`/`-Limit` off responses and self-throttle
   (or at minimum surface `Retry-After` on a 429 as a real, visible error
   instead of silently degrading), plus a sane `http.Client{Timeout: ...}`
   so one hung call can't hang the whole render. This is a `go-pco` change,
   not just this app.
5. **A short-lived in-process cache** (or reuse the existing DB-cache
   pattern from `song_arrangement_cache`) for things that don't change
   mid-session - team/position lists, team member names - so repeated
   add/remove renders on the same plan don't refetch identical data every
   time.

Recommended starting point: **#1** - a small, contained diff in
`rosters_builder.go`/`rosters_warnings.go`, no new infra, removes the
largest share of redundant calls, should visibly cut load time on its own.
