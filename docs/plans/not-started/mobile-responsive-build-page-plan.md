# Plan: build page mobile & tabular responsiveness pass

Not built yet - written up for review before anyone picks it up. Prompted
by direct feedback in `docs/issues.md`: "Our mobile styles are generally
lacking, especially tabular pages, especially the build page."

## Current state (what mobile handling exists today)

There is no dedicated mobile stylesheet and no deliberate mobile design
pass for the Build page - what exists is incidental, not designed for
small viewports:

- `web/static/css/base.css` (~L42-46) has exactly one truly global mobile
  rule (bumps `main`'s padding at a 640px+ breakpoint) - nothing
  build-page-specific.
- `web/static/css/build-page.css` caps the page at `max-width: 90rem`
  (~L101-118, `main:has(.build-layout)`) and has one sidebar breakpoint
  (~L235-253) for the "at a glance" side panel - neither targets phone
  widths specifically.
- `web/static/css/builder-rows.css` has exactly one `@media (min-width:
  640px)` rule, widening `#candidate-search` (~L104) - the only other
  responsive rule touching Build-page-specific markup.
- `web/static/css/components/table.css` (~L9-23): `.table-wrap {
  overflow-x: auto }`, `table { min-width: 32rem }` - the Songs tab's
  order-of-service table forces horizontal scrolling below roughly a
  512px viewport, with no stacked/card fallback.
- `web/static/css/components/card.css`'s `.team-groups-grid` (~L42-47,
  `grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr))`)
  naturally collapses to one column once the viewport is narrower than
  ~20rem + gap, so the Team tab's outer layout already degrades
  reasonably on a phone - but the *density inside* each card
  (`.team-group .position-group`, ~L122-136: zero horizontal padding,
  border-only row dividers, compact icon-button clusters for actions/drag
  handle) has never had a touch-target sizing pass.

Net: the Team tab's outer grid mostly "works" narrow by accident of
`auto-fit`; the Songs tab's table has no narrow-viewport story at all
beyond raw horizontal scroll; nothing anywhere has been checked against
real touch-target sizing.

## Design forks needing a real answer (not a default)

1. **Table strategy for narrow viewports** - three real options:
   - **(a) Keep horizontal scroll, improve the affordance** (a
     scroll-shadow/gradient hint at the table's edges, a visibly larger
     touch scrollbar) - cheapest, zero markup change, purely
     `table.css`/`build-page.css`.
   - **(b) Collapse to a stacked "card per row" layout** below a
     breakpoint (each row's cells become labeled stacked lines) - the
     standard responsive-table pattern, but needs either duplicated
     markup or a `data-label`-per-cell + CSS approach across every
     `TableWrap`/`SortableTableWrap` consumer, a real templ + CSS change,
     not CSS-only.
   - **(c) Hide less-critical columns below a breakpoint**, revealed via
     tap - least markup-disruptive, but needs a per-table decision on
     which columns are actually droppable (the order-of-service table's
     key/length columns are not obviously droppable the way a
     nice-to-have column elsewhere might be).
   Recommend (a) first - matches this project's own "ship the cheap thing
   first" sequencing precedent - and only reach for (b)/(c) if that isn't
   enough once it's live.
2. **Touch-target sizing** - the icon-button clusters
   (`.set-song-actions`, the drag handle, position-group action rows)
   need an audit against a real minimum tap-target size (44px is the
   usual accessibility baseline). Worth checking first whether
   `docs/plans/shipped/wcag-aa-audit-plan.md` already covered touch-target
   sizing as part of its own audit, or was scoped to
   contrast/screen-reader concerns only - don't duplicate work sight
   unseen.
3. **Scope** - the issue says "tabular pages... especially the build
   page." Other `SortableTableWrap`/`TableWrap` consumers exist elsewhere
   in the app and would inherit any shared `table.css` fix for free once
   that rule changes, but the Build page's own density
   (`team-groups-grid`, `position-group` padding, candidate search/filter
   rows) is unique to it and needs its own pass regardless. Worth
   confirming with the user whether "tabular pages" means literally every
   table in the app or just calls out the Build page as the worst
   offender.

## Sketch

- **Phase 1** (small, CSS-mostly): the scroll-affordance improvement from
  fork 1, a touch-target audit/fix pass on Build-page icon-button
  clusters, and a light spacing pass on `.team-groups-grid`/
  `.position-group` for narrow widths. Contained to
  `build-page.css`/`table.css`/`card.css`/`builder-rows.css`; verify at
  real phone widths (375px/390px/414px) via browser devtools, both tabs,
  both themes.
- **Phase 2** (larger, only if Phase 1 isn't enough): the real stacked-
  table pattern (fork 1's option (b)) for the order-of-service table
  specifically - needs a markup change, not just CSS. Worth deferring
  until Phase 1's shipped and felt insufficient in practice, same
  reasoning CLAUDE.md and other plans in this repo already apply
  elsewhere.

## Open questions

- Whether `docs/plans/shipped/wcag-aa-audit-plan.md` already covers
  touch-target sizing (check before duplicating).
- Whether "tabular pages" scope is Build-page-only or app-wide (fork 3) -
  worth confirming before finalizing Phase 1's file list.

## Suggested delegation

Once forks 1 and 3 are settled, Phase 1 is a reasonable
`general-purpose (isolation: worktree)` build per CLAUDE.md's delegation
guidance - self-contained CSS/markup-light work across a small, known
file set, nothing backend-touching.

## Verification (once built)

- `go build ./...` (only relevant if any `.templ` markup changes; `templ
  generate` first if so), `make test`.
- Live: check both Songs and Team tabs at 375px/390px/414px (common phone
  widths) and 768px (tablet), light and dark theme, via browser devtools'
  device toolbar.
