# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

PCO Assistant - a self-hosted scheduling assistant for Planning Center
Services. It does *not* duplicate PCO's own data model: songs, people,
plans, teams, and positions are all read live from PCO via
[`go-pco`](https://github.com/drkgrntt/go-pco) (a separate module/repo,
pulled in as a normal tagged dependency). This app's own Postgres database
only stores what PCO has no concept of - song/person relationships, role
tags, scheduling preferences, and draft sets/rosters - which get synced
back to real PCO records on Publish. See `README.md` for the full feature
list and pricing/billing model; it's worth reading before making changes
to billing, publish/sync, or the Build page.

**Product direction**: `docs/ideas.md`'s "Vision / positioning" section holds
the True North - every feature should either **protect the team**
(burnout, preferences, relational friction, mentorship pairing) or
**protect the service** (repetition fatigue, set flow, conflicts caught
before publish). Check a proposed feature against those two promises
before recommending it, especially when asked to prioritize among
docs/ideas.md's brainstorm items - "does PCO already do this" is the wrong
test (see that section for why); "which promise does this serve, and how
directly" is the right one. That test picks *which* features belong on
the roadmap; it doesn't say anything about *how* to build one. Friction
reduction is the separate, standing discipline for that - not a third
promise to weigh alongside the other two, but a build-time question for
every feature regardless of which promise it serves ("is this the fast,
obvious way to do the thing, or does it hand the user a second UI pattern
to learn / a reason to bounce back to PCO"). See "Reducing friction vs.
Planning Center" in docs/ideas.md for what that looks like concretely.

**Agent delegation**: not a blanket default, and different subagent types
fit different phases of a task - not just "delegate or don't":

- **general-purpose (isolation: worktree)** for a task that's
  fully-specified upfront, self-contained, and either runs in parallel
  with other independent work or benefits from not touching the live
  checkout - a feature build you can completely spec before handing off,
  with no need for the user to react mid-implementation, is the sweet
  spot (four have run well this way so far: a new read-only page, a
  warning-logic addition, a new outbound-email package, and the
  live-sync-reflection poll-while-open banner + extended MergeSet
  reconciliation, see docs/plans/in-progress/live-sync-reflection-plan.md -
  none of which touched files another one of them was also touching).
- **Explore** for the reconnaissance a spec needs before it can be
  written - "where does X live," "what's the existing pattern for Y,"
  "what fields does this struct/response actually have." It's read-only
  (no Edit/Write/Agent), so it can't wander into changing anything - one
  broad-fan-out prompt replaces what would otherwise be several sequential
  Read/Grep/Bash round trips spent just locating things before a real spec
  can be written. Reach for it whenever the goal is still "find out," not
  yet "change something."
- **Plan** for turning an open-ended design doc or problem into the
  concrete architecture a builder agent can execute without guessing - the
  gap between a docs/plans/ doc that deliberately doesn't pick a mechanism
  and lists "needs a real decision, not a default" open questions (see
  live-sync-reflection-plan.md's own phrasing) and an exact function/file/
  field list ready to hand to a builder. Plan resolves *how*, Explore
  resolves *where it lives*, general-purpose (worktree) *writes the code*
  - three phases of the same task, not competing options for the same one.

Whichever type, keep doing the rest directly: anything needing iterative
back-and-forth with the user (debugging against screenshots, "still
broken, try this instead" loops - a subagent can't see a screenshot and
wait for a reaction), git workflow (merge/deploy/branch cleanup -
sequential, sometimes touches production, and gated on the user's own
explicit go-ahead), small edits and doc changes, and anything touching
secrets/env files or live-mutating verification against an external API
(pulling a session token and curling the real PCO API to confirm its
actual behavior, say) - worth doing carefully and visibly yourself even
when it's just a handful of commands, not handed to a cold agent to
explore blind. Each spawn re-derives full context from its prompt alone
and costs real tokens (a general-purpose build runs on the order of
100k+; an Explore/Plan pass scoped to research or design alone typically
costs less) - real value for genuine parallel/isolated build work or the
research/design phase ahead of it, wasted on anything a direct edit or a
single Read/Grep would settle in one or two tool calls.

## Commands

```bash
make run        # templ generate + go run cmd/api/main.go
make watch       # live reload via air (installs air if missing)
make build       # templ generate + go build -o main cmd/api/main.go
make test        # go test ./... -v
make all         # build + test
make db-reset    # wipe local DB and rebuild from migrations (ARGS=-yes to skip confirm)
make db-seed     # populate local DB with example scheduling data
make pco-reset   # delete every Song/Plan in the signed-in PCO org (dev orgs only!)
```

Run a single test: `go test ./internal/server/controllers/ -run TestMergeServiceOrder -v`
(standard Go tooling throughout - no custom test runner).

`db-reset`/`db-seed`/`pco-reset` all refuse to run unless `APP_ENV=local`.
Local setup, `.env` variables, and what each of these commands actually does
are documented in `README.md` - read it rather than re-deriving from code
when working on seeding/reset flows.

Views are `.templ` files compiled to `_templ.go` by `templ generate` -
`make run`/`make build`/`make watch` all regenerate first. If you edit a
`.templ` file directly, regenerate (`templ generate` or `go generate` if
wired up) before relying on the `_templ.go` output; don't hand-edit the
generated `_templ.go` files.

The IDE's own diagnostics lag behind this - `*_templ.go` is gitignored,
so right after editing a `.templ` file (or when a new git worktree
appears alongside the checkout) the editor's language server routinely
flags real symbols as `undefined`/`unknown field`. This has been a false
alarm every time it's come up - always confirm with a real `go build
./...` (empty output = success) before trusting an IDE diagnostic over
it.

## Deployment

Production runs on a server reachable via the SSH alias `debian`, deployed
from `~/sites/pco-assistant` there via Docker Compose. Standard sequence
for a real (non-trivial) change:

1. Branch, commit, push the branch.
2. Wait for the user's explicit "merge and deploy" - never merge/deploy on
   your own judgment just because CI (there is none) or tests pass.
3. `git merge --no-ff <branch>` into `main`, push `main`.
4. `ssh debian "cd ~/sites/pco-assistant && git pull --ff-only && docker
   compose build && docker compose up -d"`.
5. Check `docker compose logs` for the goose migration line if the change
   included one (confirm it landed at the expected version, not just that
   it ran), then `curl` the live URL (`pco.derekgarnett.com`) to confirm a
   real response.
6. Wait for the user's explicit "delete the branch" before deleting it,
   locally and on origin.

Small/doc-only changes (a copy tweak, a README/docs/ideas.md correction) skip
the branch ceremony - commit straight to `main` - but still only on the
user's explicit ask to commit/push/deploy, same gating either way.

Never run or touch the user's own local dev server (port 8080) directly -
treat it as theirs, not something to start/stop/curl on their behalf.

Never `cat`/`tail`/otherwise print a real `.env` file (local or on
`debian`) - it's a live secrets file, and printing it puts every value in
it into this conversation's transcript, not just the one you meant to
check (this has actually happened - a `STRIPE_WEBHOOK_SECRET` leaked into
a transcript this way). To check which keys exist without exposing
values, `grep -o '^[A-Z_]*=' .env`; to check whether a specific key has a
value set (not what it is), `grep -c '^KEY_NAME=.\+$' .env`. Appending new
keys (`printf ... >> .env`) is fine and doesn't require reading the file
first.

When you need to confirm what Planning Center's API actually does rather
than trust documentation or an assumption, verify it live against the
signed-in dev PCO account instead of guessing - pull a session's access
token from the local `sessions` table rather than assuming a doc is
accurate; PCO's own behavior has contradicted its docs more than once
this project's history.

## Keeping the docs current

`CLAUDE.md`/`README.md`/`docs/ideas.md` drift the moment a change lands without
touching them - this has already happened more than once (a shipped
`docs/ideas.md` item left listed as open brainstorm, a new package missing from
this file's own Architecture list, a new feature missing from README's
Features section). Treat updating whichever of these a change actually
affects as part of finishing that change, not a separate follow-up
someone has to remember to ask for:

- New `internal/` package, or a real architectural shift -> this file's
  Architecture section.
- New user-facing feature or Setup/env-var change -> `README.md`.
- A `docs/ideas.md` brainstorm item actually gets built -> mark it
  `~~struck~~` with a one-line pointer to where it landed (see existing
  examples in Planning annotations), don't just leave it listed as open.
- A product-direction decision actually gets made -> `docs/ideas.md`'s
  Vision/positioning or the relevant settled-thinking section.
- A change touches how often or how heavily this app calls PCO's API (a
  new hot-path call, a fix to one, a newly-discovered risk) ->
  `docs/pco-api-risks.md`, and this file's "PCO API calls" callout in
  Architecture if the reference pattern itself changes. Add new fixes to
  that doc's Changelog as a compact entry (what/why/how, a few lines), not
  a full narrative addendum - and if its "current state"/"live-verified
  facts" tables go stale relative to what you just shipped, update those
  too rather than only appending to the changelog. That doc was
  restructured once already (2026-09-04) specifically to stop it growing
  as an unbounded append-only history - don't let it drift back into that
  shape.
- Point-in-time design docs live under `docs/plans/`, sorted by their own
  status - `docs/plans/not-started/` (written up, nothing built yet),
  `docs/plans/in-progress/` (partially built), `docs/plans/shipped/`
  (fully landed). Moving a doc between these folders as work progresses is
  routine bookkeeping, but these docs stay in the tree once shipped rather
  than getting deleted - they're cited as the design-rationale reference
  from other docs' changelogs and from code comments throughout
  `internal/`, so treat every `docs/plans/**/*.md` reference the same way
  as any other code reference: if you rename or move a plan doc, `grep -r`
  for its old filename across the whole repo (`.md` and code comments
  both - a stale pointer here won't fail a build) and update every hit in
  the same change. When a plan doc moves into `docs/plans/shipped/`, add a
  `**Status: shipped.**` line at its top pointing at where it landed (see
  `docs/plans/shipped/pco-throttle-plan.md` for the convention), same
  spirit as marking a `docs/ideas.md` item struck.

## Architecture

**Layout**: `cmd/` has the entrypoints (`api` is the server; `dbreset`,
`dbseed`, `pcoreset` are the dev CLI tools above). `internal/` has the
actual app, split by concern rather than by feature:

- `internal/database` - connection setup + goose migrations, embedded from
  `internal/database/migrations/*.sql` and run automatically on every
  startup (`New()` calls `migrate()` - no separate migrate step).
- `internal/auth` - sessions, PCO OAuth, organizations, seat counting.
- `internal/billing` - Stripe checkout/portal/webhook handling.
- `internal/notify` - Mailgun-backed outbound email (currently just
  feedback submissions from `/feedback`) - a `Client` constructed once
  from env vars in `server.go`, same shape as `internal/billing`'s.
- `internal/scheduling` - the domain package: all local-database
  scheduling data (relationships, roles, preferences, sets, rosters,
  song-info cache) plus the algorithms built on top of live PCO data
  (repetition warnings, avoid-pairing conflicts, tag suggestions,
  first-song anchors). One `scheduling.Store` per concern-file
  (`song_relationships.go`, `person_relationships.go`, `sets.go`,
  `rosters.go`, ...), all sharing the same `*sql.DB` and struct.
- `internal/server` - Fiber app wiring: `server.go` (dependency
  construction), `routes.go` (route registration - read this first to find
  where a given endpoint lives), `crons.go` (gocron job registration),
  `controllers/`, `middleware/`, `jobs/`.
- `internal/views` (+ `internal/views/scheduling`) - templ view templates
  (`.templ` source, `_templ.go` generated output) rendered server-side and
  swapped into the page via htmx - there is no client-side framework/SPA.
  Structured multi-part UI patterns that need specific nested markup,
  ARIA, or JS hookup - not just a CSS class - are real templ components,
  not hand-rolled at each call site: cross-package ones (`Badge`,
  `ConflictBadge`, `Card`, `ImportBanner`, `ListItem`, `Disclosure`,
  `TableWrap`/`SortableTableWrap`, `PlanStatusBadge`, and the `Icon*` set)
  live in `internal/views/components`, a leaf package with no dependency
  on `views` or `views/scheduling`, so both can import it freely without
  an import cycle - mirroring `web/static/css/components/`'s split of
  reusable CSS from page-specific CSS; single-package ones
  (`quickActionRow`, `collapsibleCard`, `buildTabs`, the Build page's
  detail/publish card shells) stay unexported inside
  `internal/views/scheduling`. Purely
  single-concern CSS (typography, spacing, `.muted`, icon classes, plain
  button/badge modifiers) stays as bare classes - see `/style-guide`,
  which demonstrates the real component call for every pattern that has
  one.
- `web/static` - the in-house CSS design system (`tokens.css`, `base.css`,
  `components/*.css` one file per reusable component/utility group, plus a
  handful of page-specific files for everything single-page - see README's
  Tech stack section and `designSystemCSSFiles` in `internal/views/
  layout.templ` for the full, ordered file list). A new CSS file must be
  added to that list to actually load - forgetting isn't a build/test
  failure, just a silently unstyled component, so don't skip it. Also
  holds PWA assets (manifest, service worker, icons). Rendered live at
  `/style-guide`, which only responds when `APP_ENV=local`.

**Request pipeline** (`routes.go`): `RequestID` -> `Recover` run ahead of
everything else, unconditionally (no allowlist - every response, including
a rejected CORS preflight or a 404, gets an id and is panic-safe). Every
route past that runs through `RequireAuth` -> `RequireActiveOrganization`
-> `PopulateNavState`, in that order (each depends on locals the previous
one set) - see the doc comments on each in `internal/server/middleware/`
before touching this chain. `RequireAuth` resolves the session cookie into
`person_id`/`organization_id` locals *and* a PCO-authenticated context
(`pco_ctx`), refreshing the PCO access token first if it's near expiry.
Handlers should read these via the middleware package's accessors rather
than pulling locals directly: `middleware.CurrentPerson`,
`middleware.CurrentOrganization`, `middleware.CurrentOrganizationID`,
`middleware.RequestContext(c)` (use this instead of `c.Context()` any time
you're about to call into `go-pco`, so the call authenticates as the
signed-in person - it also carries this request's id, see next).

**Logging**: `internal/logger` wraps a package-level `slog.Logger`.
Anywhere a `context.Context` is already in scope (a handler via
`middleware.RequestContext(c)`, or a function that already takes one),
log through the `*Context` variants (`logger.ErrorContext(ctx, ...)`,
`InfoContext`/`WarnContext`/`DebugContext` likewise) rather than the plain
ones - they fold in a `request_id` field automatically when the context
carries one (`logger.WithRequestID`, or `RequestContext`'s own wrapping),
so a WARN/ERROR line can be traced back to the `X-Request-ID` response
header whoever hit the route can report. A goroutine a handler kicks off
and returns without waiting on (a background PCO sync, an outbound email)
needs its own `context.Background()`-derived context anyway (the request's
own is cancelled the moment the handler returns) - wrap that with
`logger.WithRequestID(context.Background(), middleware.CurrentRequestID(c))`
the same way `song_arrangement_cache.go`/`person_data_cache.go`/
`feedback.go` already do, so its own logging still carries the id. The
plain (non-`Context`) functions stay for genuinely request-less call sites
- cron jobs (`internal/server/jobs/`), CLI tools (`cmd/dbseed`, etc.),
and process startup/shutdown - there's nothing to correlate there.
`pco_observability.go`'s go-pco response hook (a single process-wide
callback, not a per-request handler) gets its request_id from
`info.Ctx` (go-pco v0.20.0+) - the exact context every `pco.*` call in
this app was made with - rather than from `middleware.RequestContext(c)`
directly, since the hook has no `fiber.Ctx` of its own to read one from.

**Organization scoping**: this is multi-tenant - every `scheduling.Store`
query that touches org-owned data takes an organization id
(`middleware.CurrentOrganizationID(c)`) and filters on it. A few tables
(e.g. `song_relationship_types`) are shared vocabulary, not per-org data,
and are deliberately *not* scoped - each store method's doc comment says
which. When adding a new scheduling table/method, match the existing
pattern in its file rather than guessing.

**PCO API calls: rate limits & performance** - this has already caused a
real production bug, not a hypothetical one (see `docs/pco-api-risks.md`'s
changelog for the original incident and every fix since). PCO's rate limit
is 100 requests/20 seconds, per user token. `docs/pco-api-risks.md` is the
living reference - current state of what's mitigated vs. still open,
live-verified facts about which PCO endpoints batch, and a compact
changelog of every fix and why it was shaped the way it was. Read it, and
update it, any time a change touches how often or how heavily this app
calls PCO - don't let it silently drift stale the way this file's own
"Keeping the docs current" section warns about for `docs/ideas.md`/
`README.md`.
`internal/scheduling/pco_songs.go`'s `FetchActiveSongs` is the reference
pattern for "PCO data that's expensive to fetch and doesn't change
mid-session": org-scoped, cached in-process for 60s (mirrors
`internal/server/middleware/nav.go`'s `cachedNavState`/`cacheEntryFresh`),
archived-filtered both server-side (`where[hidden]`) and client-side (the
client-side filter is the real correctness guarantee - never trust a
server-side filter alone), pagination driven by `Meta.TotalCount` rather
than a silent page cap, fanned out concurrently via
`scheduling.RunConcurrently`/`scheduling.PCOFanOutLimit` (bounded to 10
in-flight). Before adding any new PCO call inside a loop or a hot render
path (page load, htmx fragment re-render): can it batch instead of
calling once per item (`where[id]=...`, following
`personNameMapForIDs`'s precedent)? Can it fan out via
`scheduling.RunConcurrently` instead of running sequentially? Is what it's
fetching actually session-stable (safe to cache, like a song list) rather
than something that must stay live for correctness (never cache a live
availability/conflict check the way the song list gets cached - staleness
there is a real bug, not a cosmetic one)? The roster's own team/position
lists and team-member names (`fetchRosterSharedPCOData`,
`internal/server/controllers/rosters_shared_cache.go`) follow the same
cache-aside shape as `FetchActiveSongs`, just with a much shorter 20s TTL
- team membership can change *during* an active scheduling session, unlike
a song library - plus a manual "Refresh team data" button on the roster
page as the remedy for that shorter window's remaining staleness; see
`docs/pco-api-risks.md`'s Changelog for the full reasoning.

**Controllers**: grouped by page/feature under
`internal/server/controllers/` (`build.go` is the combined Songs/Team
build workflow, `sets.go`/`rosters.go` are the underlying draft data,
`scheduling_api.go` is a separate JSON API controller from the
htmx-fragment controllers). Handlers call `render(c, views.Whatever(...))`
to write a templ component as the response (`render.go`); htmx-driven
partial updates render a narrower component (e.g. just the results
fragment) rather than the whole page, so typing in a search box never
clobbers its own input mid-keystroke - see `BuildPlansSearch`'s doc comment
in `build.go` for the specific failure mode this avoids and match it in
similar handlers.

**Sync model (Publish/Discard)**: draft sets/rosters live only in this
app's database until Publish pushes them to real PCO plan items/records in
one sync; Discard reverts the local draft back to whatever's currently
live in PCO. The Songs tab also has a third, non-destructive direction -
`MergeSet` (`internal/server/controllers/sets_publish.go`) pulls in songs
added directly in Planning Center since the draft was last touched without
discarding any local change, unlike Discard's full replace. Changes here
(song notes, per-occurrence key, position assignments) are two kinds: some
(like `PersonTeamPositionAssignment.schedule_preference`) write straight
to PCO immediately since PCO has no "draft" concept for them; others stay
purely local until the next Publish/Refresh. Check how an existing similar
field behaves before adding a new one.

This is the highest-friction-bar code in the app, not incidental
plumbing - see docs/ideas.md's Vision/positioning section on why
PCO-duplicating actions (song key, order of service) are the whole
point, not something to skip because "PCO already does this." Any gap
between what this app lets someone set and what Publish actually pushes
means the user has to go clean it up by hand in PCO afterward, which
defeats the entire premise of planning here at all.

**Migrations**: goose, numbered sequentially
(`internal/database/migrations/NNNNN_description.sql`), embedded at build
time and applied automatically on startup - no manual migrate step in dev
or prod. Add a new migration by following the existing numbering/naming
pattern; don't edit an already-applied one.

**Tests**: standard `go test`, colocated with the code
(`internal/server/controllers/*_test.go`). Existing tests favor small
helper constructors (e.g. `foreignItem`, `songItem`, `setSong` in
`sets_test.go`) over fixture files or mocking frameworks - follow that
style for new tests in the same package.
