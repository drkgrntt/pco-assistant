# Plan: app-operator admin tooling (separate minimal web app)

**Status: in progress.** Written 2026-09-08, following a research pass
into the current auth/org model and a decision from Derek. The shell
described below is built - `cmd/admin/main.go`, gated by `ADMIN_TOKEN`
via fiber's `basicauth` middleware, binding to `127.0.0.1` only, with the
first feature (`GET /feedback`, backed by the new `feedback.Store.List`)
live and smoke-tested locally. Not deployed anywhere yet - open question 1
below (how a request actually reaches it) still needs a real decision
before that happens; the binary itself works unchanged regardless of
which way that's decided, so building it didn't have to wait. Open
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
  first real need. **Built** - `feedback.Store.List`, plain
  `html/template` rendering (not a templ component - this binary doesn't
  share `internal/views`' design system at all, see `cmd/admin/main.go`'s
  own doc comment).
- **Deploy**: likely a second service in the same `docker-compose.yml`
  stack on `debian` (own container, own port), sharing the same Postgres
  - not a new host, not new infra beyond one more compose service. Not
  done yet - blocked on open question 1 below.

## Open questions (need a real decision before building, not a default)

1. SSH-tunnel-only vs. real subdomain exposure (above) - depends on how
   often Derek actually expects to reach for this away from a terminal.
   **Still open** - the binary itself (auth, routing, `127.0.0.1` bind) is
   built and works either way; this only gates the `docker-compose.yml`/
   nginx deploy step.
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
