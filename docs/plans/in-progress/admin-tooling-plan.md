# Plan: app-operator admin tooling (separate minimal web app)

**Status: in progress.** Written 2026-09-08, following a research pass
into the current auth/org model and a decision from Derek. The shell
described below is built - `cmd/admin/main.go`, gated by `ADMIN_TOKEN`
via fiber's `basicauth` middleware, binding to `127.0.0.1` only, with the
first feature (`GET /feedback`, backed by the new `feedback.Store.List`)
live and smoke-tested locally. **Deployed 2026-09-15**: open question 1
(how a request actually reaches it) resolved as Tailscale - see "Deploy:
Tailscale" below - and confirmed live on `debian` (`admin`/`web` services
both up, `ADMIN_BIND_ADDR` set, main site responding). Open
question 2 is resolved: it shares `internal/feedback`'s `Store` directly,
as expected.

Companion research: the same conversation also covered mobile/API
strategy - see `docs/plans/not-started/mobile-app-strategy-plan.md` - a
separate decision, bundled only because both were raised together.

## The problem

This app has exactly one identity concept: a PCO-authenticated person
inside one PCO organization, resolved by `RequireAuth`/
`RequireActiveOrganization` (`internal/server/middleware/auth.go`,
`organization.go`). There is no role system, no "app operator" flag, and
**no email even stored** - `sessions`/`organizations` capture only
`person_id`, `person_name`, `organization_id`, `organization_name` (see
`internal/auth/sessions.go`, `organizations.go`). Every signed-in person
in an org has equivalent access within that org
(`docs/plans/shipped/pco-background-sync-plan.md`'s "every signed-in
person is a full admin" line is the one existing statement of this).

Derek (the app's operator, not any org's admin) needs cross-org
visibility that doesn't fit that model at all - starting concretely with
reading the `feedback` table, which is deliberately write-only today
(`internal/feedback/store.go`'s package doc: "no read method or in-app
view exists yet, since this app has no 'app operator, not just an org
admin' access-control concept to safely gate one with").

## Decision: don't bolt a role onto the multi-tenant auth model

Considered and rejected: adding an `is_operator` check keyed off a
hardcoded `person_id` inside `RequireAuth`/`RequireActiveOrganization`, or
a new middleware in that same chain. Rejected because:

- It introduces a role concept into a system explicitly designed to not
  have one, for exactly one user - the smallest possible justification
  for the largest possible blast radius (a bug in that check risks
  leaking cross-org data through the *same* session/cookie/middleware
  path every real org tenant relies on).
- The existing identity primitives (`person_id`, `organization_id`) are
  the only stable values to key off, and mixing "is this the operator"
  logic into the per-request org-scoping path that 100% of real traffic
  runs through is exactly the kind of code that's easy to get subtly
  wrong under later refactors.

**Chosen direction: a second, separate web app** - its own `cmd/`
binary, own small Fiber instance, own auth (a shared secret, not PCO
OAuth), reusing `internal/database`/`internal/feedback`/etc. as plain Go
libraries the way `cmd/dbreset`/`dbseed`/`pcoreset` already do. This
mirrors the existing `cmd/` precedent (direct `database.Open()` + store
construction, no session cookie, no Fiber-app-wide auth chain) but adds a
real HTTP layer instead of a one-shot CLI, because Derek wants browser
access (including from a phone), not just SSH+terminal.

## Rejected alternative: SSH-only CLI

A `cmd/admin` CLI following the exact `dbreset`/`pcoreset` shape (no
HTTP at all, gated purely by SSH access to `debian`) was the other real
option and is *cheaper* to build - no new auth code, no new port to
expose. Ruled out only because Derek wants this usable from a phone /
away from a terminal. Worth remembering as the fallback if the web-app
version turns out to be more infra than it's worth for what ends up being
occasional, simple lookups.

## Sketch of the chosen shape

- **New binary**: `cmd/admin/main.go`, structured like `cmd/pcoreset` -
  `database.Open()` directly, construct whichever stores it needs
  (`feedback.NewStore(db)` first; `auth.NewStore(db)` if org-listing
  becomes useful) - but instead of running once and exiting, build a
  small `fiber.App` and `Listen()`.
- **Auth**: a single shared secret from an env var (e.g.
  `ADMIN_TOKEN`), checked via one small middleware - HTTP Basic Auth or a
  bearer header, compared with `subtle.ConstantTimeCompare` the way any
  single-secret gate should be. No user table, no session cookie, no PCO
  OAuth involvement at all - deliberately outside the multi-tenant auth
  code path entirely, not sharing a package with it beyond the plain data
  stores.
- **Exposure**: bind to `127.0.0.1` only, reached via an SSH tunnel
  (`ssh -L`), *or* a genuinely separate port/subdomain gated at the host
  nginx config - that config lives on `debian` outside this repo (per the
  research pass, no reverse-proxy config is tracked here today), so
  either path means doing some of this outside version control. Needs a
  real decision before building, not a default:
  - **SSH tunnel to a localhost-only port** - zero new attack surface,
    zero nginx changes, but only reachable when Derek can open a tunnel
    (defeats "usable from my phone" unless a tunneling app is also part
    of the workflow).
  - **A real subdomain/path behind nginx** on `debian`, protected by the
    same shared-secret middleware (defense in depth, not the only gate) -
    reachable from anywhere, but is genuine new internet-facing surface
    and means touching untracked host config.
- **First feature**: `GET /feedback` listing (add a `List`/`Get` method to
  `internal/feedback.Store` - it's currently write-only by design, see
  above). Everything past that (org list/billing status lookups, one-off
  data fixes) is additive once the shell exists; don't build past the
  first real need. **Built** - `feedback.Store.List`, originally plain
  `html/template` rendering (not a templ component - this binary doesn't
  share `internal/views`' design system at all, see `cmd/admin/main.go`'s
  own doc comment). **Superseded 2026-09-15**: switched to real `.templ`
  components (`cmd/admin/views`) - see the "org/user visibility" pass
  below. The "doesn't share `internal/views`" half of that reasoning still
  holds; only the templating *engine* changed, not an adoption of the
  main app's layout/CSS/htmx.
- **Deploy**: a second service in the same `docker-compose.yml` stack on
  `debian` (own container, sharing the same Postgres) - see "Deploy:
  Tailscale" below for the built version, once open question 1 resolved.

## Open questions (need a real decision before building, not a default)

1. SSH-tunnel-only vs. real subdomain exposure (above) - depends on how
   often Derek actually expects to reach for this away from a terminal.
   **Resolved 2026-09-15: Tailscale.** Derek already has it running on
   `debian` (tailnet node `r-debian`) and his usual dev machine
   (`pop-os`), and confirmed his phone can connect - so the phone-reachability
   requirement that ruled out the SSH-tunnel-only option doesn't force the
   public-subdomain option either; Tailscale gets both with zero new
   public attack surface, the outcome flagged as strictly better in the
   "Exposure & security" section below. See "Deploy: Tailscale" below for
   what got built.
2. Whether this binary shares `internal/feedback`'s `Store` type directly
   (adding read methods to the same package the main app writes through)
   or gets its own read-only query layer - probably the former, mirroring
   how `cmd/dbseed`/`pcoreset` already import `internal/auth`/
   `internal/scheduling` directly, but worth confirming once a second
   read-heavy use case shows up and the shape of what's needed is
   clearer. **Resolved**: the former - `feedback.Store.List` was added
   directly to the existing package.
3. Nothing here is scheduled beyond what's built - this doc exists so the
   architecture questions are answered before building further, not as a
   commitment to build the rest soon.

## Planning pass, 2026-09-15: org/user visibility, billing controls, email

Derek wants three more admin features next - design decisions below, all
now **built** as of the same day (same conversation, second pass, budget
allowing): `GET /orgs` (org+billing list, with an inline extend-trial
action), `GET /people` (the new durable directory), and the
`trial_ends_at` sync fix, all live in `cmd/admin/main.go`/
`internal/auth/`/`internal/billing/`. Not yet smoke-tested against a real
Stripe test-mode subscription or deployed - see "Still open after this
pass" at the end of this section. Also touched: `cmd/admin`/`admin`
root-binary gitignore cleanup and `make admin` switching to air-based live
reload (`.air.admin.toml`).

### Org + user directory with billing status

**Built.** `GET /orgs` (`cmd/admin/main.go`'s `orgsPage`) reads
`auth.Store.ListOrganizations` directly - no new store method needed
there, as expected. `GET /people` (`peoplePage`) reads the new
`people` table below. Both, and `/feedback`, now render via real `.templ`
components under `cmd/admin/views` (converted from the original
`html/template` strings the same day - see "Templating engine" below for
why and what changed) with a small shared `layout`/`nav`/`style`
component set so the three pages don't triple the inline `<style>` block.
`/people` shows the org's name (`Person.OrganizationName`, joined live
from `organizations` in `ListPeople` via `LEFT JOIN` - not a denormalized
copy on `people` itself, so it can't drift; `LEFT` rather than `INNER`
since `UpsertOrganization`/`UpsertPerson` are both best-effort at sign-in
and an `INNER JOIN` would silently drop a person if the former ever
failed).

### Trial extension / credit for an already-paying org

**Decision: extend the Stripe subscription's `trial_end`**, not a local
credit/coupon mechanism. Confirmed live in the code that this requires no
new webhook branch: `handleSubscriptionChanged`
(`internal/billing/webhook.go`) already writes whatever `sub.Status`
Stripe reports straight into `organizations.status` on every
`customer.subscription.updated` event, and setting `trial_end` on an
active paid subscription is exactly what makes Stripe emit that event
with `status = "trialing"` while pausing billing until that date - the
same mechanism a brand-new trial uses, just applied to an existing
subscription.

**Real gap found, must be fixed as part of this, not discovered later**:
`UpdateOrganizationSubscription` (`internal/auth/organizations.go`) never
writes `trial_ends_at` - only `status`/`plan`/the Stripe ids - because
until now nothing ever needed to change it after signup (it's a one-time
table `DEFAULT`, see migration `00025_organizations.sql`). `Require
ActiveOrganization` gates a trialing org on `time.Now().Before(org.Trial
EndsAt)` (`internal/server/middleware/organization.go`). So setting
Stripe's `trial_end` two months out on an org that's been paying since
March would flip local `status` to `"trialing"` via the webhook but leave
`trial_ends_at` sitting at that org's original, long-expired signup-trial
date - which **locks the org out**, the opposite of the intended effect.
Fix: thread `sub.TrialEnd` through `UpdateOrganizationSubscription` so it
updates `trial_ends_at` too, on every call site (checkout-completed and
subscription-changed both already have `sub` in hand). Once that's in
place it's correct for any future Stripe-side trial change, not just an
admin-triggered one (e.g. one set by hand in the Stripe dashboard).

Once the fix lands, the admin action itself is just "call Stripe's
subscription-update API with a new `trial_end`" - no separate local DB
write needed, the webhook already round-trips it back.

**Built.** `UpdateOrganizationSubscription` now takes a `trialEndsAt
*time.Time`, COALESCEd against the existing column so an event with no
`trial_end` (the common case) never zeroes it out; `webhook.go`'s
`trialEndFrom` helper converts `sub.TrialEnd` (0 when absent) into that
pointer at all three call sites. `billing.Client.ExtendTrial` is the new
admin-facing method - sets `trial_end` on an existing subscription with
`ProrationBehavior: "none"` (an operator-granted extension has nothing to
prorate). `POST /orgs/:id/extend-trial` (`cmd/admin/main.go`'s
`extendTrial`) branches on whether the org has a `StripeSubscriptionID`
yet: no subscription (still the original signup trial) writes
`trial_ends_at` directly via the also-new `auth.Store.
SetOrganizationTrialEnd`; a live subscription goes through
`ExtendTrial` and lets the webhook round-trip the local column, as
designed above. **Not yet exercised against a real Stripe test-mode
subscription** - worth doing before relying on it for a real org.

### Capturing email: a new `people` table

Derek asked whether this belongs on `sessions`, `organizations`, or a new
table. **Decision: a new `people` table**, org-scoped, upserted at sign-in
- same pattern as `organizations` itself (own table, written in
`Callback` right next to `UpsertOrganization`, deliberately decoupled from
session lifecycle):

```
people (organization_id, person_id, name, email, created_at, updated_at)
  PRIMARY KEY (organization_id, person_id)
```

Composite key, not a bare `person_id` PK - like every other org-scoped
table in this app, a PCO person id is only unique *within* one PCO
organization, not globally. Rejected alternatives and why:

- **`sessions`**: wrong lifetime. `Logout` calls `DeleteSession`
  immediately, and even without an explicit logout every session expires
  after `SessionTTL` (30 days) - email stored only there disappears
  exactly when Derek would still want to be able to email someone who
  isn't currently signed in.
- **`organizations`**: doesn't fit - it's account-level state, not a
  person's data, and an org can have multiple people.

The email itself is already available for free: PCO's OAuth userinfo
response (fetched by `auth.FetchIdentity`, `internal/auth/
pco_oauth.go`) already includes an `email` field today - it's parsed into
a local struct and silently discarded (`FetchIdentity` only returns
`personID, personName`). Capturing it is: extend `FetchIdentity`'s return
signature, add the `people` upsert call in `Callback` alongside
`UpsertOrganization`. Upserting on every sign-in also means it self-heals
if someone's email changes in PCO, and `updated_at` doubles as a cheap
"last seen" signal for other ideas below - existing sessions won't
backfill until their next sign-in, which is an accepted gap, not
something to special-case.

**Built.** Migration `00047_people.sql`, `internal/auth/people.go`
(`UpsertPerson`/`ListPeople`), `FetchIdentity` now returns `personEmail`
as a third value, and `Callback` upserts into `people` right next to its
existing `UpsertOrganization` call - all best-effort, same tolerance as
that call. Existing sessions still won't backfill until their next
sign-in, as expected/accepted above.

**Privacy policy: also done.** `internal/views/privacy.templ`'s "Account
and session data" list now has a line covering what's collected (name +
email, sourced from the PCO OAuth profile at sign-in), why
(support/billing contact reachable independent of session lifetime, not
customer-facing), and that it's operator-visible only - "Last updated"
bumped to September 15, 2026 alongside it.

### Templating engine: switched to `.templ`

Derek asked whether moving off the original `html/template` strings to
real `.templ` components (same engine/build step as the main app) would
be a heavy lift. It wasn't - **built** the same session. This is purely
an engine swap, not a reversal of the original "doesn't share
`internal/views`" decision above: a new small `cmd/admin/views` package
(its own `layout`/`nav`/`style` components, no htmx, no design-system CSS,
no session/nav-state assumptions - none of that applies to a
shared-secret-gated, no-session binary) holds `Feedback`/`Orgs`/`People`,
each one `@layout("Title") { ... }` wrapping the same markup the old
inline strings had. `main.go` now has a `render` helper mirroring the
main app's own `render.go` shape (`component.Render(ctx, w)`), and the
handlers are otherwise unchanged - fetch from the store, hand the result
to the view. `templ`'s default `{ expr }` interpolation auto-escapes,
same protection the old `html/template` had for `/feedback`'s untrusted
`SubmitterEmail`/`Message` fields - no regression there. The one real
follow-up cost: `.air.admin.toml`'s build command now runs
`templ generate ./cmd/admin/...` before `go build` (mirroring the main
app's `make build` recipe), and its `include_ext` gained `"templ"` - a
manual `go build ./cmd/admin` (outside `make admin`/air) needs a
`templ generate` run first, same caveat CLAUDE.md already documents for
the main app's own `.templ` files.

### Still open after this pass

**Resolved 2026-09-15**: the migration has run against a real dev DB, and
`/orgs`' extend-trial action has been exercised against a real Stripe
test-mode subscription. `go build ./...`/`go vet ./...`/`go test ./...`
are all clean as of this pass.

### Admin app exposure & security

**Resolved 2026-09-15: Tailscale**, see open question 1 above. The
tradeoffs below are why, kept for reference:

- **Basic Auth's real weakness here isn't the transport** - real HTTPS
  protects `ADMIN_TOKEN` in flight fine. It's that the admin app today has
  none of the hardening the main app already applies to its own public,
  pre-auth routes: `/auth/pco/start`/`callback` get `middleware.RateLimit
  (20, time.Minute)` (`internal/server/controllers/auth.go`); the admin
  app's `basicauth` check (`cmd/admin/main.go`) has no rate limit, no
  lockout, and logs nothing on failure. A single shared secret, reachable
  from the open internet, with unlimited guesses, is the actual risk.
- **If the public-subdomain path is chosen**, minimum hardening before
  exposing it: confirm `ADMIN_TOKEN` is high-entropy (not memorable),
  add `middleware.RateLimit`-style throttling (or fail2ban at nginx
  watching for repeated 401s), log every request path + result, and use a
  non-obvious subdomain name as one more free layer.
- **A third option surfaced this pass, not in the original sketch above**:
  Tailscale (or plain WireGuard) instead of a public subdomain - gets
  "reachable from Derek's phone from anywhere" with *zero* public attack
  surface (no port ever exposed to the internet, no brute-forceable
  secret, nothing for a scanner to find), which is strictly better than
  either original option on the actual tradeoff (SSH tunnel = secure but
  terminal-only; public subdomain = phone-reachable but new public
  surface) for a single-operator tool with no need for public
  discoverability. Derek doesn't yet have a solid mental model of
  Tailscale/WireGuard and isn't ready to commit to it - revisit open
  question 1 once that's true, rather than defaulting to the public
  subdomain path just because it was the original sketch's other option.

### Deploy: Tailscale

**Built and deployed 2026-09-15.** `cmd/admin/main.go` binds to
`ADMIN_BIND_ADDR` (env var, default `127.0.0.1` so local `make admin` is
unchanged) instead of a hardcoded `127.0.0.1`. `docker-compose.yml` gained
a second service, `admin`, alongside `web` - same `network_mode: host`
reasoning as `web` (native Postgres on the host, no bridge NAT to fight),
built from a new `Dockerfile.admin` (mirrors the main `Dockerfile` but
builds `./cmd/admin` and skips `web/static`/`pg_dump`, neither of which
that binary uses). `debian`'s `.env` now has `ADMIN_BIND_ADDR` set to that
host's Tailscale IP (node `r-debian`) - the app is reachable at
`http://100.125.19.115:7824` from any device on Derek's tailnet (`pop-os`,
and his phone once its Tailscale app is connected) and from nowhere else;
no nginx change, no new public port. `ADMIN_TOKEN`/Basic Auth stays on as
defense in depth even though the tailnet is the real access control.
Confirmed live post-deploy: both `admin`/`web` containers up on `debian`
and the main site responding.

### Log-in-as (impersonation) - design note, not built

Derek has raised this before as a wanted feature. One nuance specific to
this app's architecture makes it more sensitive than typical SaaS
impersonation, worth designing around deliberately rather than copying a
generic pattern: this app has **no independent server-side identity**
beyond the live PCO OAuth token itself - every real action authenticates
to PCO using the signed-in session's actual `AccessToken`
(`middleware.RequestContext`). So "log in as a customer" can't mean
minting a fake session for their id the way a typical multi-tenant SaaS
does - seeing what they see means reusing *their real, live PCO
credential*, including its write access to their actual PCO data.

There's already a precedent for borrowing a live session's token for a
non-interactive purpose: `SessionForBackgroundSync`
(`internal/auth/sessions.go`), used by the sync cron and `cmd/dbseed`.
The same constraint (only works when the org actually has a currently-live
session to borrow - nothing conjured for an org nobody's signed into) is
the right starting shape for this too. Before building: add an explicit
audit log entry every time it's used (who/which org/when), and seriously
consider disabling mutating routes while "viewing as" so it defaults to
read-only support access rather than full write access to a customer's
live PCO data.

## Planning pass, 2026-09-15 (continued): usage signals - sign-ins and setup-page adoption per org

Derek asked whether admin could show "what people are using more than
others" from data already on hand. First pass built a login-frequency
signal; Derek then clarified mid-build that the real question is *setup
feature adoption* - each org's home page shows the same 8-item onboarding
checklist (`HomeController.buildOnboardingChecklist`,
`internal/server/controllers/home.go`) as a per-org "have they actually
used the setup pages" signal, and that's what he wants visible from
admin, not just login frequency. Both are now **built**, as two more
columns on `GET /orgs` (`cmd/admin/main.go`'s `orgsPage`,
`cmd/admin/views/orgs.templ`):

- **Sign-in frequency**: migration `00048_people_sign_in_count.sql` adds
  `people.sign_in_count`, incremented (not overwritten) by
  `UpsertPerson` on every sign-in. `auth.Store.OrgActivityByOrganization`
  rolls that up per org (person count, total sign-ins, most recent
  `updated_at`) via a plain `GROUP BY` - `/orgs` shows People/Sign-ins/
  Last active columns from it, with "Last active" flagged `.stale` past
  30 days. This also fully covers the "Dormant-org / last-seen list"
  brainstorm item above - struck, no separate view needed.
  `cmd/admin/views/people.templ`'s own table also gained a Sign-ins
  column from the same field.
- **Setup-page adoption**: `internal/scheduling/admin_setup_completion.go`
  is the new piece - `SetupCompletion` (8 bools, one per onboarding-
  checklist category, Position Preferences excluded same as that
  checklist since it lives in PCO not this app's DB) and
  `Store.SetupCompletionByOrganization`, a single `UNION` query across the
  8 backing tables (`song_relationships`, `song_roles`, `song_contexts`,
  `song_tags`, `song_vocal_ranges`, `song_person_preferences`,
  `person_relationships`, `person_vocal_ranges` - all org-scoped, matched
  against the same tables `HasAnyX` already checks for the home page's own
  checklist) rather than 8 separate round trips. Deliberately unscoped,
  same documented cross-org exception as `feedback.Store.List`/
  `auth.Store.ListOrganizations`/`ListPeople`/`OrgActivityByOrganization` -
  this app still has no "app operator" access-control concept to safely
  gate a cross-org read with on the main multi-tenant app itself. `/orgs`
  shows it as a `N/8` count per org with a plain HTML `title` tooltip
  breakdown (no JS added to this binary) via a small label table in
  `orgs.templ`.
- **Ruled out as not-cheap**: attributing `sets`/`rosters` draft-building
  activity to an org (a deeper "how much are they actually scheduling"
  signal) - neither table has an `organization_id` column at all (only
  `service_type_id`/`plan_id`), confirmed via their migrations. Would need
  a real join through those to attribute to an org; left as a future
  decision, not built here.

`go build ./...`/`go vet ./...`/`go test ./...`/`gofmt -l` all clean as of
this pass.

### Further brainstorm (candidates, not commitments)

Not designed, just worth keeping on the list - grounded in things that
already partly exist:

- **Stripe/seat reconciliation view** - surface orgs where local
  `plan`/seat state may have drifted from Stripe, tied to whatever
  `jobs.BillingJobs` already reconciles on its own schedule; catch a
  broken sync before a customer notices.
- ~~**Dormant-org / last-seen list**~~ - built, see "usage signals" pass
  above: `/orgs`' Last active column (`OrgActivityByOrganization`) covers
  this directly.
- **Force sign-out** - revoke every session for an org - building block
  (`DeleteSessionByID`) already exists - useful for a security incident or
  an offboarding request.
- **Raw-id lookup** - paste a PCO org or person id straight from a log
  line or bug report and jump to that org, since logs/error messages
  reference raw ids constantly.
- **Manual cron trigger** - re-run one job for one org on demand (e.g.
  re-sync Stripe seats for a single org) instead of waiting for the
  schedule, for debugging.
- **Feedback triage** - the existing `/feedback` list is read-only; a
  "mark reviewed" flag would make it usable as a queue, not just a dump.
