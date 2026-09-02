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
   `lazyTabTrigger`/`tabPanelLazyAttrs` (`build_tabs.templ`/`build.templ`)
   went through several iterations on *when* and *where* that fetch fires:
     1. `mouseenter once, focus once, click once`, attached to the tab
        button (hover/focus-triggered, click as a touch-device fallback) -
        real usage showed the Team tab (the usual bulk of this page's
        total load cost) still had an awkward wait on hover, since the
        fetch often didn't have time to finish between a hover and the
        click right after it.
     2. `load once, mouseenter once, focus once, click once`, still on the
        button - `load` fires immediately once the page itself has
        rendered, giving the deferred tab the earliest possible head start
        instead of only starting once the user shows intent to switch.
        Still not blocking the *active* tab's own first paint - a separate
        request right after, not something `BuildPlanPage` waits on.
     3. `load once, click once` - once `load` always fires the fetch
        immediately, `mouseenter`/`focus` never get a chance to do
        anything `load` hasn't already started; they'd only ever add a
        second, redundant GET on top of it. `click` stays as a same-tab
        retry in case the `load`-triggered fetch failed (a network
        hiccup) - without it, that failure mode had no recovery short of a
        full page reload.
     4. Same triggers, moved off the button onto the panel div, with
        `click ... from:#build-tab-button-<tabID>` replacing plain
        `click`. Attaching `hx-get` to the tab button caused a real bug:
        `scheduling.js`'s app-wide "disable the clicked control while its
        request is in flight" feedback matches purely on
        `event.detail.elt` being a `<button>` - it can't tell a genuine
        click apart from the automatic `load` trigger, so the very first
        load-triggered fetch (the Team tab's, usually the slow one) left
        that button disabled - not even clickable - for the whole time it
        was in flight. Moving `hx-get`/`hx-trigger` to the (non-button)
        panel div, with `from:` still listening for the button's click,
        keeps the fetch tied to the same interactions without the button
        itself ever being the element a request is attached to.
     5. **Settled: just `load once`, and a client-side visibility resync
        added to close the state-corruption bug this whole trigger design
        was actually causing.** Two compounding problems, both real, found
        via live use:
          - htmx's `once` is tracked *per trigger clause*, not shared
            across them - `load once, click once` doesn't mean "click only
            if load didn't already fire"; `click` fires on its own
            regardless, so every first click of a deferred tab re-ran the
            fetch even after `load` had already finished successfully.
          - Worse: that redundant response's out-of-band fragments
            (`rosterPublishPanelLazyOOB` etc.) only know whether the tab
            was still inactive when the *request* went out. By the time a
            click-triggered (or even just a still-in-flight load-
            triggered) response lands, the user may have already switched
            tabs by hand - `toggleBuildTab` sets the correct hidden state
            instantly on click, and the late-arriving OOB fragments then
            silently stamp the *stale* "still inactive" state right back
            over it: the tab's own publish/detail cards hidden again, its
            glance card visible again, exactly backwards from what should
            be showing now that it's the open tab.
        Fix: dropped `click` entirely (`lazyTabTrigger` is just
        `"load once"` now) - `load` fires before any human click is
        physically possible, so there's no legitimate reason left to
        re-fire on click, only the bug it was causing. That alone doesn't
        fully close the race (a still-in-flight `load` response can still
        land after a manual switch), so `scheduling.js` also gained
        `syncBuildTabVisibility`/`currentBuildTab`, run on every
        `htmx:afterSettle`: re-derives every tab-scoped element's hidden
        state from whichever tab is actually `aria-selected` right now,
        regardless of what hidden value rode along in whatever response
        just landed. Makes hidden state self-healing rather than
        order-dependent - correct no matter which of "the click" or "the
        background fetch" finishes last. Traded away: a failed `load`
        fetch has no built-in retry any more (previously `click` covered
        that, at the cost of causing this bug) - acceptable for now, worth
        revisiting only if failed background fetches turn out to be a real
        problem in practice.
   Also required threading a `hidden bool` through every
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
5. ~~**A short-lived in-process cache**~~ - implemented, scoped specifically
   to the full song list (the concrete case that turned out to matter: a
   worship pastor's ~900-song PCO library, most of it archived, being
   pulled live/uncached/unfiltered on every page view and htmx add/remove
   - see the "Fix PCO song-list performance" plan). `internal/scheduling/
   pco_songs.go`'s `FetchActiveSongs(ctx, organizationID)` replaced the old
   unfiltered, uncapped-only-by-a-silent-20-page-cap `FetchAllSongs`:
   filters out archived (`Attributes.Hidden`) songs client-side
   (`filterActiveSongs` - the real correctness guarantee, always applied)
   and, since PCO's Songs#index confirmed live to actually support
   server-side `where[hidden]` filtering (`can_query_by` includes
   `hidden`; `where[hidden]=true` vs. `=false` return different, correct
   `total_count`s), also passes `Hidden: false` server-side for a smaller
   page count on top of that. Caches the result per organization for 60s
   (`songCacheTTL`, matching `middleware.navStateTTL`'s reasoning), with
   the same fail-safe convention as nav.go's cache: a transient fetch
   failure is never cached, so it can't pin a stale/empty result in for
   the rest of the window. `pcoreset` gets its own uncached, unfiltered
   `FetchAllSongsIncludingArchived`, since a full org reset needs to see
   (and delete) archived songs too. Team/position lists and team member
   names (this item's other original examples) remain uncached - the song
   list was the one with an actual reported symptom.
6. ~~**Bounded concurrency for independent PCO calls**~~ - not on the
   original list; added once #1/#2 landed and first-load time was still
   ~9s. #1 and #2 cut call *count*; neither touched the fact that every
   loop in `build.go`/`sets_builder.go`/`rosters_builder.go` calls PCO
   **sequentially** (`go-pco`'s `NewRequest` is a plain synchronous
   `http.Client.Do` - no batching, no concurrency), so a ~100-200-call
   chain at 100-400ms each stays a multi-second wait regardless of how
   many of those calls are duplicates. Added `runConcurrently` (bounded to
   `pcoFanOutLimit` = 10 goroutines at once, originally
   `internal/server/controllers/concurrency.go`, later relocated to
   `internal/scheduling/concurrency.go` as `RunConcurrently`/
   `PCOFanOutLimit` so `fetchSongsFromPCO`'s own concurrent page fan-out,
   #5 above, could use it too) and fanned out every independent per-item
   PCO loop through it: `rosters_builder.go`'s per-group candidate-pool
   fetch, its
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

With #1, #2, #3, #5, and #6 all landed, #4 is explicitly off the table for
now (see above) and nothing else is currently open. If first load is
*still* materially slow after all of these, that's a signal worth
re-measuring rather than guessing at a #7: confirm with real timing (not
just "still feels slow") before picking the next lever, since several
easy, well-understood wins are already in.

## Addendum: Song Info's bulk sync was a separate, sharper risk

Found after the above was written "nothing else is currently open" -
`SyncSongInfo` (`internal/server/controllers/song_arrangement_cache.go`)
walks every active song needing a cache backfill and live-fetches its
arrangement + key (2 calls/song), **fully sequentially, inside the HTTP
request**. Unlike the Plan page's ~100-200 calls (bad but survivable,
see #6's rate-limit note above), a first sync for a ~100-song library
alone needs up to ~200 calls - double PCO's 100-req/20s budget in a
*single* request, and the more songs, the worse. Simply wrapping the loop
in `RunConcurrently` (like #6 above) would make it faster but strictly
*more* likely to trip a 429, not less, since it crams the same total call
count into a much shorter window instead of the accidental self-throttling
sequential latency provided.

Fixed by detaching the sync from the request entirely: `SyncSongInfo`
launches a background goroutine (`runSongInfoSync`, given a
`context.Background()`-derived context carrying the user's PCO access
token via `middleware.AccessToken` - independent of the fiber request's
own context, which is cancelled once the handler returns) and responds
immediately. The goroutine paces itself: songs needing a sync are batched
(`songInfoSyncBatchSize` = `PCOFanOutLimit`, 10 songs/batch = up to 20
calls/batch), each batch run concurrently via `RunConcurrently`, with a
`songInfoSyncBatchPause` (10s) pause between batches - capping the
sustained rate at ~60 calls per any 20-second window (60% of budget,
comfortable headroom left for the rest of the app's ordinary traffic)
regardless of how large the library is. An in-memory `songSyncInProgress`
guard (mutex + map, same pattern as `nav.go`'s cache) stops a second
concurrent pass for the same org. No new progress-tracking table was
needed: `buildSongInfoData`'s `CachedCount`/`TotalCount` already recompute
live from the DB on every render, so reloading the Song Info page shows
the sync's progress for free, and `nav.go`'s existing `songInfoNeedsSync`
badge already clears itself within a minute of the cache filling in.

## Addendum: Team Builder's duplicate scoped-fetch calls were the worst
## per-action cost in the app

Found via two independent audit passes, unrelated to the sync above -
`internal/server/controllers/rosters_scoped.go`'s two narrow-fragment
builders, `buildRosterPositionGroupData` (recomputes one position group)
and `buildRosterLightData` (recomputes the roster's shared page chrome),
were each independently calling `pco.GetTeamMembers`,
`pco.GetNeededPositions`, and `pco.GetTeams` for the same plan.
`rosters_detail.go`'s `AddRosterAssignment`, `RemoveRosterAssignment`, and
`QuickAddPositionPreference` handlers call *both* functions back-to-back on
every single add/remove/quick-add click, so those three requests were sent
twice each - 6 calls where 3 would do, on top of
`buildRosterPositionGroupData`'s own per-position/per-person fan-out
(`GetTeamPositions`, `GetPlan`, `GetPersonTeamPositionAssignments`, plus
the `GetBlockouts`/cross-team/other-dates fan-out already covered by
mitigation #6 above). A single click on the Team Builder tab could cost
roughly 30-40 PCO calls total, at least 6 of which were byte-for-byte
duplicate requests for data already in hand from the other function's own
call moments earlier - narrower in reach than the pre-fix song list (only
the Team Builder tab, not every page) but worse per-action.

Fixed by introducing `rosterSharedPCOData` (`rosters_scoped.go`): a small
struct holding all three responses (plus their own fetch errors, since the
two functions don't agree on which of the three is worth surfacing as an
error - see the struct's own doc comment) and
`fetchRosterSharedPCOData(ctx, serviceTypeID, planID)`, which fetches all
three once. `buildRosterPositionGroupData`/`buildRosterLightData` both now
take a `*rosterSharedPCOData` parameter: nil means "fetch your own copy"
(preserving the original one-fetch-each behavior for a caller that only
needs one of the two - `QuickAddPersonRelationship` is the only such
caller today), while the three `rosters_detail.go` handlers that call both
functions together fetch once and hand the same pointer to both. No
change to `buildRosterPositionGroupData`'s own remaining per-position/
per-person fan-out, `buildRosterBuilderData`'s full-rebuild path (never
calls both scoped functions together, so was never affected), or any
behavior/error-message text - purely eliminates the duplicate requests.
Team/position lists and team member names remain uncached *across*
requests (mitigation #5's still-open gap) - this fix only removes the
*within-one-request* duplication, a narrower and cheaper problem than
adding a cache would be.

## Addendum: three residual sequential loops found by a later audit

Found by two independent audit passes well after "nothing else is
currently open" above - #2 and #6 fixed the *worst* sequential/duplicate
loops, but three smaller ones of the same shape survived both passes:

- **`personNameMap`** (`internal/server/controllers/scheduling.go`) - the
  song/person Preferences page's own name resolver, a near-duplicate of
  the `personNameMapForIDs` #2 fixed, except this one still ran one
  `pco.GetPerson` call per distinct person id in the preferences list. Not
  caught by #2 because it's a separate function serving a separate page,
  not a call site of `personNameMapForIDs` itself. Fixed by rewriting it
  to collect its distinct ids and delegate to `personNameMapForIDs`
  directly - it's now a ~10-line wrapper, same batched `where[id]=...`
  calls, same leave-it-out-of-the-map fallback for an id that fails to
  resolve.
- **`combinedPlansForServiceType`** (`internal/server/controllers/
  build.go`) - the Build page's plan-picker: after paginating to at most
  `buildPlansPageSize` (10) rows, ran `planStatus`/`rosterStatus` for each
  row in a sequential loop. Each of those falls back to a live
  `GetItems`/`GetTeamMembers` call when the row has no local draft yet, so
  a full page of rows could mean up to 20 sequential round trips just to
  render the picker - the same "independent per-row PCO calls, run one at
  a time" shape #6 fixed elsewhere in `sets_builder.go`/
  `rosters_builder.go`, just not caught in that pass since this loop lives
  in `build.go` itself rather than the two builder files #6 focused on.
  Fixed by fanning the per-row status computation out via
  `scheduling.RunConcurrently` (`buildPlanRows`, pulled out as its own
  function so the fan-out is testable against a fake status callback
  without a real `*scheduling.Store` or PCO round trip) - pre-sized,
  index-owned-per-goroutine, same pattern as `renderServiceOrder`.
- **`fetchTeamPositions`** (`internal/server/controllers/people.go`) - the
  position-preferences page's team/position walk: one `GetTeamPositions`
  call per team on the service type, sequentially. Same fix, fanned out
  via `scheduling.RunConcurrently` into a pre-sized, index-matched slice
  so team order (`GetTeams`' own `OrderBy: "name"`) survives the fan-out;
  "first error wins" (via `sync.Once`) replaces the old sequential loop's
  literal first error, since concurrent completion order no longer
  guarantees the same team "loses" the race every time - documented at
  the call site as "an" error rather than strictly the first one, which
  is fine given the caller only ever shows a single generic "loading
  teams from Planning Center" message either way.

None of these change PCO call *count* (unlike #2) or introduce a new
cache (unlike #5) - all three are pure wall-clock fixes, same category as
#6, just for loops #6's pass didn't reach. Team/position lists and
team-member names (see #5) remain uncached, same as before this addendum
- this only fixed how those calls are scheduled relative to each other,
not whether they happen at all.

## Addendum: GetServiceTypes cached, matching FetchActiveSongs' pattern

Low severity, broad reach: `build.go`'s `BuildPage`, `home.go`'s
`buildHomeData`, and `people.go`'s `PositionPreferencesPage` each called
`pco.GetServiceTypes` independently, with no caching between them - cheap
individually (a service type list is small, no pagination involved,
unlike the song library), but three uncached live calls across three
different pages that can all be hit in normal navigation.

Fixed by `internal/scheduling/pco_service_types.go`'s `FetchServiceTypes`
- an org-scoped, 60s-TTL cache, same shape as `FetchActiveSongs`
(including reusing its `songCacheEntryFresh` pure freshness check
directly, since both live in the `scheduling` package - no need to
duplicate it the way `nav.go`'s copy is duplicated across the
`middleware`/`scheduling` package boundary). All three call sites now go
through it instead of calling `pco.GetServiceTypes` directly. The `/pco`
dev-only exploration group (`pco.go`, gated behind `APP_ENV=local`, not
real production traffic) was deliberately left alone - not worth
threading org-scoping through a debug-only endpoint for.

`httptest`-backed tests (`pco_service_types_httptest_test.go`, same
`pco.SetBaseURLForTesting` seam as `pco_songs_httptest_test.go`) prove a
second call within the TTL window costs zero additional PCO requests, and
that the cache is genuinely per-organization.

## Addendum: publish-path loops parallelized - writes, not reads, extra care taken

Low-medium severity, but a different risk shape than every fix above:
`PublishSet`'s deletion loop and create/update loop (`sets_publish.go`),
`RefreshSongInfo`'s PATCH loop (`sets_publish.go`), and `PublishRoster`'s
deletion and creation loops (`rosters_publish.go`) all called PCO
sequentially, one write per song/assignment. Real, but only on an
explicit Publish/Refresh click, not render time, and already
self-throttling by virtue of being sequential - the same reason this
mattered less urgently than the read-side fixes above.

Fanned out via `scheduling.RunConcurrently`, same as every other fix in
this doc, but writes need more care than reads:

- **Ordering invariants preserved structurally, not just by convention**:
  `RunConcurrently` blocks until every goroutine in one fan-out returns
  before the caller continues, so `PublishSet`'s "deletions fully finish
  before any create/update starts" and `PublishRoster`'s equivalent still
  hold exactly as they did sequentially - each loop is its own fan-out,
  not one fan-out spanning both phases.
- **`PublishSet`'s create/update loop is the one with a real subtlety**: a
  song that fails to create must be excluded from `publishable` (the list
  fed to `mergeServiceOrder` for the reorder call) - the sequential
  version did this with a bare `continue`. The concurrent version uses a
  pre-sized, index-matched `results []publishResult` slice with an
  `include` flag, defaulting to `false` (excluded) unless the goroutine
  reaches the point where it would have fallen through to `publishable =
  append(...)` in the original. Confirmed safe to reorder in whatever
  order the fan-out finishes in: `mergeServiceOrder` groups by each
  `SetSong`'s stored `AnchorItemID`/`Position`, not by input list order
  (see `TestMergeServiceOrderGroupOrderedByPosition`,
  `sets_builder_test.go`) - `publishable`'s own element order was never
  load-bearing, sequential or concurrent.
- **Failure/counter collection made goroutine-safe**: every `failures =
  append(...)` is now guarded by a `sync.Mutex`; every `deleted`/
  `created`/`updated`/`refreshed` counter is a `sync/atomic` `int32`,
  converted back to a plain `int` only after its fan-out has fully
  returned. Same per-item error messages and best-effort tolerance as
  before - purely a concurrency-safety change, not a behavior change.

**Not covered by an automated test** - unlike every other fix in this
doc, none of `PublishSet`/`RefreshSongInfo`/`PublishRoster` have any
existing test coverage (or an `httptest`-only path to it): they need a
real `*scheduling.Store`, and this codebase has no DB-backed test harness
anywhere yet (confirmed - no other controller test needing a live Store
exists either). Verified instead by careful manual review of the ordering
invariants above, plus the existing `mergeServiceOrder`/pure-helper tests
that already cover the assumptions this change leans on. Recommended: a
manual Publish/Refresh/PublishRoster smoke test against a real (dev) PCO
plan before leaning on this in production, given the higher stakes of a
mistake here (real writes to a church's live service order) versus every
read-only fix above.
