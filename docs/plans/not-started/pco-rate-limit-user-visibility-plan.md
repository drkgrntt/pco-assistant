# Plan: user-visible PCO rate-limit signaling (banner, then per-row retry)

Not built yet - written up for review before anyone picks it up. Prompted by
direct feedback in `docs/issues.md`:

> As I understand it, we swallow 429s from the API. Perhaps there's a more
> delicate warning we can show in the ui like "Planning Center rate limit
> reached; some details may refresh shortly". Perhaps even saying what we
> were unable to complete?
>
> Would it be possible to mark elements in the UI with what was unable to
> be fetched due to 429 so it can show a spinner and try again in a few
> seconds automatically via perhaps a smaller api call?

## Current state

`docs/pco-api-risks.md` already states this app's own thesis on the
problem (its "silent 429" framing): most PCO-backed fetches use
best-effort `if err == nil`-style handling, so a 429 mid-render doesn't
crash the page - it silently blanks whatever hadn't fetched yet, which
reads as "nothing to report" rather than "we couldn't check." Two
proactive mitigations already exist and are enabled in production
(`PCO_THROTTLE_ENABLED`'s priority-aware token-bucket throttle,
`PCO_RETRY_ON_429_ENABLED`'s single retry honoring `Retry-After`), but
the doc's own changelog records a real 429 getting through both anyway
(2026-09-03) - so this is a real, occasionally-observed failure mode, not
a hypothetical.

What exists today, concretely:

- **Logging only, not render-path-visible**: `internal/server/
  pco_observability.go`'s `registerPCOResponseHook` special-cases
  `http.StatusTooManyRequests` into a grep-able `logger.WarnContext` line.
  It never touches the response or sets anything a handler/view can read -
  purely for after-the-fact log correlation via `request_id`.
- **`friendlyPCOErr`** (`internal/server/controllers/pco_errors.go`) is the
  only place in application code that inspects `pco.RequestError.StatusCode`
  at all, and only special-cases 401/403 into a friendly message; 429 falls
  through as the raw `err.Error()`. Used only on write paths (`sets_publish.go`,
  `rosters_publish.go`) - never on the read/render paths where this issue's
  complaint actually lives.
- **The pervasive read-path pattern**: error collapsed to a zero value with
  no status-code inspection at all, e.g.:
  - `resolveArrangementCacheInfo` (`internal/server/controllers/scheduling.go`,
    ~L362-369) - cache miss just enqueues a background sync and returns
    `scheduling.SongArrangementInfo{}`; there's no error return in its
    signature for a caller to even inspect.
  - `planStatus`/`rosterStatus` (`sets_publish.go`, `rosters_publish.go`) -
    `items, err := pco.GetItems(...); if err != nil { return "" }` - a 429
    here (Home page, Build-page plan picker) looks identical to a
    legitimately-empty plan.
  - `otherAssignmentDates`/`crossTeamAssignmentsThisWeek`/`personBlockouts`
    (`rosters_warnings.go`) - any cache miss or error enqueues a background
    sync and returns `nil`, no distinction between "never synced" and "sync
    attempt got 429'd."
  - `home.go`'s `buildHomeData` fan-out and most `scheduling.RunConcurrently`
    call sites (`build.go`, `rosters_builder.go`, `rosters_scoped.go`,
    `people.go`, `pco_songs.go`) - `RunConcurrently`'s own contract
    (`internal/scheduling/concurrency.go`, ~L23-29) is explicit that the
    passed function "is responsible for its own error handling" - there is
    no error-aggregation mechanism in the helper itself, by design.
- **The one existing "hasn't synced yet" precedent**: `PersonDataSyncPending`
  (`internal/views/scheduling/roster_types.go`, ~L80-90, set in
  `rosters_builder.go` ~L548-550) is a single whole-render boolean ("has
  *anyone* relevant to this render ever synced"), not per-person/per-row -
  it can't say *which* rows are affected, only that *something* is pending
  somewhere on the page.

## Two distinct asks here, two very different sizes

1. **A page-level "rate limit reached, some data may be stale" banner.**
   Needs only "did a 429 happen recently during this request," not
   per-item granularity - a much smaller lift.
2. **Per-element marking + auto-retry via a small follow-up call.** Needs
   to know *which specific row* failed and why, which means threading a
   distinguishable error signal through every best-effort fetch above and
   through `RunConcurrently`'s call sites - the "real schema/feature work"
   tier, not a UI-only change.

## Design forks needing a real answer (not a default)

1. **How does a handler learn "a 429 happened during this render," for ask
   1?** Two shapes, not obviously equivalent:
   - Extend the response hook to increment a short-lived counter a handler
     can check once its own PCO calls are done - but the hook is a single
     process-wide callback (per CLAUDE.md's own note on it) with no
     natural per-request scope of its own beyond the `request_id` it
     already logs; making it write somewhere a concurrent handler can read
     back means new shared, request-scoped state, not a trivial add.
   - Have each best-effort call site distinguish 429 explicitly
     (`errors.As(err, &pco.RequestError{})`, `StatusCode == 429` - the
     exact pattern `friendlyPCOErr` already uses, just not on read paths
     today) and fold a `RateLimited bool` into the existing per-page
     view-model, aggregated the same way `PersonDataSyncPending` already
     is. More explicit and local, but means touching every one of the
     read-path call sites listed above, not just one central hook.
2. **Banner scope** - per-page (matches what was actually affected, least
   alarming) vs. a single app-wide sticky banner. Per-page is the more
   conservative default worth confirming before building either way.
3. **What "a smaller api call to retry" means for ask 2.** This app
   already has a retry mechanism in a different shape:
   `enqueueSongSync`/`enqueuePersonSync` (background jobs) plus the
   `PersonDataSyncPending`/cache-miss pattern already re-fetch
   automatically. The likely real gap isn't "there's no way to retry" -
   it's "nothing in the UI shows *this specific row* is pending and will
   update shortly," the same affordance the Team tab's "Check availability"
   click-to-reveal already provides for candidate warnings
   (`rosters_candidate_warnings.go`). Worth checking whether piggybacking
   on that existing sync-pending/poll pattern is enough before inventing a
   new fetch-and-retry primitive from scratch.
4. **`RunConcurrently`'s signature would need to change for ask 2** - from
   `func(T)` to something that reports per-item success/failure (a result
   struct, or an aggregating error collector) - a mechanical but broad
   refactor touching all ~15 current call sites, not a one-file change.

## Suggested sequencing

- **Phase 1 (small): the page-level banner.** Reuses `friendlyPCOErr`'s
  existing 429-detection pattern, doesn't touch `RunConcurrently`'s
  signature, and answers the literal first half of the issue on its own.
- **Phase 2 (large, only if Phase 1 turns out not to be enough in
  practice): per-row "stale, refreshing shortly" marking.** Piggyback on
  the existing sync-pending/background-job system rather than a new
  fetch-and-retry mechanism, but does need the per-call-site error-typing
  plumbing above to know *which* rows to mark - build this second, and
  only once Phase 1 is live and it's clear a plain banner isn't cutting
  it, the same "ship the cheap thing first" sequencing this project
  already used for `docs/plans/shipped/vocal-range-plan.md`'s phases and
  `docs/plans/not-started/dismissable-warnings-plan.md`'s modal-vs-
  disclosure question.

## Open questions

- Should the banner fire only on a confirmed 429 (`StatusCode == 429`),
  or any transient PCO failure? Recommend scoping strictly to 429 per the
  issue's own wording, so it doesn't cry wolf on unrelated failures.
- Whether Phase 2 is worth building at all before Phase 1 ships and real
  usage shows the plain banner isn't sufficient - not a decision to make
  up front.

## Verification (once built)

- `go build ./...` (empty output = success), `make test`.
- Live: deliberately trigger a 429 in dev (a burst of PCO calls past the
  throttle, or temporarily lowering `PCO_THROTTLE_ENABLED`'s budget) and
  confirm the banner appears only then, not on an ordinary page load with
  genuinely empty data.
