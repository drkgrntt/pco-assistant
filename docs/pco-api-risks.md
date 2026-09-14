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
| Song Info sync (blocked a render on a cache miss; also had a whole-library "sync now" button/nag) | fixed | queue-on-miss: a miss enqueues and returns immediately, `buildSetBuilderData` drains the queue in a background goroutine (paced batches, 10/batch, 10s pause, ~60% of budget) - no more button/page, see Phase 2 changelog entry |
| Team Builder candidate eligibility pool (scales with volunteer pool, not the roster) | fixed | candidate warnings computed on-demand (button click), not eagerly for every candidate |
| Team Builder per-person blockout/cross-team/frequency (live call every render) | fixed | local Postgres cache (migration `00033`) + paced background sync, same architecture as Song Info |
| Roster shared-fetch duplication (`GetTeamMembers`/`GetNeededPositions`/`GetTeams` fetched twice per click) | fixed | `rosterSharedPCOData` - fetch once, pass to both scoped builders |
| Roster `GetTeams` call (a whole extra request per plan, just to resolve team ids to names) | fixed | `include=team` sideloaded on the `GetNeededPositions` call already being made - `GetTeams` eliminated, shared roster fetch now 2 calls not 3 |
| `GetServiceTypes` (called independently by 3 pages) | fixed | cached `FetchServiceTypes`, 60s TTL, same shape as `FetchActiveSongs` |
| `fetchRecentPlanHistory` (uncached, would've fired per debounced keystroke once candidates went search-driven) | fixed | cached per service type, 60s TTL |
| Home page (`buildHomeData`, sequential per-service-type) | fixed | `RunConcurrently` fan-out |
| Home page "needs attention" look-ahead (second row when the next plan's already fully planned) | mitigated | bounded to `homeLookAheadPlansCap` (3) candidates, rides the existing `GetPlans` call (`PerPage` bumped, not a second call) - checked in order, stops at the first unplanned candidate |
| Publish/Refresh writes (sequential per song/assignment) | fixed | `RunConcurrently` fan-out, with ordering/error-collection made goroutine-safe (see Design decisions) |
| Sustained-rate ceiling across all traffic on one token | enabled (prod) | go-pco `ConfigureThrottle`, `PCO_THROTTLE_ENABLED=true`, 60 req/20s default, background syncs tagged low-priority |
| A 429 that gets through anyway | enabled (prod) | go-pco `ConfigureRetry`, `PCO_RETRY_ON_429_ENABLED=true`, waits `Retry-After`, 1 retry |
| Visibility into PCO failures | shipped | `pco.SetResponseHook` logs every failure (never a success) through this app's logger; a 429 also now surfaces to the *user*, not just the log - a calm, non-dismissible "some details may be stale" banner on the next full page load for that request (Phase 1 of `docs/plans/in-progress/pco-rate-limit-user-visibility-plan.md`) - see that doc's status note for the file list. Per-row marking of *which* fetch failed (Phase 2) is still open |
| Unattended sync (Song Info + person-data refresh + person-data recent-history discovery) | shipped | hourly `jobs.PCOJobs` cron, one org at a time, borrowing and refreshing that org's own idle session token - reuses the exact same paced/guarded sync entry points (`triggerSongInfoQueueSync`, `triggerPersonDataSyncCtx`) an ordinary render already triggers, low-priority (`pco.WithPriority`, 20) |
| Live-sync poll-while-open (`SyncCheck`, one `GetItems` per open Build-page Songs tab per 30s) | shipped | poll-while-open only (option C) - bounded by concurrent viewers, not total org/plan count; tagged `pco.WithPriority(ctx, 20)` so a poll tick never contends with interactive traffic on the same token; a failed poll tick is treated as "no change," never surfaced as an error |
| Team/position lists and team-member names (fetched live, uncached, on every roster render) | fixed | cached `fetchRosterSharedPCOData` (`rosters_shared_cache.go`), 20s TTL - shorter than the 60s norm since team membership can change mid-session; manual "Refresh team data" button is the remedy for the remaining window |
| Build page "Past plans" tab (`fetchPastPlans`, uncached, no date bound - walked a service type's *entire* PCO plan history on every tab open/search/Prev/Next click) | fixed | cached `fetchPastPlansCached` (`build_past_plans_cache.go`), keyed by (organization, service type), 30min TTL; default fetch itself windowed to the last `buildPastPlansWindowMonths` (18) via `fetchPastPlansWindowed`'s early-exit paging, with a search that misses the window falling back to one uncached full/unbounded fetch (`fetchPastPlansForSearch`) so an older plan stays reachable |
| Song attachments (lead sheets/chord charts) | shipped | cached at sync time alongside arrangement info (`fetchAndCacheSongAttachments`, one more PCO call per uncached song - Song Info sync is now 2 calls/song, not 1); the attachment's own `url` is never cached, a click-triggered route live-fetches a fresh one instead |
| Song leader -> real PCO "Song leader" sync (`syncLeaderAssignment`, `sets_publish.go`, go-pco `ItemAssignment`) | shipped | `PublishSet` rides its existing `GetItems` call (adds `Include: []string{"item_assignments"}` only when at least one song in the set currently has a leader set - zero new calls otherwise); `RefreshSongInfo` previously made no `GetItems` call at all, so it gains one new per-plan `GetItems(Include: item_assignments)` call, but only when a song in the set has a leader set (opt-in cost, not paid by every org); a leader change itself costs one `CreateItemAssignment`/`DeleteItemAssignment` per affected song, same "only the songs that actually changed" gating `InfoPending` already provides for every other per-song write. (An earlier, separate vocal-notes-line sync, `syncLeaderNote`, rode the same calls alongside this one for a few hours on 2026-09-09 before being removed once this real assignment existed - see the Changelog entry below.) |
| "Browse all songs" title-click detail popover (`BuildCandidateDetails`, `sets_builder.go`) | shipped | one song per click, not the whole library - `candidateSongView`'s general-purpose lookup (used by the up-to-50-row search results too) stays cache-only with no fetch, but this single-song handler resolves arrangement info through `resolveArrangementInfo`/`resolveArrangementCacheInfo` instead, the same enqueue-on-miss path the main service order already uses: a cache hit costs nothing new, a miss enqueues into the existing paced Song Info sync queue (`enqueueSongSync` + a `triggerSongInfoQueueSync` deferred in this handler) rather than fetching inline, so an uncached song's info fills in on a later click/reload instead of staying blank forever - never a synchronous per-click PCO call |
| "Add item to plan" (`AddPlanItem`, `plan_items.go`) | shipped | one `CreateItem` call, no pre-fetch - see the sequence-auto-append fact below. Rides the same full-page rebuild every other Build-page mutation already pays (`buildSetBuilderDataWithErr`); no caching added or needed, since it's a rare, user-triggered click, not a render-path/hot-path call |
| Edit/remove a plan item (`UpdatePlanItem`/`RemovePlanItem`, `plan_items.go`) | shipped | one `UpdateItem`/`DeleteItem` call each, same "rare user click, not a render path" reasoning as the row above - no caching |
| Drag-and-drop reposition of a header/foreign item (`RepositionPlanItem`, `plan_items.go`) | shipped | one uncached `GetItems` fetch (must be live, not cached - the whole point is computing a correct new order against the plan's *current* real sequence; a cached/stale list would silently reorder against the wrong baseline) plus one `ReorderItems` call, skipped entirely when the computed order matches the current one (`itemOrderEqual`) - see the `item_reorder` full-list-contract fact below |

## Live-verified PCO API facts (worth not re-checking)

- **PCO's Services UI "Song leader" field is NOT an Item/Song/Arrangement
  attribute or relationship at all** - confirmed live (2026-09-09,
  `docs/plans/shipped/worship-leader-pco-sync-plan.md`): `Item`, `Song`,
  and `Arrangement` were each fetched and dumped in full against a real
  account with no "leader" field anywhere. It's a separate resource,
  `ItemAssignment` (`services/v2/.../items/{item_id}/item_assignments`,
  now wrapped in go-pco `v0.24.0+`, `itemAssignments.go`) - one row per
  assignable (a `Person`, or per PCO's docs an open `TeamPosition`
  placeholder not yet exercised by this app), linked to one plan `Item`.
  Confirmed both against PCO's machine-readable vertex docs (`GET
  .../documentation/2018-11-01/vertices/item_assignment`) and live by
  reading back a real assignment created through PCO's own UI. **Used**:
  `syncLeaderAssignment` (`sets_publish.go`/`leader_note.go`) writes this
  app's local `SetSong.LeaderPersonID` here on Publish/Refresh - see that
  row in Current state above. (Briefly wired in alongside a pre-existing
  vocal-notes-line sync, then that sync was removed once this real
  assignment existed - see the Changelog entry below.) It sideloads via
  `Include: []string{
  "item_assignments"}` on `GetItems`, the same sideload mechanism already
  used for `item_notes` - confirmed live that `item_assignments` is a
  valid `Include` value on the `Item` resource itself (`meta.can_include`
  lists it), so this needs no separate per-item `GetItemAssignments` call.
  `ItemAssignment` has **no update endpoint** (`can_update: false` in the
  vertex docs) - reassigning is delete-and-recreate, the same convention
  `CreateTeamMember`/`UpdateTeamMember` already use for a position
  assignment.
- **`ItemAssignment#create` rejects a person who isn't already scheduled
  on the plan** - confirmed live (2026-09-09): `CreateItemAssignment`
  with a `PersonID` that has no `PlanPerson`/team-member row on that plan
  fails with a 422, `Errors[0].Detail` (and `Error()`'s rendered text)
  reading exactly `Invalid assignable id` - no other detail, no field
  name. This app's local `SetSong.LeaderPersonID` had no equivalent limit
  before this was caught, so the song leader picker could (and did) offer
  any org person, not just this plan's own team - a real gap, not a
  hypothetical one, reported directly by live use. Fixed by scoping the
  picker to `SetBuilderData.RosterPersonOptions` (this plan's own local
  roster - `buildSetBuilderData`/`rosterPersonOptions`, `sets_builder.go`)
  instead of the general org-wide person search every other
  `PersonPickerField` on this app uses; `leaderAssignmentErr`
  (`leader_note.go`) still special-cases this exact 422 into a readable
  message for the one case scoping the picker can't fully prevent - a
  roster assignment that's only a local draft, never yet pushed to real
  PCO via the Team tab's own Publish, isn't a real team member yet
  either.
- **`Plan.updated_at` does NOT reflect item-level changes** - confirmed
  live (2026-09-05, live-sync-reflection plan) via a PATCH-and-check round
  trip: editing an item's title/description/key does not bump the plan's
  own `updated_at`. go-pco's `PlanAttributes` (`v0.21.0`, `plans.go`)
  doesn't even map this field. **Don't use `GetPlan` for any kind of
  item-level change detection** - `Plan.ItemsCount` (`PlanAttributes.
  ItemsCount`) only moves on item add/remove, not edits or reorders
  either. The only reliable signal is a real `GetItems` call - see
  `pcoItemsFingerprint` (`sets_builder.go`) below.
- **A pure reorder does NOT bump the moved items' own `updated_at`** -
  confirmed live (2026-09-05): reordering two items via
  `pco.ReorderItems` (the `item_reorder` plan action) left both items'
  `Item.UpdatedAt` unchanged. `Item.UpdatedAt` DOES bump on any actual
  attribute edit (title/description/key/etc). **Used**: the live-sync
  poll-while-open check's fingerprint (`pcoItemsFingerprint`,
  `sets_builder.go`) hashes every item's `(ID, Sequence, UpdatedAt)`
  together, not `UpdatedAt` alone, specifically because a reorder is
  invisible to `UpdatedAt` by itself.
- **`Item#create` auto-appends when `sequence` is left at 0** - confirmed
  live (2026-09-09): creating an item with `sequence: 0` sent explicitly
  (go-pco's `CreateItem` always sends the field, never omits it) still
  lands it after the plan's real, current last item, not at position 0 -
  PCO treats 0 as "no sequence given," not "put it first." **Used**:
  `AddPlanItem` (`plan_items.go`) skips the pre-create `GetItems` fetch a
  naive "compute next sequence myself" approach would otherwise need,
  cutting that call site to one `CreateItem`, not two calls.
- **`Item#create`'s `length` is silently dropped for `item_type: "header"`**
  - confirmed live (2026-09-09): a header item created with a non-zero
  `length` comes back `0` regardless, even though the create itself
  succeeds - Planning Center just doesn't track a duration for a section
  divider. **Used**: `AddPlanItem`'s own item-type picker hides/disables
  its length fields for "Section header" (`toggleAddPlanItemLengthFields`,
  `scheduling-build.js`) rather than showing fields that quietly do nothing.
- **`Item#create`'s `item_type: "media"` doesn't stick** - confirmed live
  (2026-09-09): comes back silently downgraded to a plain `"item"` (no
  attached media resource, so there's nothing for PCO to actually treat as
  media). **Used**: `AddPlanItem`'s item-type picker only offers Item/
  Section header, not Media.
- **`Item#update` (`PATCH`) drops a header's `length` too, same as create**
  - confirmed live (2026-09-09): PATCHing a header item's `length` to a
  non-zero value comes back `0` regardless, same silent no-op as
  `Item#create`'s own version of this fact above. Title/Description PATCH
  normally. **Used**: `UpdatePlanItem`'s edit form (`planItemEditForm`,
  quick_action.templ) simply omits the length fields for a header, rather
  than showing fields that quietly do nothing.
- **`Item#create`'s `sequence` auto-appends even when explicitly `0`, and
  `item_reorder` wants literally every item, in order** - confirmed live
  (2026-09-09): go-pco's `CreateItem` always sends `"sequence": 0`
  explicitly (never omitted), and PCO still appends to the end rather than
  treating that as "position 0" - see `AddPlanItem`'s own doc comment.
  Separately, `pco.ReorderItems`'s `item_reorder` action was confirmed to
  take the plan's *complete* ordered item-id list, not a partial move - the
  same contract `PublishSet`'s own `finalOrder` build already relied on
  (`sets_publish.go`). **Used**: `RepositionPlanItem`'s
  `buildReorderedItemIDs` starts from the plan's real, current item list
  (a fresh `GetItems`) and moves exactly one id within it, rather than
  trying to construct a request PCO would accept as a smaller diff (no
  such smaller-diff option exists).
- **`UpdateItemParams` has no `ItemType` field at all** (go-pco) - an
  item's type can't be changed after creation via `Item#update`, only set
  at `Item#create` time. **Used**: `UpdatePlanItem`'s edit form has no
  type control - editing a header can't turn it into a plain item, or
  back; deleting and recreating it is the only way, same as it always was.
- People#index (`GetPeople`) supports batched `where[id]=1,2,3` -
  `PeopleParams.IDs` in go-pco `v0.15.0`+.
- **`PersonAttributes.Avatar`** (a real uploaded photo, or PCO's own
  generated colored-initials image if the person has none) comes back on
  every `People#index`/`#show` response with no extra `Include` needed -
  **used** (2026-09-05, build-page redesign - see
  `docs/plans/shipped/build-page-redesign-plan.md`): `personLookupMapForIDs`
  (`people.go`) now keeps it alongside the name from the exact same batched
  call `personNameMapForIDs` already made, so the roster builder's
  avatars (assignment/candidate/suggestion rows, person-picker search
  results) cost zero additional PCO calls. Don't add a separate
  avatar-fetch path if this area changes again - it's already free on this
  call.
- Services v2 blockouts (`services/v2/people/{id}/blockouts`) has **no**
  batch/`where[id]` option - `can_query_by` is just `group_identifier`, so
  concurrency/caching are the only levers, batching isn't available.
- `GetPersonPlanPeople` is called with `PerPage: 25` in both existing call
  sites; whether it tolerates a higher `PerPage` (PCO's usual ceiling is
  100) without losing per-team accuracy for a multi-team volunteer is
  **not yet confirmed** - would let `crossTeamAssignmentsThisWeek` and
  `otherAssignmentDates` share one cached table instead of two if it checks
  out (`docs/plans/shipped/pco-roster-cold-start-plan.md` has the full design fork).
- Whether PCO exposes a global/batch blockouts listing (vs. only the
  per-person nested resource above) is also **not yet confirmed** - would
  eliminate the per-person blockout cache entirely if it exists.
- Songs#index supports server-side `where[hidden]` filtering - confirmed
  `true`/`false` return different, correct `total_count`s.
- **Arrangements#index/#show supports `include=keys`** - confirmed live
  against a real song/arrangement (`meta.can_include` lists `keys`,
  `sections`; `?include=keys` returns the arrangement's Key resource(s)
  sideloaded in `included`). **Used** (2026-09-04, Phase 1b):
  `fetchAndCacheArrangementInfo` (`scheduling.go`) now calls
  `GetArrangements(..., &pco.ArrangementsParams{PerPage: 1, Include:
  []string{"keys"}})` and reads the Key resource out of that response's
  `Included` array (`keysFromIncluded`) instead of a second `GetKeys`
  call - halves Song Info's whole-library sync cost (the "~200 calls for
  a 100-song library" figure).
- **Song Attachments' response shape confirmed live (2026-09-06)** - the
  original build shipped `pco.GetAttachments`/`AttachmentAttributes`
  without the live check `docs/plans/shipped/song-lead-sheet-attachments-plan.md`
  itself calls for (sandboxed build environment, no reachable PCO
  credentials). Confirmed as a same-day follow-up by creating and deleting
  a real throwaway attachment against a dev org song
  (`upload.planningcenteronline.com/v2/files` then `POST
  .../songs/:id/attachments`, then `DELETE`). Result: the field names
  already wrapped (`filename`, `content_type`, `file_size`, `url`,
  `created_at`/`updated_at`) are all real and correct - no silent-blank
  risk after all. But **`url` is Planning Center's own attachment web
  page** (`https://services.planningcenteronline.com/attachments/:id`),
  **not a file link** - a materially different fact than "might be an
  expiring pre-signed URL," the risk the original build defended against.
  The actual file lives behind a separate, unwrapped action: `POST
  .../songs/:song_id/attachments/:id/open` (a bare GET on that path
  returns only its own documentation, confirmed live) returns an
  `AttachmentActivity` resource whose `attachment_url` attribute holds the
  real file link. `OpenSongAttachment` redirects to `url` (PCO's page),
  not the unwrapped `/open` flow - a reasonable v1 (that page handles
  auth/viewing/downloading for an already-PCO-authenticated user), just
  worth knowing precisely what it does rather than assuming "opens the
  file." Actually streaming/downloading the file would mean wrapping
  `/open`+`AttachmentActivity` in go-pco - not done, not currently
  planned.
- **Items#index supports `include=item_notes`** (also `song`,
  `arrangement`, `key`, among others - confirmed live via `meta.can_include`
  on a real plan's items) - **used** (2026-09-04, Phase 1c): `buildSetBuilderData`
  (`sets_builder.go`) now calls `GetItems(..., &pco.ItemsParams{Include:
  []string{"item_notes"}})` and `renderServiceOrder`/`buildSongNoteViews`
  read every published song's notes out of a `map[itemID][]pco.ItemNoteData`
  built once from that response's `Included` array
  (`itemNotesFromIncluded`), instead of one `GetItemNotes` call per
  published song. `buildSongNoteViews` is now a pure function with no PCO
  call of its own; `renderServiceOrder`'s `RunConcurrently` fan-out was kept
  at the time this landed (Phase 1b, which collapsed
  `resolveArrangementCacheInfo`'s cache-miss path to one call, hadn't
  shipped yet) - collapsed to a plain sequential loop in Phase 2 below,
  once that cache-miss path stopped making any PCO call at all.
- **NeededPositions#index and TeamMembers#index both support
  `include=team`** (confirmed live) - now used: `fetchRosterSharedPCOData`
  (`rosters_scoped.go`) and `buildRosterBuilderData`'s full-rebuild path
  (`rosters_builder.go`) both call `GetNeededPositions(...,
  &pco.NeededPositionsParams{Include: []string{"team"}})` and parse Team
  resources out of the response's `Included` array (`teamInfoFromIncluded`,
  `rosters_builder.go`) instead of a separate `GetTeams` call - `GetTeams`'
  only use anywhere in this codebase was resolving bare team IDs to names/
  display order for these two functions. Drops the shared roster fetch
  from 3 calls to 2, on every Team Builder render and every roster
  add/remove/preference-change mutation (`rosters_detail.go`).
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
  is a relationship of the other) - the Team/position/member gap's real
  fix was caching (see the Changelog's "Roster shared-fetch cache" entry),
  this only trimmed the live fetch itself from 3 calls to 2.
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
  independent budget with PCO. Full reasoning in `docs/plans/shipped/pco-throttle-plan.md`.
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
  `scheduling-build.js`) before settling on plain `load once` plus a
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
  (`docs/plans/shipped/pco-roster-cold-start-plan.md`, shipped) - closes the *per-person*
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
  zero-conflicts. Team/position lists and team-member names themselves
  were, at the time this shipped, still the one deliberate open gap - this
  only closed the per-person half (see the Changelog's "Roster
  shared-fetch cache" entry for how the team/position half was later
  closed too).
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
- **Priority-aware throttle shipped** (`docs/plans/shipped/pco-throttle-plan.md`, go-pco
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
- **PCO call-efficiency rollout, Phase 1 (`include=` wins)** - go-pco
  `v0.21.0` added `Include []string` to `ArrangementsParams`/`ItemsParams`/
  `NeededPositionsParams` plus an `AccessTokenFromContext` accessor (the
  latter for Phase 2 below), landing three sideloading fixes from this
  file's own "Live-verified PCO API facts":
  - **Arrangement keys sideloaded instead of a second `GetKeys` call**
    (Phase 1b) - `fetchAndCacheArrangementInfo` (`scheduling.go`) now calls
    `GetArrangements(..., &pco.ArrangementsParams{PerPage: 1, Include:
    []string{"keys"}})` and reads the Key resource out of that response's
    `Included` array (`keysFromIncluded`) instead of a separate `GetKeys`
    call - halves Song Info's whole-library sync cost.
  - **Item notes sideloaded instead of fetched per song** (Phase 1c) -
    `buildSetBuilderData`'s one `GetItems` call now passes `Include:
    []string{"item_notes"}`; `renderServiceOrder` no longer fans a
    `GetItemNotes` call out per published song via `RunConcurrently` -
    `buildSongNoteViews` reads from a `map[itemID][]pco.ItemNoteData` built
    once (`itemNotesFromIncluded`, same `Included`-array parsing convention
    as `teamNamesFromIncluded`/`planSortDatesFromIncluded`). Kept
    `RunConcurrently` in `renderServiceOrder` rather than collapsing it to a
    sequential loop: at the time this landed, `resolveArrangementCacheInfo`'s
    cache-miss path was still two sequential calls per song, giving the
    fan-out genuine per-row latency to parallelize - Phase 1b (above)
    since folded that into one call. Revisited and collapsed to a plain
    sequential loop in Phase 2 below, once the cache-miss path stopped
    making any PCO call at all (queue-on-miss).
  - **Roster `GetTeams` call eliminated** (Phase 1d) -
    `fetchRosterSharedPCOData` (`rosters_scoped.go`) and
    `buildRosterBuilderData` (`rosters_builder.go`) now sideload `team` on
    the `GetNeededPositions` call already being made instead of firing a
    separate `GetTeams` request just to resolve team ids to names/display
    order. Team resources are parsed out of that response's `Included`
    array by a new `teamInfoFromIncluded` helper (`rosters_builder.go`,
    alongside the existing `teamNamesFromIncluded`/`planSortDatesFromIncluded`
    precedent), and `rosterSharedPCOData.Teams`/`TeamsErr` are gone -
    replaced by a pre-parsed `TeamInfo map[string]teamInfo` field. Drops
    the shared roster fetch from 3 calls to 2, on every Team Builder
    render and every roster add/remove/preference-change mutation.
- **PCO call-efficiency rollout, Phase 2 (Song Info queue-on-miss)** -
  `resolveArrangementCacheInfo`'s (`scheduling.go`) cache-miss path no
  longer blocks a render on a live `fetchAndCacheArrangementInfo` call: it
  enqueues a `songSyncRequest` (`enqueueSongSync`) and returns a zero-value
  immediately, mirroring `person_data_cache.go`'s
  personSyncPending/triggerPersonDataSync architecture exactly (see that
  file's own header comment). `buildSetBuilderData`/
  `buildSetBuilderDataWithErr` (`sets_builder.go`) call
  `triggerSongInfoQueueSync` once, right before returning, which drains
  whatever's pending for the org into a background goroutine
  (`runSongInfoQueueSync`) in the same paced-batch shape as the old
  whole-library sync. Trigger mechanism (the plan's own "2c" decision):
  called from *inside* the shared builder function rather than from each of
  its ~20-plus call sites, using a new `pco.AccessTokenFromContext`
  accessor (go-pco `v0.21.0`, added alongside Phase 1's `Include` fields)
  and a new `logger.RequestIDFromContext` counterpart to pull the access
  token/request id back out of the plain `context.Context`
  `buildSetBuilderData` already has, rather than threading either through
  as new parameters - a context with no token (never happens for a real
  signed-in request today) makes the trigger a no-op. `renderServiceOrder`'s
  `RunConcurrently` fan-out (kept in Phase 1c above) was collapsed to a
  plain sequential loop once this shipped, since no per-row PCO call
  remains in the common case any more. Also removed: the standalone
  `/scheduling/song-info` page and its "Sync now" button
  (`SongInfoPage`/`SyncSongInfo`), the home-page "Finish syncing"
  nudge/Setup-list item (`HomeData.SongInfoCached`/`SongInfoTotal`), and the
  matching nav-dropdown nag (`PopulateNavState`'s `songInfoNeedsSync`
  flag). Deliberately **not added**: a whole-library "resync" button on
  Account - the existing `ClearSongInfoCache` action already covers "force
  a fresh state" (wipe, then queue-on-miss refills it as songs are actually
  touched).
- **PCO call-efficiency rollout, Phase 3 (background-sync cron)** - full
  design in `docs/plans/shipped/pco-background-sync-plan.md` (now marked shipped). New
  `jobs.PCOJobs` (`internal/server/jobs/pco.go`), registered alongside
  `AuthJobs`/`BillingJobs` in `crons.go`, ticks hourly: for every
  organization with at least one live session
  (`auth.Store.SessionForBackgroundSync` - most-recently-created wins, any
  live session has equivalent PCO access), refreshes that session's token
  (`auth.RefreshAccessToken`), persists it back
  (`UpdateSessionTokensByID`), and calls the new
  `controllers.RunOrgBackgroundSync` (`pco_background_sync.go`) with a
  low-priority context built from it. A dead refresh token deletes the
  session on the spot (`DeleteSessionByID`) - an earlier, stronger signal
  than `deleteExpiredSessions`' 30-day sweep - and skips that org for the
  tick; no live session at all is unchanged from today (simply skipped).
  `RunOrgBackgroundSync` deliberately reuses every existing sync mechanic
  unchanged rather than inventing new ones: it computes what an idle org
  needs (a `FetchActiveSongs`-vs-cache diff for Song Info, a new
  `scheduling.Store.StalePersonSyncTargets` query across migration
  `00033`'s three tables for person-data refresh), enqueues it the same
  way an organic render-time miss would
  (`enqueueSongSync`/`enqueuePersonSync`), and triggers the same paced,
  guarded drain-and-run path a render already uses
  (`triggerSongInfoQueueSync` unchanged; `triggerPersonDataSync` split into
  a `*fiber.Ctx` wrapper and a new ctx/store/org-based
  `triggerPersonDataSyncCtx` core so both a request and this cron can
  drive it) - so this can never race a concurrent organic sync for the
  same org, and adds no second rate-limit budget to reason about. Also
  added: a bounded "recent-history discovery" pass (5 most recent past
  plans per service type, `GetPlans`+`GetTeamMembers`) that seeds
  person-data sync requests for anyone with **no** sync-marker row at all
  yet (`scheduling.Store.PersonSyncMarkerStatus`) - the "just-connected
  org with hundreds of volunteers" case - bounded by (recent plan count) x
  (service type count), not volunteer-pool size, so independent of org
  size unlike a naive full-eligibility-pool walk, which this discovery
  pass deliberately avoids being. No new migration: the
  discovery window is simply re-walked (rather than gated behind a new
  "already discovered up through plan X" marker table, an alternative the
  plan doc considered) - `PersonSyncMarkerStatus`'s existence check already
  skips everyone previously discovered, so a re-walk's only recurring cost
  is the `GetPlans`/`GetTeamMembers` calls themselves, not reprocessing
  already-known people (see the follow-up entry below on how often that
  re-walk actually runs). `middleware.AccessToken(c)` (the fiber-locals
  token accessor `triggerPersonDataSync` used before this refactor) was
  removed as dead code once nothing called it any more -
  `pco.AccessTokenFromContext` on the already-carried `pco_ctx` context
  does the same job for every remaining caller.
- **Phase 3's discovery walk throttled to once/24h per org, not every
  hourly tick** - the rest of the per-org pass (Song Info/person-data
  refresh) is genuinely time-sensitive staleness and already self-limits
  via the paced-batch sync it triggers, but step 6's discovery walk
  (`discoverRecentPersonTeamPairs`) was re-running its full
  `GetPlans`+`GetTeamMembers` walk on every hourly tick regardless of
  whether anything had changed - 24 near-identical PCO calls for every one
  that could actually find something new, for a dataset (plan/team
  membership) that doesn't change hour to hour. Gated behind a new
  `discoveryDue`/`backgroundSyncDiscoveryInterval` (24h) check, keyed by
  organization, in-memory only (a restart just means one extra walk,
  already a cheap no-op for anyone previously discovered) - no new table,
  same reasoning the plan doc used to decline a persistent marker for the
  window itself.
- **Live-sync reflection, option C (poll-while-open banner)** - full design
  in `docs/plans/shipped/live-sync-reflection-plan.md`. New traffic:
  one `GetItems` call per open Build-page Songs tab every 30s
  (`SyncCheck`, `sets_publish.go`), tagged `pco.WithPriority(ctx, 20)` so
  it never contends with interactive traffic sharing the same token - cost
  scales with concurrent open tabs, not total org/plan count, per option
  C's own tradeoff table in the plan doc. Live-verified first (both facts
  now in "Live-verified PCO API facts" above): `Plan.updated_at` doesn't
  reflect item-level changes at all, and a pure `pco.ReorderItems` reorder
  doesn't bump the moved items' own `updated_at` either - so there's no
  cheaper check than a real `GetItems` call, and the comparison
  fingerprint (`pcoItemsFingerprint`) has to hash each item's `Sequence`
  explicitly rather than relying on `UpdatedAt` alone. A poll tick that
  finds a difference responds with two `hx-swap-oob="true"` fragments (a
  banner offering the existing `MergeSet` endpoint, and the updated
  fingerprint) rather than auto-applying anything - per the plan's
  banner-not-silent-auto-merge decision. `MergeSet` itself was extended in
  the same change (step 3 of the plan) to reconcile already-tracked items,
  not just import net-new ones: an already-published row's
  Description/KeyName is pulled in from PCO only when it has no pending
  local edit (`InfoPending` false - a pending edit always wins), a row
  whose PCO item was deleted gets its `PCOItemID` cleared back to `""`
  rather than the local row being deleted, and a detected reorder among
  tracked items surfaces as a plain warning rather than an automatic
  reorder reconciliation. Option A (real PCO webhooks) and the harder
  order-diff/multi-viewer questions remain open per the plan doc.
- **Roster shared-fetch cache** (2026-09-06) - closes the last row that was
  still "still open" above: `GetTeamMembers`/`GetNeededPositions` were
  already deduped to one pair per handler call (`fetchRosterSharedPCOData`,
  see above), but every separate render/click still re-fetched live, with
  nothing shared *across* calls. Now cache-aside
  (`rosters_shared_cache.go`), keyed by `(serviceTypeID, planID)` - PCO ids
  are globally unique, so no organization id is needed in the key either.
  TTL is 20s, not the usual 60s (`songCacheTTL`/`serviceTypeCacheTTL`):
  team membership can change *during* an active scheduling session (an
  admin adds someone to a team directly in Planning Center right before
  someone here tries to schedule that same person), unlike a song library
  or service-type list, which are effectively static within one. Same
  fail-safe convention as every other cache here - only a fetch with
  neither call's error set is cached, so one bad PCO response can't pin a
  stale/partial result in for the rest of the window. No invalidation on
  local roster writes: draft assignments/preferences stay local-only until
  Publish, so nothing this app's own roster mutations do ever changes what
  these two calls return - the only real source of staleness is a change
  made directly in Planning Center, which expiry already handles. For the
  remaining window, a new manual "Refresh team data" button
  (`RefreshRosterTeamData`, top of the roster page) bypasses the cache and
  rebuilds the whole roster from a fresh live fetch - the same
  explicit-click-remedy shape as `RefreshPersonData`/
  `RefreshSetSongArrangementInfo` above, just scoped to the whole roster
  (not one row) since a team/position change can affect every group on the
  page. The cache stayed in `internal/server/controllers`
  (`rosters_shared_cache.go`) rather than moving into `internal/scheduling`
  alongside `FetchActiveSongs`/`FetchServiceTypes`: the cached value's
  `TeamInfo` field is built from `teamInfoFromIncluded`, a
  controllers-package-local type, and exporting it purely to satisfy a
  package boundary bought nothing.
- **Song attachments cached alongside arrangement info, not fetched live** -
  full design in `docs/plans/shipped/song-lead-sheet-attachments-plan.md`.
  New per-song call: `fetchAndCacheSongAttachments` (`scheduling.go`) calls
  the new `pco.GetAttachments` (go-pco, list-only wrap of Song Attachments)
  right after `fetchAndCacheArrangementInfo`'s own arrangement fetch
  succeeds, in every one of that function's existing call sites (the
  Song Info queue-drain sync, the manual "Update from Planning Center"
  refresh, and the publish-time cache-miss backfills in
  `sets_publish.go`) - same "cached at sync time, never live in a render
  path" posture as arrangement info itself, not a new hot-path risk.
  Deliberately best-effort and never reflected in
  `fetchAndCacheArrangementInfo`'s own returned error (logged and
  swallowed instead) - several of that function's callers use its error to
  decide whether the arrangement info it just fetched is safe to use, and
  an unrelated attachments hiccup must never make those callers discard an
  otherwise-good, already-persisted arrangement fetch. Cost: Song Info
  sync goes from 1 PCO call/uncached song to 2 (see
  `songInfoSyncBatchPause`'s updated doc comment for the corrected pacing
  math - that comment had drifted stale at "2 calls" ever since Phase 1b
  folded the old separate `GetKeys` call into `GetArrangements`' own
  `include=keys` sideload, dropping the *real* per-song cost to 1 call
  without anyone updating this comment; this change is what makes "2
  calls" true again, for a different reason than the stale comment
  claimed). The attachment's own `url` is deliberately never cached (see
  the "confirmed live" fact below - not because it expires, but so a
  click always reflects whatever's actually in Planning Center right now)
  - a new click-triggered route (`OpenSongAttachment`,
  `song_attachment_cache.go`) live-fetches a fresh one from Planning
  Center on the actual click and redirects, same "gated behind an
  explicit click, not a render-path cost" posture as
  `RefreshSetSongArrangementInfo`'s own button.
- **Song attachment field shape live-verified, `url`'s real meaning
  corrected** (2026-09-06, same-day follow-up to the entry above) -
  created and deleted a real throwaway attachment against a dev org song
  to confirm `go-pco`'s guessed `AttachmentAttributes` field names
  (`filename`/`content_type`/`file_size`/`url`/`created_at`/`updated_at`)
  are all correct. They are - but `url` turned out to be Planning
  Center's own attachment web page
  (`https://services.planningcenteronline.com/attachments/:id`), not a
  file link, and not the "might be an expiring pre-signed URL" risk the
  original build defended against. The real file requires `POST
  .../songs/:song_id/attachments/:id/open` (undocumented via GET,
  confirmed live), returning an `AttachmentActivity.attachment_url` -
  not wrapped in go-pco, since `OpenSongAttachment` only ever redirects
  to PCO's page today. See the "Live-verified PCO API facts" entry below
  for the full finding; no behavior change, just corrected reasoning in
  code comments and this doc.
- **Song leader synced into the existing vocal notes category, not a new
  hot-path call** - full design in `docs/plans/shipped/song-leader-plan.md`.
  `syncLeaderNote` (`sets_publish.go`) composes a reserved "Leading:
  [name]" first line into whichever Item Note category the org designates
  as "the vocal one" (a new per-service-type setting,
  `scheduling.Store.VocalNoteCategoryID`/`SetVocalNoteCategory`, migration
  `00038`), using the same `pco.CreateItemNote`/`UpdateItemNote`/
  `DeleteItemNote` calls `UpdateSongNote` already makes - no new call
  shape. `PublishSet` already fetches the plan's items for its reorder
  step; this just adds `Include: []string{"item_notes"}` to that existing
  call (and only when a vocal category is actually configured), reusing
  `itemNotesFromIncluded` (Phase 1c, above) instead of a fresh
  `GetItemNotes` per song. `RefreshSongInfo` had no `GetItems` call of its
  own before this - it now makes one, per Refresh click, per plan, but
  only for a service type that's opted into the setting; an org that
  never configures it pays nothing extra. Person-name resolution for
  display (`SetSongView.LeaderPersonName`) reuses the existing
  `personNameMapForIDs` batched `where[id]=...` lookup, the same pattern
  rosters/preferences already use - not a new category of call.
- **Song Vocal Range page's current-key line, removed (2026-09-08)** -
  `song_vocal_range.go`'s `currentKeysBySong` (added no new PCO call, per
  this same changelog just above previously) has been deleted along with
  the "current key" display line it fed: a song can have more than one
  PCO arrangement in different keys, so "the" current key was never a
  well-defined single answer, and the preview-key control added the same
  day (client-side, no PCO call at all) already covers "show this range in
  some other key" for whichever key the reader actually wants.
- **Home page "needs attention" widget's bounded look-ahead row** - the
  widget used to show only the single chronologically-next plan per
  service type (`homeServiceTypesCap`), even when that plan was already
  fully planned and the real next-*unplanned* plan was further out.
  `buildHomeData` (`home.go`) now optionally shows a second row per
  service type in that case, capped at `homeLookAheadPlansCap` (3)
  candidate plans - see that const's doc comment for the full reasoning.
  Costs zero extra `GetPlans` calls (the existing per-service-type call's
  `PerPage` is bumped from 1 to `1+homeLookAheadPlansCap`, still one
  request); the only added cost is `planStatus`/`rosterStatus` checks on
  those extra candidates, and only when the primary plan is already fully
  planned, checked in plan order and stopped at the first unplanned one -
  worst case 3 extra candidate checks per service type, still fanned out
  concurrently across service types via the same `RunConcurrently` call
  `buildHomeData` already used.
- **Vocal-range mismatch warning (Phase 3, 2026-09-08) adds one new
  render-path call, bounded and batched, not a new risk category** -
  `buildSetBuilderData`'s new roster-vocalist cross-reference
  (`vocalRangeWarningForSong`, `sets_builder.go`) fetches the plan's
  roster assignments (`scheduling.Store.FindRoster`/
  `ListRosterAssignments`) and every `PersonVocalRange` on file, both
  local-DB reads with no PCO call at all. The one real addition is a
  `personNameMapForIDs` call to resolve display names for the mismatch
  message - the exact same batched `where[id]=...` pattern `leaderNames`
  already uses on this same render (see the Song leader entry above) -
  and its input is intersected down to just the roster members who are
  *both* assigned to this plan and have a range on file
  (`vocalistsWithRangesOnFile`), not the whole roster: 0 calls when
  nobody assigned has a range recorded (the common case today, since
  Phase 1 person ranges are still new), and never more than one batched
  call bounded by a single plan's own roster size otherwise.
- **"Add item to plan" (2026-09-09) - not a hot path, no caching added,
  and one call cheaper than the first draft.** `AddPlanItem`
  (`plan_items.go`) is a rare, user-triggered click (add a sermon slot/
  header/video from the Build page instead of switching to Planning
  Center), not something that runs on every render - the "does this need
  a cache?" question this file exists to raise doesn't apply the way it
  does to a render-path call. The first draft did add a real second call
  per add though: a `GetItems` fetch just to compute the next `Sequence`
  before `CreateItem`. Live-checking that assumption (see the
  `Item#create` auto-append fact above) found PCO does that itself -
  dropped the pre-fetch, so this is now one `CreateItem` call, plus
  whatever the existing full-page rebuild
  (`buildSetBuilderDataWithErr`) already costs after any Build-page
  mutation - not a new cost category. Same live check also caught two
  smaller footguns before they shipped: a header item's `length` is
  silently dropped by PCO regardless of what's sent (the length fields
  now hide/disable themselves for that item type), and `item_type:
  "media"` doesn't actually stick (dropped from the type picker
  entirely, not offered as a real option).
- **Move/edit/delete a plan item (2026-09-09) - same "rare click, not a
  render path" shape as Add, three more thin call sites.**
  `RepositionPlanItem`/`UpdatePlanItem`/`RemovePlanItem` (`plan_items.go`)
  round out drag-and-drop reordering, editing, and removing a header/plain
  item once `AddPlanItem` could create one - none of the three are called
  on render, so none needed caching. Reposition is the one with a real
  fetch: it needs the plan's current, real item order to compute a correct
  move, and that fetch can never be cached (a stale list would silently
  reorder against the wrong baseline) - confirmed live that `item_reorder`
  wants the complete ordered id list, not a partial diff, same contract
  `PublishSet`'s own reorder already relied on, so `buildReorderedItemIDs`
  just moves one id within that fetched list rather than trying to send a
  smaller request. A not-yet-published set song complicates "what's the
  right anchor" for a drag (it isn't part of Planning Center's own order
  at all yet) - `anchorRealItemID` walks backward through the same merged
  view the page already renders until it finds a row that actually is a
  real item, rather than assuming every row in the drop zone is one.
  Editing hit two more live-checked footguns before shipping: PATCHing a
  header's `length` is silently dropped, same as create (edit form omits
  those fields for a header too), and PCO's `UpdateItemParams` has no
  `ItemType` field at all - an item's type is create-time-only, so the
  edit form has no type control and never tries to change it.
- **Build page "Past plans" tab, cached and windowed (2026-09-09)** - the
  reported symptom was the Past tab being slow enough that a real user gave
  up waiting. Root cause: `fetchPastPlans` (`build.go`) had no date bound
  and no caching at all, so opening the tab, every search-box submit, and
  every Prev/Next click (plain submit buttons on the same search form) each
  re-fanned `fetchPlansByFilter` out across a service type's *entire*
  historical plan list from PCO - every page, on every single interaction.
  `fetchPlansByFilter`'s own concurrent fan-out was already correct and
  left untouched; the fix is entirely about not calling it so often or so
  broadly. New `build_past_plans_cache.go`: `fetchPastPlansCached` caches
  the result per (organization, service type) - past plans are
  per-service-type data, unlike the roster shared cache above which needs
  no org id since it's keyed on already-globally-unique PCO ids - for
  `buildPastPlansCacheTTL` (30 minutes, much longer than the 60s/20s norm,
  since a plan's title/dates are fixed once it's actually in the past,
  unlike a song library or team membership). The default (cold-cache) fetch
  is itself windowed via `fetchPastPlansWindowed`: pages sequentially
  (deliberately not `RunConcurrently`'d - the fan-out would need to know
  the total page count before fetching any of them, which defeats "stop
  early") through the `-sort_date`-ordered "past" filter, stopping as soon
  as a page's oldest plan crosses `buildPastPlansWindowMonths` (18 months)
  back, rather than walking a multi-year history to the sanity ceiling.
  `combinedPlansForServiceType`'s past-tab branch now calls
  `fetchPastPlansForSearch`, which checks the cached/windowed result first
  and only pays for one further uncached, unbounded `fetchPastPlans` call
  (unchanged - the pre-existing full fetch) when a search term doesn't
  match anything *and* the windowed result was truncated (meaning older,
  unfetched plans might still contain a match) - so bounding the default
  load never makes an older plan permanently unreachable. No PCO-side date
  filter was added (`PlansParams` has no such param today, and adding one
  would need its own live-verification pass per this doc's own standing
  rule - out of scope here); the window is enforced purely by this app
  stopping its own paging early against the ordering PCO already provides.
- **Song leader now also syncs to PCO's real "Song leader" field, not just
  a vocal-notes text line (2026-09-09).** Live feedback (`docs/issues.md`)
  reported the local leader field "doesn't write to anything in PCO" and
  asked to tether it to PCO's own "Song leader" UI. Checking that claim
  live (rather than guessing at the design, per `docs/plans/shipped/
  worship-leader-pco-sync-plan.md`) found the field is a whole separate
  resource, `ItemAssignment` - not an Item/Song/Arrangement attribute, so
  the original plan doc's leading theory (a per-plan Team assignment) was
  wrong; it's per-item, matching this app's existing per-occurrence
  `SetSong.LeaderPersonID` model exactly. Added `ItemAssignment` to go-pco
  (`v0.24.0`, `itemAssignments.go`: `GetItemAssignments`/
  `CreateItemAssignment`/`DeleteItemAssignment`, no update per PCO's own
  vertex docs), then wired `syncLeaderAssignment` (`leader_note.go`) into
  `PublishSet`/`RefreshSongInfo` alongside (not replacing) the existing
  `syncLeaderNote` vocal-notes sync - both are cheap, idempotent writes of
  the same local field, so there's no drift risk in keeping both. Sideloads
  via the same `Include` mechanism as `item_notes` (confirmed live that
  `item_assignments` is a valid `Include` on the `Item` resource), so this
  added zero new PCO calls to the already-happening `GetItems` fetch in
  `PublishSet`, and only a conditionally-new one in `RefreshSongInfo` (same
  "opt-in cost" shape the vocal-notes sync already has there).
- **Song leader's vocal-notes-line sync removed outright (2026-09-09, same
  day as the entry above).** "No drift risk in keeping both" turned out to
  be true but beside the point - once the real `ItemAssignment` sync
  existed, the vocal-notes-line sync (`syncLeaderNote`/
  `composeLeaderNoteContent`/`noteInCategory`) and the "Song leader sync
  setting" it depended on (`SetVocalNoteCategory`/`VocalNoteCategoryID`,
  `service_type_vocal_note_categories` - migration `00038`, dropped by
  `00044`) had no remaining benefit, just setup friction (a one-time
  per-service-type dropdown pick) for a strictly worse mechanism (an
  opaque note string PCO can't notify on, vs. a real assignment it can).
  `PublishSet`/`RefreshSongInfo` no longer conditionally request
  `Include: item_notes` on their `GetItems` call at all - only
  `item_assignments`, gated purely on whether any song in the set has a
  leader set, same as before. See
  `docs/plans/shipped/song-leader-plan.md`'s updated Status note.
- **Song leader picker scoped to the plan's own roster (2026-09-09)** -
  reported directly ("PCO only allows leaders that are already scheduled
  in the plan. We don't have that limit."), and confirmed live per the
  new "Live-verified PCO API facts" entry above:
  `CreateItemAssignment` 422s with `Invalid assignable id` for anyone who
  isn't already a scheduled team member on the plan. `buildSetBuilderData`
  (`sets_builder.go`) now also resolves `SetBuilderData.RosterPersonOptions`
  from the exact same local roster-assignments read it already made for
  the vocal-range mismatch warning (`vocalistPersonIDs` -
  `scheduling.Store.FindRoster`/`ListRosterAssignments`, a local-DB read,
  no PCO call) - the one new PCO cost is resolving *all* of those person
  ids to display names (`personNameMapForIDs`, one batched
  `where[id]=...` call), where before only the subset with a vocal range
  on file got resolved. Bounded by the plan's own roster size (a handful
  of names, not the whole org), and zero calls when nobody's on the
  roster yet - same "opt-in, bounded by what's actually on screen" shape
  as every other batched name lookup on this page. The song detail
  card's leader field (`leaderForm`, `song_detail_card.templ`) changed
  from a `PersonPickerField` (searches the whole org, like every other
  person picker on this app) to a plain `<select>` over that list -
  correct by construction rather than correct by luck, and a smaller
  amount of UI than adding a second, roster-scoped search endpoint would
  have been, for a candidate pool that's realistically a handful of names
  per plan anyway.
- **Fully-staffed teams showed their raw PCO id instead of a name, on the
  Team Builder page (2026-09-10).** Reported live, against a real org's
  account: team `62735` ("Vocal Team") rendered as `62735` on both the
  "Teams, at a glance" card and the Team tab. Root cause was the
  `include=team` optimization above (2026-09-04 changelog entry): it only
  sideloads a team when that team has at least one currently-unfilled
  position, since `GetNeededPositions`' own primary listing is filtered to
  unfilled slots only - a fully-staffed team never appears in `Needed.Data`
  at all, so its `Team` resource never appears in `Included` either, even
  though `groupNeededPositions` still correctly keeps that team's position
  group (built from local roster assignments, not from the missing
  `NeededPosition` entry). No backfill existed for this the way
  `resolveMissingTitles` already covers stale/archived song ids. Fixed
  with `resolveMissingTeamNames` (`rosters_builder.go`) - a full
  `GetTeams(..., &pco.TeamsParams{PerPage: 100})` list call (Teams has no
  `where[id]` filter the way People does, so a full list is the only way
  to resolve a specific missing id), fetched only when `positionGroupTeamIDs`
  finds a team id actually missing from the map already built from
  `TeamInfo` - the common, fully-resolved case still costs nothing. Wired
  into all three places that independently built a `teamNames`/`teamName`
  value from `shared.TeamInfo` (`buildRosterBuilderData`,
  `buildRosterLightData`, `buildRosterPositionGroupData`).
- **Song Tag Suggestions now also reads PCO's own `Themes` field
  (2026-09-11)** - Phase 1 of
  `docs/plans/in-progress/song-theme-and-lyric-tag-suggestions-plan.md`.
  `pco.SongData.Attributes.Themes` is a plain string already returned by
  the exact same `services/v2/songs` call `scheduling.FetchActiveSongs`
  already makes and already caches for 60s - this app just never read that
  field before. **Zero new PCO calls, zero new caching, no migration at
  all** - `scheduling.SongThemeSuggestions` (new, alongside the existing
  title-word `SongTagSuggestions`, both now delegating to a shared
  `songTagSuggestionGroups` helper) and `middleware.PopulateNavState`'s
  extended pending-suggestions check both just do a second pure
  computation over the same `songs`/`tags`/`ignored` data already in
  scope. Phase 2 of that plan (lyric-derived suggestions, extending
  `song_arrangement_cache`) is not yet built.
- **Poll-while-open live-sync check now skips backgrounded tabs
  (2026-09-12)** - reported directly (`docs/issues.md`: "what happens if
  the user has 4-5 tabs open... only poll on the active tab"). The
  "Live-sync reflection, option C" entry above already documented that
  cost as scaling with concurrent open tabs and *tolerated* it (bounded,
  low-priority-tagged, cheap fingerprint check) rather than mitigated it -
  this closes the specific case of one person's own idle background tabs
  adding to that scaling, not the separate N-different-people-with-the-
  same-plan-open case, which still costs N polls by design (no shared/
  dedup state exists, per that entry's own reasoning). `#pco-sync-poll`'s
  `hx-trigger` (`set_builder.templ`) gained a `[!document.hidden]` filter -
  a plain htmx trigger-filter expression, re-evaluated every tick, no new
  JS - so a tick simply doesn't fire while its own Build page tab isn't
  the visible one; switching back resumes on the next 30s tick, not
  instantly. Only the Songs tab polls at all (the Team tab has no
  equivalent), so this only ever bounds that one poll.
- **Page-level "rate limit reached" banner shipped (2026-09-14)** - Phase 1
  of `docs/plans/in-progress/pco-rate-limit-user-visibility-plan.md`,
  closing the first half of the direct feedback quoted at that doc's top.
  A 429 no longer only reaches this app's own logs (the "Visibility into
  PCO failures" row above) - `pco_observability.go`'s response hook now
  also calls `middleware.RecordPCORateLimit(requestID)`
  (`internal/server/middleware/pco_ratelimit.go`, new: a request-scoped,
  read-once, mutex-guarded map, swept of entries older than 5 minutes on
  each write so a handler that never renders - e.g. a redirect - can't
  leak an entry). `controllers/render.go` consumes it
  (`middleware.HadPCORateLimit`) right before every render and stashes the
  result as `views.PCORateLimitedKey`; `layout.templ`'s `Layout` reads it
  and shows a calm, non-dismissible "Planning Center's rate limit was
  reached - some details may be stale and will refresh shortly" notice
  (`pcoRateLimitBanner`, `pco-rate-limit-banner.css`) when true. Zero new
  PCO calls - purely wiring an existing failure signal through to the
  render that experienced it. Deliberately full-page-load only: an htmx
  fragment reload doesn't re-render `Layout`, so a 429 during a
  fragment-only request won't update an already-visible banner - see the
  plan doc's own "Scope limit" note. Per-row marking (Phase 2) is not
  built.
