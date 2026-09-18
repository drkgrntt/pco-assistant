# Plan: multi-week matrix view (people x dates, cross-plan)

Not built yet - written up for review before anyone picks it up. Prompted
by direct feedback from a worship pastor: PCO's own matrix (Services > a
team > Schedule > Matrix - a grid of people down one axis, upcoming plan
dates across the other, cells showing scheduled/declined/needs-response
per position) is apparently where he basically lives day to day, and
nothing in this app today gives that season's-eye view - the Team Builder
(`internal/views/scheduling/roster_builder.templ`) is deliberately scoped
to one plan at a time. Full brainstorm context lives in `docs/ideas.md`'s
"Multi-week matrix view (people x dates, cross-plan)" section - this doc
picks up from there and works out the cost model and loading design in
enough detail to hand to a builder.

## Which build, and which True North promise it serves

`docs/ideas.md` already distinguishes two very different builds hiding
under one name:

- **A bare clone** (who's scheduled where, across N weeks) would just
  re-render data PCO already shows better, natively, live-updating - pure
  navigation convenience, not a real win. Not what this plan builds.
- **An annotated matrix** - the same grid, but with this app's own
  signals layered onto each cell/row instead of PCO's plain
  scheduled/declined status (a frequency-preference violation, a blockout
  conflict, a same-week cross-team commitment, an avoid-pairing conflict,
  and eventually a thin-bench position) - is the real, unduped win. A
  season laid out at a glance surfaces burnout/friction patterns invisible
  one week at a time (**protects the team**) and staffing gaps with lead
  time to fix them instead of the week of (**protects the service**).
  This is the wedge PCO's own matrix structurally can't have, since it has
  no idea any of this app's relationship/preference data exists. **This
  plan builds this version.**

Distinct from two existing ideas worth not conflating (see `docs/ideas.md`
for the full callout): the **vocal range/key matrix**
(`vocal_range_matrix.go`, shipped 2026-09-12) is a person x song grid
about vocal fit, unrelated axis, same word by coincidence. The **browser
extension HUD** (also in `docs/ideas.md`, under "Reducing friction vs.
Planning Center") is an alternate delivery mechanism for this same
idea - injecting these same annotations directly onto PCO's own live
matrix UI instead of building a native page here - flagged there as
higher-ceiling but higher-effort, gated on adoption data from lighter
integrations first. This plan proceeds with a native page; revisit the
HUD alternative only if that gating condition is later revisited.

## Cost model

This is likely the single most expensive read-only page this app would
build, so the loading design has to be right from the start rather than
retrofitted under load the way the single-plan roster builder was
(`docs/pco-api-risks.md`'s changelog: ~100-200 sequential calls per load
before mitigation). The good news: several of that page's fixes already
exist and this page can ride them for free rather than re-solving them.

**Already free, no new work needed:**

- **Songs** - `FetchActiveSongs` (`internal/scheduling/pco_songs.go`)
  already caches the org's entire active library for 60s. The matrix
  should read that cache and filter locally to whatever songs the
  window's plans reference - no `where[id]` songs call needed at all, and
  no per-plan song fetch either.
- **Person names** - `personNameMapForIDs` already does exactly the
  "bundle ids into one call" pattern: collect the deduped union of person
  ids across every plan in the window first, then one batched
  `where[id]=...` call (100/call, go-pco `v0.15.0`+). The matrix just
  needs to feed it the whole window's id set at once, not call it
  per-plan.
- **Blockout / frequency / cross-team checks** - the Team Builder's own
  version of this exact problem (per-person, live call every render) was
  already fixed with a local Postgres cache (migration `00033`) kept warm
  by an hourly background cron (`jobs.PCOJobs`), reusing the same paced
  sync entry points (`triggerSongInfoQueueSync`,
  `triggerPersonDataSyncCtx`) an ordinary render already triggers. The
  matrix should read through that same cache rather than re-fetching live
  per cell - for anyone whose cache is already warm, this axis costs
  **zero additional PCO calls** at render time. **Note the one real PCO
  constraint underneath this**: blockouts (`services/v2/people/{id}/
  blockouts`) have no batch/`where[id]` option at all (`can_query_by` is
  just `group_identifier`, confirmed live) - that's a hard PCO-side floor
  on the *background sync's* own cost, not something this page's design
  can bundle away; it's exactly why that data has to be cached rather than
  fetched live in the first place.

**Not bundleable - scales with weeks shown, not people shown:**

- `GetItems` (one per plan, to know which songs/occurrences exist) and the
  roster shared-fetch (`GetNeededPositions`+`GetTeamMembers`, already
  collapsed to 2 calls/plan via `include=team` - see
  `docs/pco-api-risks.md`) are both PCO endpoints scoped per plan, with no
  cross-plan batch equivalent to `where[id]`. ~3 calls/plan is the real,
  unavoidable floor for the "which songs/who's scheduled" axis.

**Net for a bounded window**: ~1 (`GetPlans` for the window, one call,
`PerPage` bumped rather than paginated - same pattern as the home page's
look-ahead) + 3/plan x N plans (items + roster shared-fetch) + 1 (batched
person names) + ~0 (songs, cached) + ~0 (blockout/frequency/cross-team,
cached) calls. For an 8-week/~40-person window that's roughly **~26
calls** - comfortably under PCO's 100-requests/20s budget in one render.

**Why boundless/large-bounded scope is a different problem**: the ~3
calls/plan floor is linear in *weeks*, not *people*, so it doesn't get
cheaper from any of the caching above. A full season (26-52 plans) is
80-150+ calls in one render - already at or over budget - and a truly
boundless view is unbounded by definition. This is the part that needs a
loading design, not a bundling trick; see below.

## Loading design for large/boundless scope

**Decision: scroll-triggered incremental loading**, not a fixed
bounded-window build. Reuses an idiom this app already has, rather than
introducing a new one - `set_builder.templ`/`build_tabs.templ` already
use `hx-trigger="load once"`/`"click once"` for exactly this
"don't fetch until it's actually needed" shape on the Build page tabs and
song popovers; htmx `2.0.10` (vendored, `web/static/js/htmx.min.js`)
still ships the built-in `revealed` trigger, so this extends to
`hx-trigger="revealed once"` on a sentinel row/column at the grid's edge
rather than trying to trigger off real content rows.

**Reuse existing windowing/caching rather than building a second one:**

- **Backward (into history)**: `fetchPastPlansCached`/
  `fetchPastPlansWindowed` (`build_past_plans_cache.go`) already solved
  "don't walk a service type's entire plan history at once" - windowed
  default fetch, cached per (organization, service type) with a 30min
  TTL, early-exit paging, with a documented fallback for a search that
  misses the window. The matrix should ride this same cache for
  historical weeks rather than building a second windowing mechanism.
- **Forward (into the future)**: closer to the home page's own
  look-ahead pattern (`homeLookAheadPlansCap`, rides the existing
  `GetPlans` call with `PerPage` bumped, not a second call) than to the
  past-plans cache, since future plans are a much shorter, cheaper list
  to begin with.

**Each scroll-triggered increment is its own bounded fan-out** - a fixed
number of additional weeks per reveal (matching `PCOFanOutLimit`'s
existing "bound it, don't let it grow with data size" precedent), not the
whole remaining history/future in one shot.

## Open questions

- **Scroll direction(s) actually needed** - forward only (future weeks,
  the more common "season planning ahead" use case), backward only
  (reviewing history), or both? Confirm with the user before scoping the
  fetch logic for each - they're two different code paths (look-ahead vs.
  `fetchPastPlansCached`) with different existing precedent to reuse.
- **Increment size** - how many additional weeks does one scroll-reveal
  load? Needs to be large enough that scrolling doesn't feel like
  constant loading-spinner churn, small enough that a fast scroll (or a
  bot/prefetch) can't accidentally walk the whole history in one burst.
- **Cold-cache cell behavior** - what does a cell render as when a
  person's local blockout/frequency/cross-team cache (migration `00033`)
  isn't warm yet for a given date? Options: a "pending" state that fills
  in once the background sync catches up (no live PCO call triggered by
  viewing the matrix), vs. some bounded live top-up fetch scoped to just
  the newly-revealed people/weeks. Recommend the pending-state option -
  triggering a live fetch from a scroll-reveal reintroduces exactly the
  per-render live-call risk the migration `00033` cache was built to
  eliminate.
- **HUD alternative** - still open per `docs/ideas.md`, gated on adoption
  data from lighter integrations first. Not re-litigated here; flagged so
  it isn't silently forgotten if that gating condition changes before this
  is built.

## Suggested build order

1. Settle the open questions above with the user.
2. Bounded-window version first (fixed 6-8 week default, no scroll
   loading yet) - proves the annotated-cell rendering and the cache-reuse
   plumbing (songs/person-names/blockout-frequency-cross-team) against a
   real dev org before the loading mechanism adds its own complexity.
3. Add scroll-triggered incremental loading in the confirmed
   direction(s), riding `fetchPastPlansCached`/the look-ahead pattern per
   direction as designed above.
4. Cell-level disclosure UI (compact cell by default, full warning detail
   on click) following the existing `Disclosure`/`collapsibleCard`
   pattern (`internal/views/components`) rather than rendering every
   warning type inline unconditionally.

## Verification (once built)

- `go build ./...` (empty output = success), `make test`.
- Live, against a real multi-team dev org: confirm the bounded-window
  version's actual PCO call count roughly matches the ~26-call estimate
  above (check via the response hook's logging or a manual count), before
  trusting the estimate at larger scroll-loaded scope.
- Confirm a cold-cache person's cells show the pending state rather than
  triggering a live PCO call, then confirm they backfill correctly once
  the hourly `jobs.PCOJobs` cron (or a manual trigger, in dev) completes a
  sync pass.
- Confirm scroll-revealing additional weeks doesn't reload/refetch weeks
  already on screen (the sentinel-based `revealed once` trigger should
  only ever fire once per sentinel).
