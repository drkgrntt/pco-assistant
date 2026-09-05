# Plan: PCO call-efficiency rollout (for a new session)

**Status: shipped 2026-09-04.** All three phases below landed (see each
phase's own "Status" line) - kept as the reference doc for the design
rationale behind the code it's cited from throughout `internal/`
(`nav.go`, `home.go`, `sets_builder.go`, `pco_songs.go`,
`person_data_cache.go`, and others point back here for "the full design").

Written 2026-09-04. This bundles everything from one conversation into
concrete, buildable phases for a fresh session to pick up - it's an
execution plan, not new design. Two other docs already hold the actual
research/rationale; **read them before touching code**, this doc won't
re-derive what they already establish:

- `docs/pco-api-risks.md` - read the whole thing first (its own opening
  line says the same). Its "Live-verified PCO API facts" section has the
  three `include=` findings Phase 1 below implements, each with the exact
  live-verified `can_include` values and why every other endpoint this
  app calls was checked and ruled out.
- `docs/plans/shipped/pco-background-sync-plan.md` - the full design for Phase 3 below
  (unattended cache warming via borrowed idle session tokens). Explicitly
  marked **not urgent, written up for later** - don't build it just
  because it's next in this list; see "Phase 3" below for the actual
  gating.

## Order and why

**Phase 1 (go-pco `include=` wins) first** - smallest, most mechanical,
zero design ambiguity (everything's already live-verified), and it
directly reduces the call volume every other phase's testing/rollout has
to reason about. No dependency on the other phases.

**Phase 2 (Song Info queue-on-miss) second** - independent of Phase 1,
but do it second anyway: smaller and more certain than Phase 3, and it's
the one piece from the conversation that was explicitly deferred and
never fully specced - this doc specs it now (see below).

**Phase 3 (background-sync cron) last, and only if explicitly asked
for** - `docs/plans/shipped/pco-background-sync-plan.md` already states its own
condition for building it ("revisit only if staleness genuinely proves
to matter... not speculatively"). That condition hasn't changed. Don't
start Phase 3 from this doc alone without checking with the user first -
see that plan's own opening section.

---

## Phase 1: go-pco `include=` wins

Three call sites, one shared go-pco change. Full rationale and the exact
live-verified `can_include` values are in `docs/pco-api-risks.md`'s
"Live-verified PCO API facts" - this section is just the task breakdown.

### 1a. go-pco: add `Include []string` to three params structs

**Status: shipped 2026-09-04, go-pco `v0.21.0`.**

This is a change to the separate `go-pco` module
(`/home/drkgrntt/projects/go-pco` locally), not this repo - same
"lives in go-pco first" situation as the throttle work
(`docs/plans/shipped/pco-throttle-plan.md`). `PeopleParams`/`TeamMembersParams`/
`PersonPlanPeopleParams` already have this field and already wire it into
the query string via `.Include(params.Include...)` - mirror that exact
pattern for:

- `ArrangementsParams` (`arrangements.go`) - `GetArrangements` needs
  `include=keys` support.
- `ItemsParams` (`items.go`) - `GetItems` needs `include=item_notes`
  support (also exposes `song`, `arrangement`, `key` - not needed by this
  plan, but no reason to restrict the field to fewer values than PCO
  actually supports).
- `NeededPositionsParams` (`neededPositions.go`) - `GetNeededPositions`
  needs `include=team` support.

Also add one exported accessor this repo's Phase 2 needs (see "Trigger
mechanism" below):

```go
// AccessTokenFromContext returns the access token WithAccessToken stored
// on ctx, if any. Exists so a caller holding a context built earlier in a
// request (rather than the original *fiber.Ctx or a bare token string)
// can still construct an equivalent, independently-lived context for a
// background goroutine - see pco-assistant's Song Info queue-on-miss sync
// for the motivating case.
func AccessTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(accessTokenKey).(string)
	return token, ok
}
```

Bump go-pco's version, then bump this repo's `go.mod` to it - same
sequence as every previous go-pco version bump in this project's history.

### 1b. This repo: `fetchAndCacheArrangementInfo` (`scheduling.go`)

**Status: shipped 2026-09-04.** See `docs/pco-api-risks.md`'s changelog
entry for what landed (`keysFromIncluded`). Section left as-written below
for reference.

Currently: `GetArrangements(ctx, songID, &pco.ArrangementsParams{PerPage: 1})`
then, on success, a second `GetKeys(ctx, songID, arrangement.ID, ...)` call.

Change to one call: `GetArrangements(ctx, songID, &pco.ArrangementsParams{
PerPage: 1, Include: []string{"keys"}})`, then read the Key resource out
of the response's `Included` array instead of a second call. `Included`
is `[]any` (raw JSON), same shape `teamNamesFromIncluded`/
`planSortDatesFromIncluded` (`rosters_builder.go`) already parse from a
`PersonPlanPeopleParams`-style response - write a small
`keysFromIncluded`-style helper following that exact precedent (match a
`"type": "Key"` entry, decode its attributes) rather than inventing a new
parsing convention. Keep the existing "only set `key`/`keyID` when a real
Key row exists" logic (`fetchAndCacheArrangementInfo`'s current
lines ~102-113) unchanged - just source the Key data differently.

### 1c. This repo: item notes in `renderServiceOrder`/`buildSetBuilderData` (`sets_builder.go`)

**Shipped 2026-09-04** - see `docs/pco-api-risks.md`'s changelog for what
landed and the `RunConcurrently`-kept reasoning. Section left as-written
below for reference.

Currently: `buildSetBuilderData` calls `GetItems(ctx, serviceTypeID,
planID, nil)` once, then `renderServiceOrder` fans out one `GetItemNotes`
call per published-song row via `scheduling.RunConcurrently` (see
`buildSongNoteViews`, `scheduling.go`).

Change: call `GetItems(..., &pco.ItemsParams{Include: []string{"item_notes"}})`,
build a `map[itemID][]pco.ItemNoteData` from the response's `Included`
array once (same `Included`-parsing precedent as 1b), and pass that map
into `renderServiceOrder`/`buildSongNoteViews` instead of having
`buildSongNoteViews` call `GetItemNotes` itself. `buildSongNoteViews`
becomes a pure function over that map (no `ctx`/PCO call needed at all)
- its `noteCategories`-matching logic (lines ~1243-1258) stays exactly as
is, only where the raw notes come from changes. This also means
`renderServiceOrder`'s `RunConcurrently` fan-out (added specifically to
parallelize per-row PCO calls) may no longer need to be concurrent for
this reason once `resolveArrangementCacheInfo`'s cache-miss path is the
only remaining live-call source per row - don't remove the concurrency
without checking whether 1b above also already avoided a per-row PCO
call in the common case (it should have, after Phase 1b - cache hits are
already PCO-free), or whether the concurrency now has nothing left to
parallelize.

### 1d. This repo: `GetTeams` elimination (`rosters_scoped.go`, `rosters_builder.go`)

**Status: shipped.** See `docs/pco-api-risks.md`'s changelog entry for
what actually landed (`teamInfoFromIncluded`, `rosterSharedPCOData.TeamInfo`).

Currently: `fetchRosterSharedPCOData` (`rosters_scoped.go`) makes
`GetTeamMembers`, `GetNeededPositions`, and `GetTeams` - three calls per
plan - purely to resolve team IDs to names (`GetTeams`' only use anywhere
in this codebase, confirmed by grep). `buildRosterBuilderData`
(`rosters_builder.go:315`) has its own separate `GetTeams` call for the
same reason, in the full-rebuild path.

Change: call `GetNeededPositions(..., &pco.NeededPositionsParams{
Include: []string{"team"}})` in both places, parse Team resources out of
the response's `Included` array (same precedent again), and delete both
`GetTeams` calls and the `Teams`/`TeamsErr` fields from
`rosterSharedPCOData` entirely - replace every read of
`shared.Teams.Data`/`shared.TeamsErr` (`rosters_scoped.go:166-172`,
`:387-391`) with a lookup into the new team-name map built from
`Needed`'s `Included`.

### Testing (Phase 1)

No live PCO needed for any of this - `Included`-array parsing is pure
JSON decoding, same testability as `teamNamesFromIncluded`/
`planSortDatesFromIncluded` already have. Add table-driven tests for the
new `keysFromIncluded`-style helper(s) matching those two's existing test
shape. `fetchAndCacheArrangementInfo`'s own existing tests (if any) need
updating for the new call shape; same for anything asserting
`fetchRosterSharedPCOData`'s three-call behavior. No DB-backed harness
needed - this phase touches PCO parsing only, not caching/storage.

---

## Phase 2: Song Info queue-on-miss (kills the button/nag)

**Status: shipped 2026-09-04.** See `docs/pco-api-risks.md`'s changelog
entry for what landed - the queue/trigger architecture below was followed
exactly, including the "trigger mechanism" decision (2c): the recommended
in-function approach (`buildSetBuilderData`/`buildSetBuilderDataWithErr`
calling `triggerSongInfoQueueSync` once via `pco.AccessTokenFromContext`/
`logger.RequestIDFromContext`), not the ~20-call-site fallback. Section
left as-written below for reference.

Converts `resolveArrangementCacheInfo`'s cache-miss path
(`scheduling.go`) from block-and-fetch-inline to queue-and-batch,
mirroring `person_data_cache.go`'s architecture exactly - see that
file's own header comment for the pattern being cloned. This is what
removes the home-page "Finish syncing" nudge and the standalone
`/scheduling/song-info` page's reason to exist, per the conversation this
plan comes from.

### 2a. New queue, mirroring `personSyncPending`/`personSyncInProgress`

Add to `song_arrangement_cache.go`:

- `songSyncRequest{organizationID, songID string}` (comparable struct,
  same shape as `personSyncRequest` minus the kind/team fields Song Info
  doesn't need).
- `songQueuePending map[songSyncRequest]bool` guarded by the *existing*
  `songSyncMu`. **Do not add a second mutex** - reuse `songSyncMu` so a
  queued-drain pass and the existing whole-library `SyncSongInfo` bulk
  pass can never race for the same org (both should respect the existing
  `songSyncInProgress` guard).
- `enqueueSongSync(req songSyncRequest)` - mirrors `enqueuePersonSync`
  exactly (lock, set, unlock).
- `triggerSongInfoQueueSync(ctx context.Context, store *scheduling.Store, organizationID string, accessToken, requestID string)` -
  mirrors `triggerPersonDataSync`'s check-in-progress-then-drain-pending
  sequence under one lock, builds the same
  `pco.WithPriority(pco.WithAccessToken(logger.WithRequestID(
  context.Background(), requestID), accessToken), 20)` context
  `SyncSongInfo`/`triggerPersonDataSync` both already build, and launches
  a background goroutine running the drained song IDs through
  `songSyncBatches`/`runPacedBatches` (already exist, unchanged) calling
  `fetchAndCacheArrangementInfo` per song - essentially `runSongInfoSync`'s
  tail end (batching + pacing + best-effort logging), factored out to
  accept a caller-supplied song ID list instead of computing "needs sync"
  itself by diffing the whole library. Factor that shared tail out of
  `runSongInfoSync` into its own function both it and this new path call,
  rather than duplicating the batching loop.

### 2b. `resolveArrangementCacheInfo`'s cache-miss path (`scheduling.go`)

Change from:

```go
info, err := fetchAndCacheArrangementInfo(ctx, store, organizationID, songID)
if err != nil {
    return scheduling.SongArrangementInfo{}
}
return info
```

to enqueueing and returning immediately, matching
`person_data_cache.go`'s read path exactly (never block a render on a
live PCO call):

```go
enqueueSongSync(songSyncRequest{organizationID: organizationID, songID: songID})
return scheduling.SongArrangementInfo{}
```

### 2c. Trigger mechanism - a real decision, not just plumbing

`person_data_cache.go`'s `triggerPersonDataSync(c *fiber.Ctx)` is called
explicitly from each of ~6 named roster-rendering handlers, right after
each builds its response. Song Info's read path
(`resolveArrangementCacheInfo`) is reached from **`buildSetBuilderData`/
`buildSetBuilderDataWithErr`**, which is itself called from **~20
call sites** across `build.go`, `sets_detail.go`, `sets_publish.go`,
`sets_reorder.go`, `run_sheet.go`, and `song_arrangement_cache.go` (grep
`buildSetBuilderData(\|buildSetBuilderDataWithErr(` to confirm the
current count before starting - it may have grown). Copying
`person_data_cache.go`'s per-handler-call convention here means ~20
mechanical edits, and a new call site added later has to remember a
20th-plus copy of the same line - fragile in a way the roster case isn't
(6 well-known handlers vs. 20+ and growing).

**Recommended instead**: call the trigger from *inside*
`buildSetBuilderData`/`buildSetBuilderDataWithErr` themselves, once,
right before they return - every caller gets it for free, new call
sites too. The blocker: those two functions take a plain
`context.Context`, not a `*fiber.Ctx`, so they have no direct way to get
the raw access token/request id `triggerPersonDataSync`'s construction
needs. This is exactly what 1a's `pco.AccessTokenFromContext` accessor
is for - pull the token back out of the `ctx` already being passed in
(`token, ok := pco.AccessTokenFromContext(ctx)`), pair it with
`logger`'s existing request-id-from-context extraction (check
`logger.WithRequestID`'s counterpart for reading one back out, or thread
a `requestID string` param through if none exists), and skip the trigger
entirely if `!ok` (matches every other best-effort pattern in this
codebase - a context built without a token, e.g. a cron in Phase 3, just
doesn't get one).

If that accessor turns out to be awkward in practice, the fallback is
the person-data-cache convention (call `triggerSongInfoQueueSync`-wrapping-
`triggerSongInfoSync(c)` from each of the ~20 handlers) - slower to wire
up and more fragile long-term, but zero design risk. Decide once actually
touching the code, not from this doc alone.

### 2d. Remove the button/nag

- Delete `SongInfoPage`/`SyncSongInfo` handlers and their routes
  (`scheduling.go:372-373`) - no more standalone `/scheduling/song-info`
  page. `buildSongInfoData` likely becomes dead code too; confirm nothing
  else calls it before deleting (it's currently also read by
  `HomeController.buildHomeData` - see next bullet).
- Remove the home-page nudge: `HomeData.SongInfoCached`/`SongInfoTotal`
  (`home_types.go`), the `buildSongInfoData` call and both assignments in
  `home.go:127-129`, and both `if data.SongInfoTotal > 0 && ...` blocks in
  `home.templ` (the "Finish syncing" paragraph and the Setup-section list
  item).
- **Do not add a new whole-library "resync" button to the Account page.**
  Considered during the conversation this plan comes from and decided
  against: the existing `ClearSongInfoCache` action already on Account
  (`account.go`) covers "force a fresh state" (wipe, then queue-on-miss
  refills it as songs are actually touched), and Phase 3 (if it ships)
  covers "refill without anyone visiting." A dedicated resync button adds
  a third, redundant way to trigger the same outcome as the other two -
  skip it unless a real gap shows up once Phases 1-2 are live.

### Testing (Phase 2)

Follow `person_data_cache_test.go`'s shape for the pure logic (queue
dedup, the check-in-progress-then-drain sequence against a plain map -
`songSyncTryStart` is already unit-tested this way, reuse it unchanged).
New coverage needed: `enqueueSongSync`/queue-drain interaction with the
existing `songSyncInProgress` guard (confirm a queue-drain pass and a
bulk `SyncSongInfo` pass - if that handler still exists in some form, or
its replacement - can't run concurrently for the same org), and
`resolveArrangementCacheInfo`'s miss path now returning a zero-value
immediately rather than blocking (a fast, deterministic unit test, unlike
today's version which would need a live/mocked PCO call to test the
miss branch at all).

---

## Phase 3: background-sync cron (only if explicitly requested)

**Status: shipped 2026-09-04**, on explicit user request (the "not urgent"
gating below applied until then). Full design in
`docs/plans/shipped/pco-background-sync-plan.md` (now marked shipped there too) - see
`docs/pco-api-risks.md`'s changelog entry for exactly what landed.

Full design already written: `docs/plans/shipped/pco-background-sync-plan.md`. Nothing
to add here beyond the gating already stated at the top of this doc and
in that plan's own opening section - re-read its "not urgent" framing
before starting, and confirm with the user this condition has actually
changed rather than assuming this rollout plan is blanket permission to
build all three phases.

---

## Rollout

Real, non-trivial code change (all three phases) - branch, commit, push,
and **wait for explicit "merge and deploy"** per CLAUDE.md's standard
sequence - don't merge/deploy on your own judgment just because it builds
and tests pass, same as every other change in this area. Phase 1 touches
a separate repo (go-pco) - that half needs its own version bump/tag
before this repo's `go.mod` can pick it up; sequence accordingly rather
than trying to land both in one PR.

Update `docs/pco-api-risks.md`'s "Current state" table and changelog once
each phase ships - Phase 1 closes out the three `include=` findings
(mark them applied, not just discovered), Phase 2 adds a line for the
Song Info queue-on-miss conversion. `docs/ideas.md`'s "cache all" plan
doc reference (if any) and `README.md`'s Song Info description should
also be checked for drift once the button/page are actually gone - see
CLAUDE.md's "Keeping the docs current" section.
