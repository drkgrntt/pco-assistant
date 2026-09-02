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
4. **Split into three, re-evaluated 2026-09-01 - two done, one still
   declined.** The original item bundled a timeout, `Retry-After`
   visibility, and proactive self-throttling together and declined all
   three as "a `go-pco` change, not just this app." Splitting them out
   matters because the app genuinely *cannot* implement any of this
   itself no matter how it's structured - `NewRequest` (a stateless free
   function, not a client struct/options pattern) discards the raw
   `http.Response` after reading the body, so nothing in it (headers,
   status nuance) is visible to a caller unless `go-pco` chooses to
   surface it. That's a hard constraint on "let the app manage it
   instead," not a design preference either way.
     - ~~**Request timeout**~~ - done: confirmed live in `go-pco`'s
       `pco.go` that `NewRequest` built a fresh `&http.Client{}` per call
       with no `Timeout` set at all, so a hung PCO response really could
       hang the whole render indefinitely (nothing in a render-time
       caller's context enforces a deadline unless it sets one itself).
       Not a rate-limit or policy question - plain correctness gap only
       `go-pco` could fix, since only it owns that `http.Client`. Fixed
       unconditionally (no opt-in needed) with a fixed 30s
       `requestTimeout` const in `go-pco` `v0.17.0`; this repo's `go.mod`
       now pins that tag.
     - ~~**Surface `Retry-After` on a 429**~~ - done, same release:
       `RequestError` already existed and already carried `StatusCode` (a
       429 was already distinguishable via `errors.As`, just nothing was
       checking), so this was a one-field, backward-compatible addition -
       `RetryAfter string`, populated verbatim from the header (empty
       when absent), left unparsed since PCO always sends a
       delay-in-seconds value, never an HTTP-date. This is the
       mechanism/policy split working as it should: `go-pco` exposes the
       one fact only it can see, this app decides what (if anything) to
       do with it - nothing consumes `RetryAfter` yet, added purely so
       the option exists next time a 429 actually shows up in practice.
     - **Proactive self-throttle from `X-PCO-API-Request-Rate-Count`/
       `-Limit`** - still declined, unchanged reasoning: this is the
       genuinely policy-shaped piece (different consumers would want
       different behavior - throttle vs. log vs. alert), and nothing in
       this re-evaluation surfaced a new symptom to justify building it
       speculatively. If it's ever built, it should be opt-in (a
       nil-by-default callback/hook `go-pco` calls after every response,
       mirroring the existing `SetBaseURLForTesting` package-level-var
       pattern) rather than automatic behavior every consumer is forced
       into - revisit only if a real consumer hits an actual 429 in
       practice.
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

With #1, #2, #3, #5, and #6 all landed, and #4's timeout/`Retry-After`
pieces landed separately (its self-throttle piece still declined, see
above), nothing else is currently open. If first load is *still*
materially slow after all of these, that's a signal worth re-measuring
rather than guessing at a #7: confirm with real timing (not just "still
feels slow") before picking the next lever, since several easy,
well-understood wins are already in.

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

## Addendum: "Browse all songs" made search-driven; a new hot path caught and cached along the way

Different risk shape again: a rendering/CPU cost, not a PCO call-count
problem by itself - `buildSetBuilderData`'s Candidates loop
(`sets_builder.go`) used to compute a full `views.CandidateSongView`
(conflict titles, tags, roles, arrangement info) for *every* active,
not-yet-added song in the library, unconditionally, on every single Build
page render - whether or not the "Browse all songs" section was even open.
The underlying song list itself was already the cached, filtered
`FetchActiveSongs` call (#5 above) - this didn't add a new PCO call, but a
few hundred songs still meant a few hundred rich rows computed and
rendered every time, most of it thrown away unseen.

Fixed by making that list search-driven: `BuildCandidatesSearch` (new
route, `/scheduling/build/:service_type_id/:plan_id/candidates/search`)
computes `CandidateSongView` rows only for songs actually matching a typed
query/role filter, capped at 50, via the exact same per-song computation
factored into `candidateLookups.candidateSongView` (shared with
`buildSetBuilderData`'s much smaller remaining use of it - see below).
With no search term and no role filter, nothing is fetched or computed at
all: the list shows a plain "type to search" prompt. `buildSetBuilderData`
itself now only builds a lightweight `[]views.SongOptionView` (id+title)
for the whole library, for the replace-form `<select>` pickers - cheap,
no PCO or per-song local-DB lookups beyond what's already fetched. Its
`SongsToReinforce` pass still needs the rich per-song computation, but
only for songs that are actually reinforcement-eligible (tracked-new,
sung once or twice) - a small fraction of a real library - so the loop
now checks that cheap map lookup first and skips straight past everything
else.

**The one genuinely new PCO-facing risk this introduced**: search requests
are debounced (300ms) but still fire on every real pause while typing, and
`searchCandidates` (backing `BuildCandidatesSearch`) needed
`fetchRecentPlanHistory`'s repeat-song/average-length data (same as
`buildSetBuilderData` already did) - which, until this change, was an
uncached live `GetPlans` + 2x `GetItems` on *every* call. Turning one
call-per-full-render into one call-per-debounced-keystroke would have
meaningfully multiplied a previously-rare live PCO round trip. Caught
before shipping (this is exactly the "does this touch how often the app
calls PCO" question this file exists to keep asking) and fixed the same
way as #5/the `GetServiceTypes` addendum above: `fetchRecentPlanHistory`
now caches per service type (a PCO service type id is already
org-scoped, so no separate org key is needed), 60s TTL matching
`FetchActiveSongs`' own. A genuine PCO failure is never cached (only a
successful fetch - including the legitimate "fewer than two past plans
exist yet" zero-value result - gets stored), same fail-safe convention as
every other cache in this doc. `sets_builder_history_cache_test.go`
(`httptest`-backed, same `pco.SetBaseURLForTesting` seam as the other
tests in this doc) proves a second call within the TTL costs zero
additional requests, a failed call is never cached, and two different
service types never share an entry.

## Addendum: app-wide re-audit (2026-09-01) - one sequential loop found, everything else already covered

Prompted by a request to re-check for N+1/uncached-fan-out risk at scale
(hundreds of songs/arrangements/services/people), independent of any
specific reported symptom. Walked every call site of `pco.Get*`/
`pco.Create*`/`pco.Update*`/`pco.Delete*` across `internal/server/
controllers` and `internal/scheduling` and checked each one against the
patterns already fixed above (sequential per-item loop, duplicate fetch,
missing cache on a session-stable read). Everything already covered by
mitigations #1-#6 and the addenda above was confirmed still fixed in its
current form: `rosters_builder.go`/`rosters_scoped.go`'s team/position/
candidate/blockout/cross-team fan-outs, `people.go`'s `fetchTeamPositions`
and batched `personNameMapForIDs`, `build.go`'s paginated/fanned-out
`fetchPlansByFilter` and `buildPlanRows`, `sets_builder.go`'s
`renderServiceOrder` and cached `fetchRecentPlanHistory`, and
`sets_publish.go`/`rosters_publish.go`'s fanned-out publish loops.

One real gap survived every prior pass: **`home.go`'s `buildHomeData`**,
the signed-in landing page every login hits. For each of up to
`homeServiceTypesCap` (5) service types, it ran `pco.GetPlans` then
`planStatus`/`rosterStatus` **sequentially, one service type at a time**
- the exact same shape #6/the `combinedPlansForServiceType` addendum
already fixed for the Build page's plan picker (`buildPlanRows`), just
never applied here since this is a separate function on a separate page.
`planStatus`/`rosterStatus` each fall back to a live `GetItems`/
`GetTeamMembers` call whenever the service type has no local draft yet
(the common case for a church just adopting this app, or a service type
nobody's built out locally), so a multi-service-type church could pay up
to 3 sequential round trips per service type - up to ~15 sequential PCO
calls - on every single home page visit, not just occasionally.

Fixed the same way as `buildPlanRows`: fanned out via
`scheduling.RunConcurrently` over a pre-sized, index-owned `[]*views.
HomePlanRow` slice (a nil entry means "no upcoming plan, or a fetch
failed" and is filtered out afterward), preserving service-type order
regardless of which goroutine finishes first. No behavior change beyond
wall-clock time - same best-effort tolerance, same data, same order.

## Addendum: Team Builder's cold-start risk was the candidate eligibility pool, not the assigned roster

A closer read of `buildRosterBuilderData`/`buildRosterPositionGroupData`
found the real driver behind this doc's own "6 position groups, ~40
people" estimate (mitigation #6, the addendum above): "person involved in
the roster" was never "person assigned to the plan" - it was every person
PCO's `GetPersonTeamPositionAssignments` reports as *eligible* for any open
position on the team, whether or not they were actually scheduled.
`views.CandidatePersonView` (the "who could be added" list shown per
position) had its `FrequencyWarning`/`BlockoutWarning`/`CrossTeamWarning`
computed eagerly for *every* eligible candidate on *every* open position,
fanned out via `scheduling.RunConcurrently` alongside the assigned roster's
own (small, plan-bounded) warnings. A plan only ever assigns a small number
of people, but a well-staffed volunteer church can have a large eligibility
pool *per position* - and that pool, not the assigned roster, is what
actually scales with "hundreds of people." Concurrency only compresses
wall-clock time, not call count, so a large pool could throw a few hundred
requests at PCO within a single rolling 20-second window - and because
every one of these fetches already used the existing best-effort `if err
== nil` handling, a 429 partway through didn't surface as an error: a
director would just see candidates with no warnings at all, indistinguishable
from "no conflicts."

**Fixed by no longer computing candidate warnings eagerly at all** - same
move as the "Browse all songs" addendum above (compute rich per-item data
only for what's actually being looked at, not the whole pool).
`buildRosterBuilderData` and `buildRosterPositionGroupData`
(`rosters_scoped.go`) now scope every PCO-backed warning fetch
(`pco.GetBlockouts`, `crossTeamAssignmentsThisWeek`, `otherAssignmentDates`)
down to people actually assigned to a position (`AssignmentView`'s own
warnings are unaffected - inherently bounded by the plan, not the
eligibility pool). Every `CandidatePersonView` both functions build now
routes through one shared constructor, `candidatePersonView` (rosters_
builder.go), which only ever sets `PersonID`/`Name`/`SchedulePreference` -
names still resolve for every candidate (`personNameMapForIDs` stays
unscoped), just not their warnings.

A candidate's own warnings are now fetched **on demand** instead: a new
route, `GET /scheduling/rosters/:service_type_id/:plan_id/candidates/
:person_id/warnings` (`CandidateWarnings`, `rosters_candidate_warnings.go`),
computes one person's three warnings via the exact same pure functions and
thin PCO-calling wrappers the old eager path used, returning a small
fragment (`CandidateWarningsFragment`, `roster_builder.templ`) swapped into
that one candidate's own placeholder span. The trigger is a plain
`<button>` ("Check for conflicts") on that candidate's own sidebar detail
card (`candidateDetailCard`, `person_detail_card.templ`) - reached by
clicking the candidate row itself, which opens the card but never fires
the fetch on its own - a real UI constraint ruled out anything passive
(`mouseenter`/on-visible/on-load): `positionGroupCard`'s `collapsibleCard`
already expands a position by default whenever it's short-staffed (the
common case for an open position), and an expanded card renders every one
of its candidate rows at once with no per-row disclosure - a passive
trigger firing for every row the instant such a card opens would
reintroduce exactly the burst this fix
removes, just moved from render-time to open-time. A `<button>`'s default
htmx trigger is a real click, so this can't fire without someone actually
asking for that one person's warnings.

Not addressed by this fix, and not claimed to be: team/position lists and
team-member names (mitigation #5's still-open gap, reaffirmed by the
addendum above) remain fetched live and uncached on every roster render -
a separate, still-open item.

## Addendum: Team Builder's per-person blockout/cross-team/frequency calls cached in a local Postgres table, backed by a paced background sync

`plan.md`'s follow-up to the two addenda above: `otherAssignmentDates`/
`crossTeamAssignmentsThisWeek`/the per-person `pco.GetBlockouts` call
(`rosters_warnings.go`/`rosters_builder.go`/`rosters_scoped.go`) were still
live PCO calls on every single Team Builder render, once per relevant
person - the actual per-call-volume driver the "Team Builder's duplicate
scoped-fetch calls" addendum above measured at 30-40 calls/click, and the
reason `pco-api-risks.md` kept listing "team/position lists and
team-member names remain uncached" as a still-open per-person gap. This
closes the *per-person* half of that gap (blockouts, cross-team history,
per-team history) - team/position *lists* and team-member *names*
themselves are untouched by this change and remain the one deliberate
open gap, same as before.

**What's cached, and how**: three new local tables (migration `00033`),
each a sync-marker row (`person_*_sync`, "when was this last checked")
plus zero-or-more detail rows (`person_*`, "what was found") - the same
shape as `song_arrangement_cache` (`00023`), minus that table's "a row's
mere existence means checked" shortcut, which doesn't work for list-valued
data (zero blockouts and never-synced both look like zero rows).
`otherAssignmentDates`/`crossTeamAssignmentsThisWeek`/the new
`personBlockouts` wrapper (`rosters_warnings.go`) are now pure reads of
these tables - no PCO call happens in the Team Builder render path at all,
cache hit or miss. A miss (no sync-marker row yet) renders without that
person's warning this one render and queues a background sync; a stale
hit (`synced_at` older than `personCacheTTL`, 4 hours - blockouts/serving
history don't change minute-to-minute the way the song-list's 60s TTL
target does, so a short TTL would barely help the cold case) serves the
stale row immediately and *also* queues a refresh. Either way the render
never blocks on PCO - the exact fix this addendum's "duplicate scoped-fetch
calls" predecessor didn't reach, since deduping calls within one render
still leaves every render paying full price on a cache miss.

**Background sync** (`person_data_cache.go`) clones `SyncSongInfo`'s
architecture (`song_arrangement_cache.go`) rather than inventing a new
one: a package-level pending-request queue populated by the misses/stale
hits above, drained by a background goroutine in `personDataSyncBatchSize`
(`scheduling.PCOFanOutLimit`, 10) batches with a `personDataSyncBatchPause`
(10s) pause between batches, guarded against a duplicate concurrent pass
per organization (`personSyncInProgress`, mirroring `songSyncInProgress`).
Each request costs exactly one PCO call (one `GetBlockouts` or
`GetPersonPlanPeople`, vs. Song Info's two calls per song), so this
pacing's sustained rate works out to at most 30% of the 100-requests/20s
budget - even more headroom than Song Info's already-conservative 60%, see
`personDataSyncBatchPause`'s doc comment for the arithmetic. Deliberately
*not* an unattended/nightly sync (`plan.md`'s "unattended-cron question,
revisited" explicitly defers that): this only ever runs off a live
request's own PCO access token (`triggerPersonDataSync`, called from every
roster-rendering handler right after building its response), so it needs
no new token-management mechanism (borrowing/refreshing some other
session's token for unattended use) at all - a genuinely idle org's cache
just stays cold until someone actually opens the Team Builder page again,
which is an acceptable trade per that plan section's own reasoning.

**A render-time "still gathering data" notice** (`RosterBuilderData.
PersonDataSyncPending`, checked via the new `scheduling.Store.
AnyPersonDataSynced`) covers the resulting UX gap: a brand-new org's very
first render has *nothing* synced for anyone yet, which would otherwise
render as zero warnings - indistinguishable from "checked, nothing to
report," a worse regression than the old always-live behavior in exactly
the case this change most needs to look correct. Same "no new tracked
state, recomputed live from an existing table on render" spirit as
`nav.go`'s `songInfoNeedsSync` badge - one extra `EXISTS` query against the
three sync-marker tables, scoped to the specific people relevant to *this*
render (`buildRosterBuilderData`'s assigned+candidate `relevantIDs` on a
full build, `buildRosterLightData`'s assigned-only set on a scoped
mutation's OOB refresh), not an org-wide flag - so a large org that's
warmed one team's cache still correctly sees the notice on a different,
never-rendered team. Self-clearing: once any relevant person has a
`synced_at` row in any of the three tables, `AnyPersonDataSynced` returns
true and the notice stops appearing on its own, no dismiss/session state
needed.

**Unverified-live-check caveat, taken deliberately**: `plan.md` proposed
merging `crossTeamAssignmentsThisWeek`'s all-teams call and
`otherAssignmentDates`' per-team call into one shared cached table,
contingent on live-verifying two things against the real PCO API first -
(1) whether `GetPersonPlanPeople` accepts a `PerPage` above 25 without
losing per-team accuracy for a multi-team volunteer, and (2) whether PCO
exposes a global/batch blockouts listing instead of only the per-person
nested endpoint this app already uses. **Neither could be live-verified
this session** (blocked by sandbox tooling policy when attempting to use a
live session token) - per `plan.md`'s own explicit fallback instruction,
this shipped as the conservative design instead: two genuinely separate
tables/sync passes for plan-people history (`person_plan_history` for the
all-teams cross-team check, `person_team_plan_history` - keyed additionally
by `team_id` - for the per-team frequency check) rather than one merged
table filtered two ways, and the per-person nested `GetBlockouts` call
exactly as it's always been called, not a global endpoint. Both remain
worth a real live check in a future pass, per `plan.md`'s own framing -
if `GetPersonPlanPeople` does tolerate a higher `PerPage` without diluting
per-team accuracy, or PCO does expose a global blockouts listing, either
would simplify this schema (one table instead of two, or no
`person_blockout_sync`/`person_blockouts` tables at all) - but risking
warning accuracy to save a table wasn't worth it without confirming first.

## Addendum: two manual "refresh this one item now" actions added to the Song Info and person-data caches

Both `song_arrangement_cache` (Song Info addendum above) and the
per-person blockout/plan-history caches (previous addendum) are read-first,
background-sync-second designs: a cache miss or stale hit never blocks a
render on a live PCO call, but it also means a single item's correction
(a song's key was retuned in Planning Center, a person's blockout was just
added) only shows up once that item's own turn comes up in the paced
background queue - `songInfoSyncBatchPause`/`personDataSyncBatchPause`
(10s between batches of `scheduling.PCOFanOutLimit`, 10, items each), which
for a large library/org can be minutes away. Two small, deliberately
narrow actions close that gap for the case where someone actually needs
one specific item fixed right now, without touching the cache's own
read/background-sync design at all:

- **Song Info: "Refresh song info"** - a button on the Build page's song
  detail card (`songDetailCard`, `song_detail_card.templ`), next to
  Duplicate/Remove/Replace. Handled by
  `RefreshSetSongArrangementInfo` (`song_arrangement_cache.go`), a thin
  wrapper that calls `fetchAndCacheArrangementInfo` - the same per-song
  fetch-and-upsert helper both the read-through cache-miss path
  (`resolveArrangementCacheInfo`) and `runSongInfoSync`'s batch loop
  already share - directly and synchronously for the one song id in that
  card. `fetchAndCacheArrangementInfo` has no "skip if already cached"
  guard (confirmed by reading it before adding this - it's a plain
  unconditional fetch-and-upsert), so no force variant was needed: calling
  it always overwrites whatever's currently cached, which is the entire
  point of this action (unlike `runSongInfoSync`'s batch backfill, which
  deliberately skips a song that's already cached). Cost: 2 live PCO calls
  (arrangement + key, same as any other single-song cache fill), bounded
  to one song, triggered by an explicit click - same reasoning the
  existing Sync-now/on-demand-warning-check actions in this doc already
  use for a live call outside the read path.

  This exists on a page that only ever shows an aggregate
  `CachedCount`/`TotalCount` progress line (`/scheduling/song-info`, see
  the Song Info addendum above) - there's no per-song list there to hang a
  per-row action off, so the action instead lives wherever a single song's
  arrangement info is actually displayed to someone building a set: the
  Build page's own song detail card, which already shows
  `SetSongView.ArrangementInfo` and already has a precedent for
  per-song action buttons (Duplicate/Remove/Replace, and the Tag/Relate/
  Mark-role quick actions).

- **Person-data cache: "Refresh availability data"** - a button on the
  Team Builder's person detail sidebar card (`personDetailCard`,
  `person_detail_card.templ`), next to Remove from position. Handled by
  `RefreshPersonData` (`rosters_person_data_refresh.go`), which looks up
  the clicked assignment's person/team ids and calls the exact three
  per-person sync functions (`syncPersonBlockouts`/`syncPersonPlanHistory`/
  `syncPersonTeamPlanHistory`, `person_data_cache.go`) the background pass
  already calls per person - just synchronously, for this one person,
  right now, instead of waiting on the paced queue. Cost: 3 live PCO calls
  (one `GetBlockouts`, two `GetPersonPlanPeople` - all-teams and
  this-team), bounded to one person, triggered by an explicit click - same
  reasoning as the song action above.

  Re-rendering after the sync reuses the existing scoped-mutation
  machinery (`buildRosterPositionGroupData`/`buildRosterLightData`,
  `rosters_scoped.go`) rather than a full page rebuild, matching every
  other Team Builder mutation in `rosters_detail.go` - the touched
  position group's own card (whose assigned-row badges already show the
  same Frequency/Blockout/CrossTeam warnings) is the primary swap target,
  plus an out-of-band update to the same person's sidebar card badges
  (`personBadgesOOB`) so that card reflects the refresh immediately too,
  since it lives outside the position group's own DOM subtree. That OOB
  update deliberately targets only the badges block
  (`personBadgesElementID`), never the whole card shell - the shell
  (`buildDetailCardShell`) always renders `hidden` by default and is only
  ever made visible by client-side JS (`selectPersonRow`,
  `scheduling.js`), so swapping the whole card via OOB would silently
  re-hide whatever card the person clicking Refresh is actively looking
  at.

**Deliberately not built**: a full org-wide/"nuclear" cache reset (wipe
every cached song's arrangement info, or every person's blockout/history
rows, and force a full re-sync). Both actions above are scoped to exactly
one item because that's what an explicit click naturally targets and
because it keeps the live-PCO-call cost small and bounded regardless of
library/org size; a bulk reset has no such natural bound and belongs on a
settings/account-level page this app doesn't have yet, not bolted onto
either cache's own read path. Out of scope for this change - deferred
until that page exists.

## Addendum: one new per-login GetServiceTypes probe call, gating sign-in on Services access

Not a hot-render-path risk like the fixes above - a new call, but a small
and deliberately bounded one, added to `auth.go`'s `Callback` (the OAuth
sign-in handler) rather than any page render. Before this, anyone who
could complete PCO's OAuth consent screen for an organization got a
session (and a billed seat, see `auth.SeatCounts`) regardless of whether
their PCO account had any access to the Services product at all.

`Callback` now makes exactly one extra live PCO call per login: a
`pco.GetServiceTypes(ctx, &pco.ServiceTypesParams{PerPage: 1})` probe,
authenticated as the person signing in, right after the existing
`GetOrganization` lookup succeeds. `PerPage: 1` keeps it the cheapest
possible request - the response body is discarded entirely; only whether
the call succeeded or came back 401/403 is used. A confirmed 401/403
blocks sign-in (no session created, no cookie set); any other error
(network blip, PCO 5xx, timeout) fails **open** and lets sign-in proceed,
matching this handler's existing tolerance for `UpsertOrganization`
failures a few lines below - an ambiguous failure shouldn't lock out a
legitimate admin. The 401/403-vs-other classification is factored into
`isServicesAccessDenied` (`auth.go`), unit-tested directly in
`auth_service_access_test.go` since `internal/auth`'s OAuth
token/userinfo calls have no `pco.SetBaseURLForTesting`-style seam to
drive a full handler-level `httptest` test the way PCO-data-fetching code
elsewhere in this doc gets tested.

Deliberately calls `pco.GetServiceTypes` directly rather than the
org-scoped, cached `scheduling.FetchServiceTypes` wrapper (the
"`GetServiceTypes` cached" addendum above) used by
`build.go`/`home.go`/`people.go`: that cache is keyed only by
organization id, not by which person's token made the call, so a cache
hit warmed by one admin's earlier session could wrongly let a different,
access-less person's login through this gate. This call is also
per-login only, not per-render, so there was no meaningful call-volume
reason to route it through the cache anyway.

Known, accepted ceiling (not something to revisit here): PCO's API, as
wrapped by `go-pco`, exposes no clean "this person's Services permission
level" field, so this probe can only distinguish "has some Services
access" from "has none" - it cannot tell PCO's own Viewer/Scheduler/
Editor/Administrator levels apart. Anyone with at least Viewer access
still signs in.

## Addendum: priority-aware request throttle shipped in go-pco, adopted here (off by default)

`pco-throttle-plan.md` (written 2026-09-02, prompted by a conversation
during the roster cold-start fix above) specced a real fix for the gap
this doc's mitigations above all work around by hand: Song Info's
background sync, the person-data cache's background sync, and ordinary
interactive traffic each self-throttle independently, tuned by hand
against a shared "60% of budget should leave enough headroom" assumption,
with no shared awareness of each other or of PCO's actual per-token
budget at any given moment. Full design (public API, correctness
reasoning for keying per access token rather than one global bucket,
scheduling semantics, the deliberate no-anti-starvation tradeoff, testing
plan) is in that doc - not repeated here.

Shipped in `go-pco` `v0.18.0`: `pco.ConfigureThrottle`/`pco.WithPriority`,
a per-token priority queue gated by a token bucket, off unless opted into
via `PCO_THROTTLE_ENABLED=true` (or an explicit `ConfigureThrottle` call).
This repo now pins that tag (`go.mod`) and:

- Tags both background syncs' contexts at priority 20 (admitted after the
  untagged default of 10) via `pco.WithPriority` -
  `song_arrangement_cache.go`'s `runSongInfoSync` launch and
  `person_data_cache.go`'s `runPersonDataSync` launch - so neither ever
  delays interactive traffic sharing the same token, once the throttle is
  actually turned on.
- Leaves interactive/render-path calls untagged (default priority) -
  no evidence yet that they need elevating above the default, and the
  plan is explicit not to do this speculatively.
- Does **not** enable the throttle itself. `PCO_THROTTLE_ENABLED` is
  documented in `README.md`'s Setup section but left unset - no consumer
  has hit an actual 429 yet (this doc's own mitigations above already
  keep call volume well under budget), so this is defense-in-depth ready
  to flip on, not a response to an observed problem. Turning it on is a
  separate, later operational decision, not bundled into this adoption
  step.
- Default rate, if/when enabled: 60 requests/20s (`PCO_THROTTLE_RATE_PER_20S`,
  matching this app's own existing 60%-of-ceiling convention above) -
  deliberately more conservative than PCO's raw 100/20s ceiling or
  go-pco's own originally-proposed 80/20s default, since raising it later
  with real production signal is safer than shipping close to the wire
  and hoping it holds.

**Deliberately not done** (per the plan's own "Adoption in this repo"
step 5): the existing hand-tuned pacing this addendum's intro calls
out - `songInfoSyncBatchSize`/`songInfoSyncBatchPause` and the
person-data cache's equivalent constants - is left exactly as-is. Those
loops remain the real safety net until the throttle has actually run in
production for a while with `PCO_THROTTLE_ENABLED=true`; simplifying them
to a plain bounded fan-out before then would mean removing a working
safety net before its replacement is even turned on, let alone proven.
