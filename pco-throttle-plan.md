# Plan: a priority-aware, opt-in request throttle for go-pco

Written 2026-09-02, prompted by a conversation during the roster
cold-start rate-limit fix (see `plan.md`, now shipped as two branches) -
that work surfaced a coordination gap this doc exists to close: Song
Info's background sync, the new person-data cache's background sync, and
ordinary interactive traffic each self-throttle *independently*, tuned by
hand against a shared assumption ("60% of budget should leave enough
headroom"), with no awareness of each other or of how busy PCO's
per-token budget actually is at any given moment. This is a design for
the real fix - not urgent (no consumer has hit an actual 429 yet), so
this is written up for later rather than built now.

## Where this lives: go-pco, not this repo

`pco-api-risks.md`'s mitigation #4 already established why: every
`pco.Get*`/`Create*`/`Update*`/`Delete*` call goes through go-pco's
`NewRequest`, a stateless free function that builds and discards its own
`http.Client` per call. This app has no seam to intercept calls globally
without wrapping every call site by hand - fragile, and silently bypassed
the moment a new call site is added and someone forgets to wrap it.
go-pco is the one thing that actually owns the request path. This is the
same reasoning that put the 30s timeout and `RetryAfter` surfacing there
instead of here - this is a bigger version of the same kind of change.

Consequence: this doc is a spec to hand to a go-pco-focused session, not
something buildable from this checkout. Once shipped and tagged, this
repo's own follow-up is small (see "Adoption in this repo" below).

## Design goals

- **Off by default, zero behavior change for any consumer who doesn't
  opt in.** go-pco stays a plain "make HTTP requests to PCO" library
  unless explicitly configured otherwise - other consumers of this SDK
  (if any exist or ever do) must never be silently throttled.
- **Opt in two ways**: an environment variable (for a pure ops/deployment
  toggle, no code change) or an explicit config call (for a consumer that
  wants to set values in code, which takes precedence if made).
- **Priority is numeric, with a default, and is never required.**
  Opting in with zero per-call tagging still gets you a safe global rate
  limiter (every call at the same default priority, admitted
  first-come-first-served) - priority is a pure enhancement on top, not a
  precondition for throttling to work.
- **Scoped per credential, not one global bucket.** PCO's 100-req/20s
  limit is per authenticated user token, not per-process - see the
  correctness note below, this is the one place this plan deviates from
  a naive reading of "just add a global queue."
- **Never block forever**: a caller whose context gets cancelled while
  queued must return promptly with that context's error, not hang.

## Non-goals

- Multi-instance/distributed coordination. This app runs single-instance
  today (confirmed via `docker-compose.yml` - one `web` service, no
  replicas) - a per-process, in-memory limiter is the right scope. If
  go-pco ever needs to coordinate across multiple processes sharing the
  same token's budget, that's a real redesign (shared external state,
  e.g. Redis), not an extension of this - don't build for it
  speculatively.
- Fairness/anti-starvation between priority levels (see "Accepted
  tradeoff" below).
- Metrics/observability dashboards - a debug log line is enough for now.
- Migrating this app's existing hand-rolled pacing (`songInfoSyncBatch*`,
  `personDataSyncBatch*`) - that's a real follow-up once this ships, but
  a separate, later change, not bundled into go-pco's own work.

## Correctness note: this must be keyed per access token, not global

PCO's rate limit is **per authenticated user token**. A single
process-wide queue/limiter would over-throttle unrelated organizations
sharing this one process - a busy org's background sync would slow down
every other org's unrelated interactive traffic too, even though each
token has its own independent 100/20s budget with PCO. That's wrong and
unnecessary.

The limiter must be keyed by whatever identifies "whose budget this is" -
in practice, the access token already carried on `ctx` via
`pco.WithAccessToken` (go-pco already threads this through every call, so
`NewRequest` already has it in hand). Concretely: an internal
`map[string]*tokenLimiter` (keyed on the token string, or a hash of it if
holding the raw token in a long-lived map key feels wrong), each entry
independently enforcing the configured rate/queue for that one token.
Idle entries should be evicted after some window (e.g. 30 minutes with no
activity) so a long-running process doesn't accumulate an ever-growing
map across many distinct users over its lifetime - a simple
last-used-timestamp sweep is enough, no need for a full LRU library.

## Public API surface (proposed)

```go
// Opt in and configure. Call before making requests; if never called,
// NewRequest lazily checks environment variables on first use (see
// below) via sync.Once, so a pure ops/deployment toggle needs no code
// change at all. An explicit call always wins over env vars.
func ConfigureThrottle(cfg ThrottleConfig)

type ThrottleConfig struct {
    Enabled bool

    // RatePerWindow/Window bound sustained throughput per token - default
    // should land comfortably under PCO's 100-req/20s (proposed default:
    // 80 requests per 20s, documented as "80% of PCO's real ceiling,
    // leaving headroom for imprecision in the token-bucket refill timing
    // itself" - not a number to treat as sacred, just a sane starting
    // point to revisit once this is actually in use).
    RatePerWindow int
    Window        time.Duration

    // DefaultPriority applies to any call whose context never set one via
    // WithPriority. Proposed default: 10.
    DefaultPriority int

    // IdleEvictAfter controls when a per-token limiter with no recent
    // activity is dropped from the internal map. Proposed default: 30m.
    IdleEvictAfter time.Duration
}

// WithPriority tags a context so every PCO call made with it (and
// anything derived from it) is admitted before calls with a numerically
// larger priority - priority 1 is admitted before priority 20. Same
// direction and same default (10) as WordPress's add_action/add_filter
// priority argument, deliberately: a widely-known precedent beats an
// arbitrary house convention, and it means "priority 10, same as
// WordPress's default" is instantly legible to anyone who already knows
// that system. An interactive, user-waiting call might use priority 1; an
// unattended background sync might use priority 20.
//
// Docs/comments throughout this package should say "admitted first" or
// "runs before," never "higher priority" or "lower priority" - those
// phrases are genuinely ambiguous in English (does "higher priority" mean
// numerically higher, or more urgent?) and WordPress's own docs avoid them
// for the same reason. State the number-to-order mapping once, concretely,
// with an example, and never redescribe it in words that could be read
// either direction.
func WithPriority(ctx context.Context, priority int) context.Context
```

Environment variables (checked once, lazily, only if `ConfigureThrottle`
was never called):

- `PCO_THROTTLE_ENABLED` (`"true"`/`"1"` to enable, anything else/unset =
  disabled - matches this repo's existing boolean-env-var conventions)
- `PCO_THROTTLE_RATE_PER_20S` (int, defaults to 80 if unset but enabled)
- `PCO_THROTTLE_DEFAULT_PRIORITY` (int, defaults to 10 if unset but
  enabled)

Priority is deliberately **not** settable via env var - it's a per-call
concern, not a process-wide one; only enable/rate/default belong there.

## Scheduling semantics

When disabled (the default): `NewRequest` behaves exactly as it does
today - no queue, no wait, straight to `http.Client.Do`. This must be a
genuinely free-standing code path (an `if !enabled { ...today's code...
}` at the very top), not "the same path with a zero-length queue" - the
goal is literally zero behavior change, not "behaviorally equivalent
under most conditions."

When enabled: `NewRequest` resolves the caller's token (from ctx),
fetches or creates that token's limiter, and asks it to admit this call
at its priority (from `WithPriority(ctx, ...)` or the configured
default) before proceeding to the real HTTP call. Internally, a
reasonable shape:

- A min-heap (or similar priority queue) per token, ordered by
  `(priority, sequence_number)` - sequence number is a monotonically
  increasing counter assigned at enqueue time, used only to break ties
  between equal-priority calls in FIFO order (never let equal-priority
  calls reorder relative to each other - avoids a subtly-nondeterministic
  scheduler that's hard to reason about later).
- A token-bucket (or sliding-window counter) gating admission, refilling
  toward `RatePerWindow` over `Window`, checked whenever the head of the
  heap could be admitted.
- The waiting caller blocks on a channel/condition variable that the
  admission loop signals when it's this call's turn - respecting
  `ctx.Done()` the whole time it's waiting, so a cancelled request
  returns immediately with the context's error instead of blocking until
  its turn arrives (this matters more here than in today's code, since
  waiting is now a real, sometimes-long state a call can be in).

## Accepted tradeoff: no anti-starvation

Strict priority-order admission, no aging/promotion of long-waiting
low-priority calls. For this SDK's actual use case (an unattended
background sync competing with interactive, user-waiting traffic on the
*same* token), starvation of the background work is the *intended*
outcome, not a bug - "it'll just take longer" is explicitly the tradeoff
being designed for here, not an oversight. Document this explicitly in
go-pco's own docs so nobody "fixes" it later without realizing it was
deliberate. Revisit only if a real background job is observed never
completing under sustained interactive load in practice - not
speculatively.

## Testing plan

- Pure scheduler-logic unit tests, no real HTTP: priority ordering,
  same-priority FIFO tie-break, rate-limiting math (reuse the
  token-bucket/window arithmetic pattern this repo's own
  `songSyncBatches`/`runPacedBatches` tests already use as a style
  reference - pure functions over synthetic inputs, no goroutines needed
  to test the *math*).
- A concurrency-level test proving: N concurrent callers at mixed
  priorities are admitted in the right order, a cancelled context's call
  returns promptly rather than hanging, and two different tokens' queues
  are provably independent (a busy token-A queue never delays a token-B
  call).
- An integration-style test via go-pco's existing `SetBaseURLForTesting`
  seam, proving the disabled path is truly a no-op (identical timing to
  today) and the enabled path actually enforces the configured rate
  against a fake server.

## Documentation requirements (explicitly called out because it matters)

This needs real, prominent documentation in go-pco itself, not just code
comments - it changes ambient behavior for every call a consumer makes
once enabled, and a consumer who doesn't understand the priority
convention or the no-starvation tradeoff could easily misuse it. At
minimum, go-pco's README needs a section covering: how to opt in (both
mechanisms), what the default priority and rate mean in practice, the
priority convention (lower numbers admitted first, same direction and
default as WordPress's `add_action` priority argument) stated
concretely with an example rather than in "higher/lower priority" words,
the deliberate no-anti-starvation tradeoff, and a short
"why would I want this" motivating example (a background sync tagged at
low priority so it never delays interactive requests on the same token).

## Adoption in this repo, once shipped

A separate, later step - not part of go-pco's own change:

1. Bump `go.mod`'s pinned `go-pco` version to the tagged release.
2. Call `pco.ConfigureThrottle(...)` once during startup (`server.go`,
   alongside this repo's other package-level construction), or set
   `PCO_THROTTLE_ENABLED=true` (plus rate/default overrides) via env -
   pick whichever this repo's own conventions favor once it's real; env
   var is likely the better fit here given how this repo already
   configures most things.
3. Tag the two existing background syncs' contexts with a low priority
   (e.g. 20) via `pco.WithPriority` - `runSongInfoSync` and the
   person-data cache's equivalent background goroutine.
4. Leave interactive/render-path calls and the on-demand
   candidate-warnings/manual-refresh endpoints at the default priority
   (or slightly elevated, if real usage shows they still get delayed
   behind a large background sync) - decide with real data, not
   speculatively.
5. **Later cleanup, not required immediately**: once this is proven safe
   in production, `songInfoSyncBatchSize`/`songInfoSyncBatchPause` and
   the person-data cache's equivalent constants become redundant - the
   queue itself now provides the real safety guarantee, so those loops
   could simplify to a plain bounded fan-out tagged at low priority
   instead of hand-timed batches-with-pauses. Don't do this until the
   throttle has actually been running in production for a while -
   removing a working safety net before trusting its replacement would
   be backwards.
6. Add a `pco-api-risks.md` addendum once adopted, same as every other
   change in that doc's history.

## Open questions for whoever implements this

- Is 80 req/20s (80% of the real ceiling) the right default headroom, or
  should it be more conservative (e.g. 60%, matching this repo's own
  Song Info sync's existing 60% figure) until there's real production
  signal? Leaning toward starting conservative and raising it later
  rather than the reverse, since raising a limit is a much safer change
  to make after the fact than lowering one that already looked "safe."
