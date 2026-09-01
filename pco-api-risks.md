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
   `crossTeamAssignmentsThisWeek` is now fetched once per personID and
   `otherAssignmentDates` once per `(personID, teamID, teamPositionName)`
   instead of recomputing per group - cheapest fix, directly attacks the
   worst multiplier. (Originally landed as lazily-filled memoization maps;
   superseded by the concurrency mitigation below, which needed every key
   collected up front anyway to fan the fetch out.)
2. ~~**Batch `personNameMapForIDs`**~~ - confirmed live against the signed-in
   dev PCO account that People#index's `can_query_by` includes `id` and a
   comma-joined `where[id]=1,2,3` returns exactly those people in one call.
   Added `PeopleParams.IDs` to `go-pco` (`GetPeople` wires it to
   `where[id]`), released as `go-pco` `v0.15.0`, and switched
   `personNameMapForIDs` to batch through it (`personIDBatchSize` = 100,
   matching PCO's per_page ceiling) instead of one `GetPerson` call per id.
   This repo's `go.mod` now pins that real tag - fully shipped, not just
   locally tested.
3. ~~**Only build the active tab's data**, lazy-loading the other via htmx
   instead of both on every `BuildPlanPage` hit~~ - done: `BuildPlanPage`
   now builds only `activeTab`'s data; the other tab (and its "at a
   glance" sidebar card, which reads the same data - see build.templ's own
   comment) renders a loading placeholder that fetches from a new
   `BuildSongsTabLazy`/`BuildTeamTabLazy` endpoint (`build.go`).
   `BuildTab.LazyURL`/`tabButtonAttrs`/`lazyTabTrigger` (`build_tabs.templ`)
   originally fired that fetch on `mouseenter once, focus once, click
   once` (hover/focus-triggered, click as a touch-device fallback) - moved
   to `load once, mouseenter once, focus once, click once` after real
   usage showed the Team tab (the usual bulk of this page's total load
   cost) still had an awkward wait on hover, since the fetch often didn't
   have time to finish between a hover and the click right after it.
   `load` fires immediately once the page itself has rendered, so the
   deferred tab gets the earliest possible head start instead of only
   starting once the user shows intent to switch - it's still not blocking
   the *active* tab's own first paint, just no longer waiting on
   hover/click either. The other three triggers stay as independent
   fallback retriggers (each has its own "once") in case the load-time
   fetch failed. Required threading a `hidden bool` through every
   `*ShellOOB` component this page already had
   (`glanceCardShellOOB`, `publishCardShellOOB`, `buildDetailGroupShellOOB`)
   so the new `*LazyOOB` variants (`songGlanceLazyOOB`,
   `songPublishPanelLazyOOB`, `songDetailCardsLazyOOB`, and their Team
   equivalents) can refresh a sidebar card's content while correctly
   keeping it hidden - the existing OOB siblings all assumed "my own tab
   is already open" and never set `hidden` at all, which is wrong for a
   tab being prefetched while it's still inactive.
4. **Will not do.** Add a client-side rate-limit guard in `go-pco` - read
   `X-PCO-API-Request-Rate-Count`/`-Limit` off responses and self-throttle
   (or at minimum surface `Retry-After` on a 429 as a real, visible error
   instead of silently degrading), plus a sane `http.Client{Timeout: ...}`
   so one hung call can't hang the whole render. This is a `go-pco` change,
   not just this app. Declined explicitly - revisit only if a future
   symptom (real 429s in practice, a hung render) points back at it.
5. **A short-lived in-process cache** (or reuse the existing DB-cache
   pattern from `song_arrangement_cache`) for things that don't change
   mid-session - team/position lists, team member names - so repeated
   add/remove renders on the same plan don't refetch identical data every
   time. Not started - mostly helps *repeat* renders on an already-open
   plan (the add/remove churn noted above), not first page load.
6. ~~**Bounded concurrency for independent PCO calls**~~ - not on the
   original list; added once #1/#2 landed and first-load time was still
   ~9s. #1 and #2 cut call *count*; neither touched the fact that every
   loop in `build.go`/`sets_builder.go`/`rosters_builder.go` calls PCO
   **sequentially** (`go-pco`'s `NewRequest` is a plain synchronous
   `http.Client.Do` - no batching, no concurrency), so a ~100-200-call
   chain at 100-400ms each stays a multi-second wait regardless of how
   many of those calls are duplicates. Added `runConcurrently` (bounded to
   `pcoFanOutLimit` = 10 goroutines at once, `internal/server/controllers/
   concurrency.go`) and fanned out every independent per-item PCO loop
   through it: `rosters_builder.go`'s per-group candidate-pool fetch, its
   per-person `GetBlockouts` fetch (confirmed live this endpoint has no
   batch/`where[id]` option - `services/v2/people/{id}/blockouts`'
   `can_query_by` is just `group_identifier` - so concurrency is the only
   lever available here, unlike #2's fix for `personNameMapForIDs`), its
   cross-team/other-dates warm-up (now a prefetch-then-fan-out pass instead
   of lazy per-call memoization), and `sets_builder.go`'s
   `renderServiceOrder` (per-song `GetItemNotes`/arrangement resolve).
   Doesn't reduce PCO's own rate-limit exposure (same call count within
   the same ~20s window, just compressed in time) but directly attacks
   wall-clock load time, which #1/#2 didn't.

Rate-limit note on #6: PCO's 100-req/20s ceiling counts calls within a
rolling window regardless of how bunched-up they are - a page load that
already finished (sequentially or concurrently) within under ~9s was
already inside one window either way, so fanning it out doesn't add new
rate-limit exposure; if anything, finishing sooner narrows the chance of
overlapping with the *next* request's own calls.

With #1, #2, #3, and #6 all landed, the only open item is **#5** (short-lived
in-process cache) - lower priority since it mainly helps repeat edits on an
already-open plan, not first load. #4 is explicitly off the table for now
(see above). If first load is *still* materially slow after #3/#6, that's a
signal worth re-measuring rather than guessing at a #7: confirm with real
timing (not just "still feels slow") before picking the next lever, since
the two easy, well-understood wins are both already in.
