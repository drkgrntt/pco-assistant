# Plan: mobile & tabular responsiveness pass

**Status: shipped** (Phase 1, 2026-09-14; a same-day follow-up fixing
page-level overflow and Build-page row density, also 2026-09-14 - see its
own section below). Phase 2 (a real stacked-table markup pattern) remains
an explicit, non-committed follow-up.

Originally written up scoped to the Build page only, prompted by direct
feedback in `docs/issues.md`: "Our mobile styles are generally lacking,
especially tabular pages, especially the build page." Broadened to
app-wide before Phase 1 was built, on direct instruction to also nail down
the 8 setup pages (`/scheduling/song-relationships`, `song-roles`,
`song-tags`, `song-contexts`, `person-relationships`, `vocal-range`,
`song-vocal-range`, `position-preferences`) and confirm the app is
genuinely mobile-first the way `docs/ideas.md`'s "narrow/compact
side-by-side layout" item already assumed it to be.

## Current state (research pass, pre-build)

There was no dedicated mobile stylesheet and no single deliberate mobile
design pass - but contrary to this doc's original "Current state" section
(now stale, corrected here), most of the pieces were already in decent
shape by the time this was picked back up:

- **Nav/chrome**: `header-nav.css` is already mobile-first (a hamburger
  menu below 640px, expanding to the full header at 640px+); `base.css`
  bumps `main`'s padding at the same 640px breakpoint. The viewport meta
  tag (`width=device-width, initial-scale=1`) is in place
  (`layout.templ`). No changes needed here.
- **Setup-page forms**: `components/forms.css`'s `form.inline` was already
  built mobile-first - fields stack full-width below 640px, flow into an
  inline row above it. Every one of the 8 setup pages' create/edit forms
  uses this same class, so this was already solid; confirmed by reading
  rather than assumed.
- **Setup-page list layouts**: three shapes exist, not one:
  - `song-roles`/`song-tags`/`song-contexts` use a card-per-item grid
    (`.song-roles-list`/`.song-tags-list`/`.song-contexts-list`) that's
    already single-column below 768px, two-column above it - no table
    involved, already responsive.
  - `relationships`/`preferences`/`person-relationships`/`vocal-range`/
    `song-vocal-range`/`position-preferences` (6 of the 8) use
    `components.SortableTableWrap`/`TableWrap` - a real table, 3-5 columns
    each, all sharing `table.css`'s one `.table-wrap { overflow-x: auto }`
    / `table { min-width: 32rem }` rule. This is where the actual gap was:
    horizontal scroll with no visual cue that more columns exist
    off-screen, on every one of these six pages *and* the Build page's own
    Songs-tab order-of-service table (same shared component).
- **Build page's Team tab density** (`.team-groups-grid`/
  `.position-group` in `card.css`) - flagged in this doc's original
  research pass as "zero horizontal padding, needs a touch-target sizing
  pass." Already fixed as unrelated general-polish work
  (`1d60bf2`/`bfcb767`, 2026-09-12/14, from `docs/issues.md`'s "scrunched"
  feedback) before this doc was picked back up: `.team-group
  .position-group` now has real padding (`var(--space-3) var(--space-2)`)
  and the grid itself switched from CSS Grid to flexbox so a lone/last
  card fills its row. Nothing left to do here for this pass.
- **Touch-target sizing** - `--control-height: 2.25rem` (36px) is the
  deliberate, documented floor for every form control/button/icon-button
  app-wide (tokens.css). 36px clears WCAG 2.2's 2.5.8 Target Size Minimum
  (24x24 CSS px, Level AA) with real margin; it's short of 2.5.5's 44x44px
  (Level AAA, optional). Audited and left as-is - bumping the shared
  token to 44px would be a visual redesign of every button/input in the
  app, not a mobile-specific fix, and nothing here requires AAA. Worth
  reopening only if AAA touch targets become a real goal in their own
  right, not bundled into this pass.

## Design forks (resolved)

1. **Table strategy for narrow viewports** - three options were on the
   table originally:
   - **(a) Keep horizontal scroll, improve the affordance** - cheapest,
     CSS/JS-only, no markup change.
   - **(b) Collapse to a stacked "card per row" layout** below a
     breakpoint - the standard responsive-table pattern, needs a real
     templ + CSS change across every `TableWrap`/`SortableTableWrap`
     consumer.
   - **(c) Hide less-critical columns below a breakpoint** - needs a
     per-table decision on which columns are droppable.
   **Went with (a)**, per the original recommendation - matches this
   project's "ship the cheap thing first" precedent, and every table here
   is a modest 3-5 columns (not a wide data grid), so scroll-with-a-cue is
   a reasonable resting state rather than a stopgap.
2. **Touch-target sizing** - resolved above: already compliant at the
   Level AA minimum, deliberately not pushed to AAA.
3. **Scope** - resolved as app-wide (setup pages *and* Build), per direct
   instruction. In practice this cost nothing extra: the fix lives in
   `table.css`/`scheduling-global.js`, both already shared by every
   `TableWrap`/`SortableTableWrap` consumer app-wide, so fixing it once
   fixed all 6 table-based setup pages and the Build page's Songs tab in
   the same change.

## What shipped (Phase 1)

- `web/static/css/components/table.css`: `.table-wrap.can-scroll-left`/
  `.can-scroll-right` - an inset `box-shadow` on the wrap's own border box
  (not a child element or background-gradient trick), so it stays fixed at
  the visible edge regardless of scroll position and naturally respects
  the wrap's existing `border-radius`.
- `web/static/js/scheduling-global.js`: `wireTableScrollAffordance` -
  toggles those two classes per `.table-wrap` based on its own
  `scrollLeft`/`scrollWidth`, wired on `DOMContentLoaded`, `htmx:afterSwap`
  (every setup-page table gets replaced wholesale via `outerHTML` on
  add/edit/delete, same as `scheduling-sortable-table.js`'s existing
  `wireSortableTables` has to handle), and `window resize` (a wrap's own
  scrollability can flip at a breakpoint, e.g. rotating a phone to
  landscape).
- No templ/markup changes, no touch-target changes, no Team-tab CSS
  changes - all three turned out to already be either fine or already
  fixed by other work, per the research above.

### Verification

- `templ generate` (no `.templ` changes, ran anyway for parity) + `go
  build ./...` - clean.
- `make test` - full suite passes.
- Live device-width check (375/390/414px phone widths, 768px tablet,
  light+dark, both a setup-page table and the Build page's Songs tab) is
  still worth doing against a running dev server before calling this
  fully verified - not done as part of this change per this repo's own
  rule against touching the user's local dev server; left for a manual
  spot-check.

## Follow-up (same day): page-level overflow + Build-page row density

Reported right after Phase 1 shipped: tables were still forcing the whole
*page* to scroll horizontally, not just the table within its own box - a
real bug, not a request for more polish - plus the Build page's
order-of-service row specifically was cramped enough (tags wrapping
across several lines, up to 4 action icons crowding the actions column)
that Phase 1's scroll-shadow affordance alone didn't make it feel right at
phone width.

**Root cause (page-level overflow), first pass**: `.table-wrap`'s
`overflow-x: auto` correctly contains the table's own `min-width: 32rem`
on plain block-flow pages (every setup page), but on the Build page
`.table-wrap` sits inside `.build-main`, a flex item several levels up
(`build-page.css`) - a flex/grid item's automatic minimum size can let a
descendant's intrinsic/min-content width bubble up and widen the item
past its own container unless every link in that chain explicitly opts
out. Fixed with `.table-wrap { max-width: 100%; }` (`table.css`, kept -
still correct and still needed) - the same fix one level closer to the
actual table. Backed, at the time, by a global `html, body { overflow-x:
hidden; }` safety net (`base.css`).

**That safety net was itself a regression, reported immediately after**:
setting `overflow-x: hidden` on *both* `html` and `body` together is a
known trap - on some browsers (mobile Safari in particular) it can break
the page's own vertical scrolling outright, not just clip the intended
horizontal overflow. Reported as "now the entire page won't scroll" on
the Song Relationships setup page specifically - a page with no flex
ancestor around its table at all (`main` -> a plain block `div` ->
`.table-wrap` -> `table`), which is what exposed that the *real* source
of the width problem was never fully diagnosed the first time around:
`main` itself is a flex item of `body` (the sticky-footer column,
`base.css`) and, like `.build-main`/`.build-glance` one level down
(`build-page.css`, `min-width: 0`, same reasoning), had no `min-width: 0`
of its own - so a wide-enough table could inflate `main` itself past the
viewport before `.table-wrap`'s own `overflow-x: auto` ever got a sane
100% to work with. **Corrected fix, second pass**: removed the
`overflow-x: hidden` safety net entirely (reverted, `base.css`) and added
`main { min-width: 0; }` (`base.css`) instead - the same textbook
flex-item fix already proven at `.build-main`/`.build-glance`, applied to
the one ancestor that was actually missing it, on every page rather than
just the Build page. `<dialog>`'s top-layer rendering (help/tour dialogs)
was never relevant to this - not touched either way.

**Still not enough, reported again**: `main`'s own `min-width: 0` closed
the upstream flex-item gap, but `.table-wrap`'s `max-width: 100%` itself
turned out to never have been doing anything in the first place, on
either pass. `max-width` is a *clamp* - it only ever pulls a width down
from whatever the box's own width calculation already produced. On a
plain block box (every setup page's `.table-wrap`), that calculation is
already "fill the containing block, no more" (`width: auto`'s own
definition) - `max-width: 100%` there is provably a no-op, since the
tentative width it would clamp is already numerically identical to what
it's clamping *to*. **Third, actual fix**: `.table-wrap` now sets
`width: 100%; min-width: 0;` explicitly, alongside `max-width: 100%`
(kept, harmless, redundant given `width: 100%` but doesn't hurt) - not
because the percentage math was wrong, but because relying on `width:
auto`'s default fill-the-container behavior turned out to be exactly the
assumption that kept not holding further up an ancestor chain not every
page's table sits in the same way (the Build page's `.table-wrap` *is* a
flex item, several levels inside `.build-main` - `min-width: 0` matters
there for the same reason it mattered for `main` itself). Both properties
now sit directly on the one element every table-bearing page shares,
rather than trusting each ancestor up the chain to have remembered its
own opt-out.

**Still not enough, reported a third time - the actual root cause,
fourth pass**: correctly called out as still broken - "it still does
nothing because its container has no set width." Right diagnosis: `main`
had `max-width: 60rem; margin: 0 auto;` but no `width` of its own, and
that specific combination is the bug, not `main`'s `min-width: 0` from
the previous pass (which was necessary but not sufficient). `main` is a
flex item of `body`; `align-items`' default `stretch` is what would
normally give a width-less flex item a definite cross-axis size equal to
its container's - but per the flexbox spec, *auto cross-axis margins
opt a flex item out of stretch*, and `margin: 0 auto` is exactly that.
With stretch defeated and no explicit `width`, `main` fell back to
shrink-to-fit sizing - computed from its own children's intrinsic width,
the reverse of what any percentage-width descendant needs handed down to
it. A shrink-to-fit box has no definite width to publish top-down; per
CSS 10.3.7 a percentage-width child inside one doesn't resolve against
"whatever width the parent eventually gets," it resolves against
nothing. That's why every previous round of `.table-wrap` percentage
tweaks (`max-width: 100%`, then `width: 100%`) kept doing nothing no
matter how it was worded - the bug was never in `table.css` at all, it
was one level up in `base.css`, the entire time. **Fourth, actual fix**:
`main` now sets `width: 100%;` explicitly, alongside its existing
`max-width: 60rem; min-width: 0; margin: 0 auto;` - the standard
"centered column" idiom (`width: 100%` gives a definite width to clamp
and hand down; `max-width` clamps it; `margin: 0 auto` recentres using
the post-clamp width), rather than relying on flexbox stretch, which
turned out to be silently defeated by the very margin rule that was
supposed to center it. `.table-wrap`'s own `width: 100%; min-width: 0;`
(third pass) stays as-is and now has an actual definite width to resolve
against.

**Build-page row density (order-of-service table)**: two consolidations,
both CSS-gated at the same 640px breakpoint everything else in this app
already uses, both additive (a second, parallel rendering shown/hidden by
CSS) rather than moving/rearranging the existing desktop markup, so
desktop behavior is unchanged:

- **Actions column**: a new `rowMobileActionsToggle`/`rowMobileActionsItems`
  Disclosure (`set_builder.templ`) folds Duplicate, Remove, Replace (reusing
  `replaceMenuItems` directly), and everything the existing "More
  actions" menu already covers (tag/relate/role/vocal-range/move
  up/move down) into one "Actions" kebab - down from up to 4 icon
  buttons to exactly 1 at phone width. Precedent: `song_detail_card.templ`'s
  `songDetailMenuItems` already folds Replace into one combined menu for
  its own Duplicate/Remove/More-actions cluster (see its own doc
  comment) - this is the same fold, gated to mobile only via
  `.set-song-actions-mobile` (`builder-rows.css`) since the table row,
  unlike the sidebar card, does have the room for separate one-click
  desktop buttons and shouldn't lose them.
- **Tags column**: a song row whose "extras" (roles, resurfaced
  transition-note prompts) are under `songBadgeSpans`' own
  `badgeCollapseThreshold` renders them directly on desktop (unchanged)
  but behind the same `+N more` popover pattern on mobile
  (`.badge-extras-desktop-only`/`.badge-extras-mobile-only`,
  `builder-rows.css`) - a row a desktop user wouldn't call "busy" can
  still wrap across several lines at phone width. Deliberately does not
  touch the always-visible status/warning badges (published/moved/
  conflict/repeat/key-transition/vocal-range-mismatch) - those stay
  directly visible on every viewport, same as before; hiding an actual
  warning behind a tap would defeat the point of it, and in practice
  they're each capped at one badge, so they were never what made a row
  feel busy (see `songBadgeSpans`' own pre-existing doc comment).

Both use the "duplicate content, let CSS pick" approach rather than
Go-side viewport detection (Go has no media query to evaluate) or
JS-driven DOM surgery (moving nodes between containers, harder to verify
correct without a live device check) - the same tradeoff
`song_detail_card.templ`'s own doc comment already made for its
Duplicate/Remove/Replace overlap with the row, just applied to two more
spots. `songCandidateMeta` (the "Browse all songs" candidate list) has
the identical busy-row collapse and could get the same mobile treatment
later if it turns out to need it - not done here, since it wasn't the
one flagged.

### Verification

- `templ generate` + `go build ./...` + `go vet ./...` - clean. `make
  test` - full suite passes.
- Live device-width check is still the open item from Phase 1's own
  verification note, now covering this follow-up too - not done as part
  of this change per this repo's rule against touching the user's local
  dev server.

## Second follow-up (same day): "Find songs to add" filter chips on mobile

Reported after the page-overflow fix above landed: the Build page's role/
context/tag filter chips (`.role-filters`/`.context-filter`,
`set_builder.templ`) render as full bordered `<button>`s (forms.css's base
control size, ~36px tall) and read as busy at phone width once role
filters, an optional context row, and however many tag chips a
`collapsibleSection` reveals are all wrapping in the same narrow column.

**Fix**: a mobile-only (`max-width: 639px`) rule in `builder-rows.css`
restyles `.role-filter`/`.context-filter` down to a `.tag-badge`-style
muted pill - no border, `color-mix`-tinted background, smaller padding/
font-size, `min-height: 1.5rem` (24px - still clears WCAG 2.2's 2.5.8
Target Size Minimum, the same AA-not-AAA call already made for
`--control-height` itself, see this doc's Design forks section). One rule
covers all three filter types with no markup change, since tag chips
already reuse `.role-filter` directly (`applyTagFilter`,
`set_builder.templ`) rather than having a class of their own. The
existing `.role-filter.active`/`.context-filter.active` accent-fill rule
needed no change - two classes outrank one in the cascade regardless of
source order, so a selected filter still shows solid-accent exactly as
before, just at the new smaller size. Desktop is untouched.

### Verification

- `templ generate` + `go build ./...` + `go vet ./...` - clean (CSS-only
  change; no Go/templ files touched).
- Live device-width check not done, same standing constraint as above.

## Phase 2 (not committed - only if Phase 1 turns out insufficient)

The real stacked-"card per row" pattern (fork 1's option (b)) for a
specific table, if scroll-with-a-cue doesn't feel like enough once it's
been live a while. Needs a markup change (duplicated per-row markup, or a
`data-label`-per-cell + CSS approach), not just CSS/JS - a real templ
change, not a drop-in. No table has been flagged as needing this yet;
revisit only if real usage says otherwise.
