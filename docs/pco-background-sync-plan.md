# Plan: unattended cache warming via borrowed idle session tokens

**Status: shipped 2026-09-04.** Implemented as `jobs.PCOJobs`
(`internal/server/jobs/pco.go`, hourly) plus `controllers.RunOrgBackgroundSync`
(`internal/server/controllers/pco_background_sync.go`) - see
`docs/pco-api-risks.md`'s changelog entry ("PCO call-efficiency rollout,
Phase 3") for exactly what landed, including the two deliberate deviations
from this doc's exact wording: no new "discovery already ran up through
plan X" marker table (the "Open questions" section's alternative - a fixed
5-plan-per-service-type window is simply re-walked every tick instead,
since the existence check already skips everyone previously discovered),
and `triggerPersonDataSync` was split into a `*fiber.Ctx` wrapper plus a
new `triggerPersonDataSyncCtx` ctx/store/org-based core (mirroring
`triggerSongInfoQueueSync`'s existing shape) rather than left as a
fiber-only function, so this cron could call the identical drain-and-run
logic without needing a `*fiber.Ctx` of its own. Section below left
as-written for reference.

Written 2026-09-04, prompted by a conversation about whether Song Info's
"cache all" button + home-page nag are still pulling their weight. That
conversation converged on two separable pieces:

1. **Song Info's cache-miss path should match `person_data_cache.go`'s**
   (queue-and-batch instead of block-and-fetch-inline), which kills the
   button/nag's *reason to exist* - not written up here, it's a small,
   low-risk change that mirrors an existing, proven pattern directly. See
   the conversation this doc comes from for the specifics; implement it
   independently of this plan.
2. **This doc** - the bigger, genuinely new piece: whether either cache
   should also warm itself for an org where nobody's actively rendering
   pages, by borrowing that org's own idle session token on a schedule.

**This is explicitly the thing `docs/pco-roster-cold-start-plan.md`'s
"unattended-cron question, revisited" section considered and declined to
build**, with an explicit condition attached: "revisit the unattended
version only if staleness genuinely proves to matter for orgs that go a
long time between visits - not speculatively." That condition hasn't been
met - no org has reported stale Song Info or person data as an actual
problem. Same posture as `docs/pco-throttle-plan.md` when it was written:
**not urgent, written up for later rather than built now.** Don't start
implementing this from a re-read of this doc alone without first checking
whether that's changed.

## What's changed since the cold-start plan declined this

That doc's stated objection was needing "new mechanism this codebase
doesn't have" (picking/refreshing an idle session's token for unattended
use) and a "subtler risk" (a background sync competing with that same
person's own live usage for their 100/20s budget). Neither objection
holds up the same way anymore:

- **The mechanism is smaller than it looked.** `auth.RefreshAccessToken`
  (`internal/auth/pco_oauth.go`) is already a bare function, independent
  of `*fiber.Ctx` - `middleware.RequireAuth` calling it inline is just
  its current only caller, not something baked into the request
  pipeline. A cron can call it directly on a stored `refresh_token` the
  same way, then persist the result via the existing
  `auth.Store.UpdateSessionTokens`.
- **The contention risk is already solved, by something that didn't
  exist when that doc was written.** `docs/pco-throttle-plan.md`'s
  priority-aware throttle (shipped, `PCO_THROTTLE_ENABLED`) already
  guarantees low-priority traffic yields to interactive traffic on the
  same token - `SyncSongInfo`'s background goroutine already opts into
  this via `pco.WithPriority(ctx, 20)`. A cron-borrowed token tagged the
  same way gets "don't step on this person's own live usage" for free.
  This is also why **no `last_accessed`/idle-detection column is
  needed** - an idea considered in the conversation this doc comes from
  and dropped for the same reason: the throttle already provides
  backpressure at the point of contention, which is a strictly better
  signal than a coarse "hasn't been used in N minutes" heuristic guessed
  from the caller's side.

What's still true and unresolved: **whether a session sitting idle for
days still has a refresh token PCO honors is unverified.** Checked the
local dev DB for a real idle session to test against - only one session
existed, refreshed within the last ~2 hours, not a useful test subject.
Per the conversation's own resolution: don't block on this, **design for
graceful failure instead** (see below) - a refresh failure just means
skip that org this cycle, indistinguishable from "no session available
at all." Revisit live verification only if this cycle's skip rate turns
out to matter in practice.

## Scope: Song Info fully, person data for refresh AND recent-history discovery

The cron's job on each tick is "for orgs with nobody around to trigger
this organically, do the discovery + kick off the same paced sync that
already exists." That's a clean fit for Song Info: the full candidate
list is one cheap, already-cached call
(`scheduling.FetchActiveSongs`), and "needs sync" is a plain diff against
`song_arrangement_cache` - exactly what `buildSongInfoData`
(`song_arrangement_cache.go`) already computes for the page.

Person data has three angles, not two - an earlier draft of this doc
conflated the second and third below and wrongly called both "blocked."
Correcting that:

- **Refresh (a person this org already knows about, now stale)** - a
  clean fit, no new gap needed. `person_data_cache.go`'s queue
  (`personSyncPending`) is normally populated as a side effect of a
  render (`otherAssignmentDates`/`crossTeamAssignmentsThisWeek`/
  `personBlockouts` in `rosters_warnings.go` enqueueing a miss they hit),
  but the three sync-marker tables underneath it
  (`person_blockout_sync`/`person_plan_history_sync`/
  `person_team_plan_history_sync`, migration `00033`) are themselves a
  durable, org-scoped, purely-local record of every `(person[, team])`
  this org has *ever* organically synced - independent of PCO, no live
  fetch required to read it. The cron can query those three tables for
  rows older than `personCacheTTL` (`rosters_warnings.go`), turn each
  into the matching `personSyncRequest`, and hand the batch straight to
  the already-existing `runPersonDataSync`. New store method needed:
  something like `StalePersonSyncTargets(ctx, organizationID, ttl)
  ([]personSyncRequest-shaped rows, error)` reading across the three
  tables, one new query per concern-file the way `person_data_cache.go`'s
  existing methods already are.
- **Recent-history discovery (seed a brand-new org, or one whose team
  membership has shifted, with people it hasn't organically encountered
  yet)** - also a clean fit, and this is the piece worth having
  specifically for the "just connected, hundreds of volunteers" case:
  `pco.GetTeamMembers(serviceTypeID, planID)` - already called live on
  every roster render today (`rosters_builder.go:294`,
  `rosters_scoped.go:64`) - returns "every person currently assigned to
  fill a position on **this plan**" (its own doc comment in go-pco), a
  small, plan-bounded list, *not* the org-wide eligibility pool. Walking
  a bounded window of recent **past** plans per service type (already
  fetchable via `combinedPlansForServiceType(..., past: true)`,
  `build.go`) and calling `GetTeamMembers` once per plan turns "who has
  actually served recently" into a handful of cheap, already-proven-safe
  calls - independent of total org size, since it's bounded by
  (recent plan count) x (service type count), not volunteer count. Each
  `PlanPersonData` result carries the person id and team id directly
  (`PlanPersonRelationships.Person`/`.Team`), enough to seed both
  `personSyncKindBlockouts`/`personSyncKindCrossTeamHistory` (person-only)
  and `personSyncKindTeamHistory` (person+team) requests for anyone with
  no sync-marker row yet. Feeds the same `runPersonDataSync` pipe as the
  refresh sweep above - a second discovery query, still no new sync
  mechanics.
- **Full eligibility-pool discovery (everyone who *could* fill a
  position, whether or not they've served recently)** - this is the one
  still genuinely blocked, and the one `docs/pco-api-risks.md`'s "Team
  Builder candidate eligibility pool" row already describes as scaling
  with volunteer-pool size, not roster size - a real org-wide enumeration
  problem, unlike the two bullets above. A volunteer who serves rarely
  enough to fall outside the recent-plans window above would still go
  undiscovered until they're actually scheduled once (at which point the
  refresh sweep picks them up like anyone else). Acceptable gap for a
  first version: this only means "not pre-warmed before their first real
  use," not "broken" - same cold-start-render fallback
  (`PersonDataSyncPending`) that already covers a genuinely brand-new
  person today.

Net: this plan's first version should include Song Info (full),
person-data refresh, and person-data recent-history discovery - all
three are bounded, already-proven-safe calls or pure local-DB reads, zero
new PCO enumeration risk. Only the full eligibility-pool case stays out
of scope, tied to the same already-flagged gap as before.

## Design

### New job: `jobs.PCOJobs`

`internal/server/jobs/pco.go` already exists as an empty placeholder
("for PCO-specific scheduled jobs... mirroring AuthJobs so new jobs have
an obvious home") - this is exactly what it was left empty for. Give it
an `authStore *auth.Store` and `schedulingStore *scheduling.Store`
(matching `BillingJobs`'s constructor-injection shape) and one job,
registered the same way `AuthJobs`/`BillingJobs` are in `crons.go`.

### Cadence

Something in the range of `AuthJobs`'/`BillingJobs`' own 24h cadence is
almost certainly wrong here - those jobs are pure housekeeping
(idempotent, cheap, "did anything change" is irrelevant). This job
spends a real chunk of an org's own PCO budget every time it actually
finds work, so it should run more often but do nothing for the vast
majority of orgs on the vast majority of ticks (already-warm orgs are a
cheap no-op - one `FetchActiveSongs` call, no gap found, done). An hourly
tick, skipping any org with no gap, is a reasonable starting point -
revisit against real prod behavior once shipped rather than tuning this
number speculatively now.

### Per-org pass

For each organization with at least one non-expired session row
(new `auth.Store` method needed - something like
`SessionForBackgroundSync(ctx, organizationID) (Session, bool, error)`,
picking e.g. the most-recently-created live session; which one doesn't
matter much since any of them authenticates as *some* real person in
that org with real PCO access):

1. `auth.RefreshAccessToken(ctx, session.RefreshToken)`. **On failure**:
   this session's refresh token is no longer honored by PCO (revoked
   access, disconnected app, or - the open question above - simply too
   old). Delete it now (`auth.Store.DeleteSession`), log it, and skip
   this org for this tick. This is a strictly better cleanup trigger than
   `deleteExpiredSessions`' 30-day TTL sweep (`jobs/auth.go`) - a dead
   refresh token is a much stronger "this session is actually useless"
   signal than merely being 30 days old, and this catches it far
   earlier. Not a separate job: same store method
   (`DeleteSession`), just called from a second call site.
2. On success, persist the refreshed tokens back via
   `auth.Store.UpdateSessionTokens` - same as `RequireAuth` does inline -
   so a real visit using this same session later doesn't immediately
   need its own refresh, and so the next tick's `RefreshAccessToken` call
   is against a token that's actually still fresh rather than needlessly
   re-refreshing one just refreshed an hour ago (`RefreshAccessToken`'s
   own doc comment: trade only "at or near expiry," which this satisfies
   by construction here since the cron always refreshes before use).
3. Build a `context.Background()`-derived context carrying the refreshed
   access token at low priority - the same construction `SyncSongInfo`
   already does (`pco.WithAccessToken`, `pco.WithPriority(ctx, 20)`,
   `logger.WithRequestID` with a fresh id for this cron pass, not a
   request's).
4. Call the *existing* Song Info sync entry point with that context -
   `runSongInfoSync`'s shape (or a thin wrapper exposing it outside
   `song_arrangement_cache.go`'s own handler), reusing
   `songSyncTryStart`/`songSyncInProgress` unchanged so this never races
   a concurrent organic sync (someone clicking "Sync now," or - once
   built - a queued cache-miss pass) for the same org. **No new pacing,
   batching, or gap-detection logic** - this step's only job is
   supplying a valid token from a context nobody's actively watching,
   the sync mechanics underneath are 100% existing code.
5. Query the new `StalePersonSyncTargets` store method (see "Scope"
   above) for the same org, turn any stale `(person[, team])` rows into
   `personSyncRequest`s, and feed them straight into `runPersonDataSync`
   under the same `personSyncTryStart`/`personSyncInProgress` guard the
   organic path already uses - same reasoning as step 4, this is a
   second existing sync entry point, not a new one.
6. Fetch this org's recent **past** plans per service type
   (`combinedPlansForServiceType(..., past: true)`, bounded to a small
   window - see "Open questions" below for how many), call
   `pco.GetTeamMembers(serviceTypeID, planID)` once per plan, and for
   every `(person_id, team_id)` pair that comes back with **no existing
   sync-marker row at all** (a genuinely new discovery, not a refresh),
   enqueue the matching `personSyncRequest`s the same way step 5 does.
   Skip anyone `StalePersonSyncTargets` already picked up in step 5 -
   they don't need discovering, only refreshing. This is the "recent
   history of people" seed for a just-connected org: bounded by
   (recent-plan count) x (service-type count), independent of how many
   total volunteers the org has.

Steps 4-6 all run under the one token this pass already refreshed in
step 1 - no second refresh needed.

### What this deliberately does not change

- The manual per-org "resync" action discussed for the Account page (the
  small companion piece to killing the button/nag) stays - this cron is
  a complement for orgs nobody's visiting, not a replacement for "I want
  this to happen right now."
- No behavior change for an org with zero sessions at all (a connected-
  but-never-signed-in-again org, or one where every session already aged
  out) - it's simply skipped every tick, same as today's total absence of
  unattended sync.
- Still governed by the same `songInfoSyncBatchSize`/
  `songInfoSyncBatchPause` and `personDataSyncBatchSize`/
  `personDataSyncBatchPause` pacing as any other sync of each kind - this
  doesn't add a second, differently-tuned rate limit to reason about for
  either cache.
- Full eligibility-pool discovery (a person who's never served recently
  enough to appear in step 6's window, and has no sync-marker row) stays
  exactly as unimplemented as it is today - see "Scope" above for why
  that's an acceptable, bounded gap for a first version rather than a
  silent omission.

## Open questions before implementing

- **Refresh-token idle lifetime** (see above) - not blocking, but worth
  keeping an eye on once shipped: if a meaningful fraction of orgs turn
  out to get their session deleted by step 1 above on a regular cadence
  (not just once, ever), that's a sign the assumption "sessions survive
  idle for a useful window" was wrong, and this plan's value is much
  lower than it looks on paper.
- **Which session to pick per org**, if more than one is live - probably
  doesn't matter (any live session in the org has equivalent PCO access,
  per this product's "every signed-in person is a full admin" model -
  see `SignOutPerson`'s doc comment in `account.go`), but worth a
  one-line decision rather than an arbitrary `LIMIT 1` nobody chose on
  purpose.
- **Whether an hourly cadence is actually right** - see "Cadence" above;
  this is a guess, not a measurement.
- **How wide step 6's "recent past plans" window should be** - too
  narrow and a monthly-rotation team doesn't get discovered before
  someone happens to render a page for them; too wide and a long-idle
  org's cron pass gets needlessly expensive re-walking plans that were
  already covered by a previous tick. A per-org "discovery already run
  up through plan X" marker (one more small table, or reuse
  `AnyPersonDataSynced`'s existing per-org check as a cheap "already has
  *some* history, only look for genuinely new plans since last pass"
  guard) is probably worth it rather than always re-walking a fixed
  window - decide at implementation time rather than guessing the number
  here.
- **Full eligibility-pool discovery** - blocked on the same
  `docs/pco-api-risks.md` gap as before, not on anything in this plan;
  don't attempt it until that closes. (Refresh and recent-history
  discovery are both in scope for this plan - see "Scope" above; only
  the full eligibility pool is excluded.)

## Testing

`songSyncBatches`/`runPacedBatches`/`personSyncBatches` are already
unit-tested independent of DB/PCO (`song_arrangement_cache_test.go`,
`person_data_cache_test.go`) and untouched by this plan. New coverage
needed is narrower: the token-refresh-then-delete-on-failure branch (a
fake/failing `RefreshAccessToken` path deleting the right session and
skipping cleanly), the "which org/session gets picked, already-in-progress
orgs get skipped" selection logic (both plain-Go, no live PCO required),
`StalePersonSyncTargets`' TTL-boundary logic, and the recent-history
discovery query's "skip anyone already caught by the refresh sweep, only
enqueue genuinely new (person, team) pairs" dedup logic - both of the
latter are DB-backed tests the same shape as `person_data_cache_db_test.go`'s
existing backdated-`synced_at` tests. Follows
`docs/pco-roster-cold-start-plan.md`'s own "decide up front whether this
needs the DB-backed harness" note.

## Rollout

Real (non-trivial) code change if and when this gets built - branch,
commit, push, **wait for explicit "merge and deploy"** per CLAUDE.md's
standard sequence, same as any other change touching PCO call volume.
Update `docs/pco-api-risks.md`'s "Current state" table and changelog once
shipped, same as every other fix in this area - add an "unattended sync
(Song Info + person-data refresh + person-data recent-history discovery)"
row, and leave "Team/position lists" as-is (only the full eligibility-pool
case still depends on it, unchanged by
this plan).
