# Plan: WCAG 2.2 AA gaps found in a first full accessibility audit

Not built yet. Prompted by a first end-to-end WCAG 2.2 Level AA audit of
the app (layout, the shared `internal/views/components` set, the Build
page, the design-token color system, and the drag-reorder/htmx-swap
interactions) - nothing here was already tracked anywhere. The app is in
genuinely good shape on most of what was checked (see "What already
passes" below) - this is a short, real list, not a padded one.

## What already passes (checked, not flagged)

- **1.4.4/1.4.10 zoom/reflow**: `layout.templ`'s only viewport meta is
  `width=device-width, initial-scale=1` (`internal/views/layout.templ:77`)
  - no `maximum-scale`/`user-scalable=no` anywhere in the codebase blocking
    pinch-zoom.
- **1.4.3/1.4.11 text and UI contrast (almost everywhere)**: computed real
  contrast ratios from `tokens.css`'s actual HSL values (both themes) -
  body text, muted text, links/headings, badges (`.badge`, `.badge.published`,
  `.badge.status-pending`, `.badge.status-needed`, `.conflict-badge`,
  `.error`/`.success`), and the primary button (white on `--accent-solid`)
  all clear 4.5:1 (text) / 3:1 (UI) in both light and dark mode, several by
  a wide margin. The one real exception is form control borders - see
  Finding 5 below.
- **1.4.1 use of color**: every status/warning indicator (`ConflictBadge`,
  `PlanStatusBadge`, `.error`) pairs color with an icon/symbol or text
  label (`⚠`, "Planned"/"Pending"/"Todo", the error text itself) - nothing
  is color-only.
- **2.1.2/2.4.3/2.4.7 focus**: no page traps or hides focus outside a
  dialog; `input:focus-visible, select:focus-visible, textarea:focus-visible,
  button:focus-visible` all get a visible `--accent` outline
  (`web/static/css/components/forms.css:51-57`); no CSS anywhere removes
  the browser's default link outline. No sticky/fixed header or footer
  exists that could obscure a focused element (only the thin
  `#htmx-progress-bar`, `position: fixed`, is fixed - and only during an
  in-flight request).
- **Disclosure/expandable widgets (4.1.2, part of 1.3.1)**:
  `components.Disclosure` (`internal/views/components/disclosure.templ`)
  consistently sets `aria-haspopup`/`aria-expanded`, and every JS toggle
  (`toggleNavDropdown`, `toggleRowMenu`, the collapsible-card helpers)
  actually flips `aria-expanded` on open/close - confirmed in
  `scheduling-build.js`, `scheduling-global.js`, and
  `scheduling-collapsible.js`, not just hardcoded `"false"` in the
  template.
- **2.2.x / modal dialogs**: every dialog in the app (`genericHelpDialog`,
  the 8 setup-page help dialogs, the onboarding/build tour) is a native
  `<dialog>` opened via `.showModal()` and closed via `.close()`
  (`scheduling-global.js`'s `createTourController`/`openHelpDialog`) -
  native `<dialog>` gives focus trapping, Escape-to-dismiss, and
  return-of-focus to the invoking element for free; nothing here
  reimplements or fights that.
- **2.5.8 Target Size Minimum**: the design system enforces
  `--control-height: 2.25rem` (36px) as the shared min-height/min-width on
  every `input`/`select`/`textarea`/`button` and on `.icon-btn`
  specifically (`web/static/css/components/buttons.css:88-97`,
  `forms.css:36-49`) - comfortably clears the 24px minimum everywhere
  spot-checked (icon buttons, the mobile menu toggle, badge/chip close
  buttons).
- **3.2.6 Consistent Help**: the footer Help button and
  `genericHelpDialog` render unconditionally from `Layout` itself
  (`internal/views/layout.templ:179-185`) on every authenticated page, not
  per-page ad hoc - same place, same mechanism, everywhere.

## Finding 1 - 2.5.7 Dragging Movements (AA, new in 2.2) and 2.1.1 Keyboard (A)

**Highest real-user impact of anything found.** The order-of-service
reorder in the Build page's Songs tab is pointer-only, with no
alternative of any kind:

- The drag handle is a plain `<span class="drag-handle" title="Drag to
  reorder">` (`internal/views/scheduling/set_builder.templ:870`, `:1054`,
  `:1083`) - not a `<button>`, no `tabindex`, no `aria-label` (only a
  `title`, which isn't keyboard-reachable and isn't reliably announced).
- All reorder logic in `web/static/js/scheduling-build.js:927-1058` is
  built entirely on Pointer Events (`pointerdown`/`pointermove`/
  `pointerup`) scoped to that handle - there's no `keydown` handler
  anywhere in the file for arrow-key or other keyboard-driven reordering.
- The per-row "More actions" menu (`rowMenuItems`,
  `internal/views/scheduling/set_builder.templ:455-472`) has four actions
  (tag / relate / mark role / set vocal range) and no "Move up"/"Move
  down"/"Move to..." option. There is no reorder path anywhere else in the
  UI.

Net effect: a keyboard-only user cannot reorder the order of service at
all (2.1.1, Level A - stricter than the 2.5.7 gap alone), and a
touchscreen/switch-access/voice-control user has no single-pointer
alternative to the drag gesture (2.5.7, AA). This is the one finding in
this audit that blocks a task outright rather than degrading it.

**This needs a real design decision, not a default** - see Open questions.

## Finding 2 - 4.1.3 Status Messages (AA)

Systemic, not a one-off: outside of the two `plan-note-status` spans
(`internal/views/scheduling/build.templ:267,270`, which correctly use
`role="status"`/`role="alert"`), there is no `aria-live`, `role="status"`,
or `role="alert"` anywhere in `internal/views/**/*.templ` or
`web/static/js/**/*.js` (checked directly, not inferred). Every other
htmx swap region changes silently for a screen reader user:

- Search results: `#build-plans-results` (plan search,
  `internal/views/scheduling/build.templ:127,152`) and `#set-candidates`
  (song/candidate search, `set_builder.templ:719,1268`) both swap in new
  content on every keystroke/filter with nothing announcing it changed.
- Warnings/state: `#set-builder-body` and `#roster-builder-body`
  (`set_builder.templ:114,491,...`; `roster_builder.templ:289,311`) are
  the primary swap targets for basically every action on the Build page
  (add/remove/duplicate/reposition a song, assign/unassign a person) -
  none of it is announced either.
- Form errors: roughly 30 call sites render `<p class="error">{ errMsg
  }</p>` (or `<span class="error">`/`<li class="error">` - see
  `song_detail_card.templ:181`, `set_builder.templ:653,690,777`, every
  setup page) with no `role="alert"`. This is the highest-impact piece of
  this finding: `plan-note-status`'s own `role="alert"` two lines away
  from these (`build.templ:270`) shows the pattern already exists in this
  codebase, just wasn't propagated to the ~30 other error paragraphs.

**Mostly a mechanical fix** for the error-paragraph piece (`.error` is
already one shared CSS class from one shared visual pattern - adding
`role="alert"` to it, or to a small wrapper component, covers all ~30
sites in one change) and for wrapping the fixed swap-target divs
(`#set-builder-body`, `#roster-builder-body`, `#build-plans-results`) in
`aria-live="polite"`. The one real judgment call is the as-you-type
candidate/plan search (`#set-candidates`, `#build-plans-results` on
keystroke) - announcing every keystroke's result count could be more
noise than help; see Open questions.

## Finding 3 - 2.4.1 Bypass Blocks (A)

No skip-to-content link anywhere in `layout.templ`. Every page's header
(`internal/views/layout.templ:113-165`) puts the brand link, the mobile
menu toggle, "Home", "Build a Service", and the "Setup" dropdown ahead of
`<main>` in tab order, and the account dropdown further down - a
keyboard user re-tabs through all of it on every single page navigation
before reaching page content. `<main>` (`layout.templ:175`) is already a
real landmark, so a skip link has a real target to jump to.

**Trivial fix** - one `<a href="#main-content" class="skip-link">Skip to
content</a>` as the first element inside `<body>`, visually hidden until
focused (standard pattern - no design discussion needed), plus an
`id="main-content"` on the existing `<main>`.

## Finding 4 - 3.3.2 Labels or Instructions / 4.1.2 Name, Role, Value

Two search inputs are labeled by `placeholder` text alone, no `<label>`
or `aria-label`:

- `#build-plans-search` (`internal/views/scheduling/build.templ:141-151`)
  - `placeholder="Search by plan title, date, or note..."`.
- `#candidate-search` (`internal/views/scheduling/set_builder.templ:1261-1270`)
  - `placeholder="Search songs or tags..."`.

Both live in the shared `components/search.css` pattern, so this is the
same two-instance gap, not a one-off. Every other text input checked in
`internal/views/scheduling/*.templ` that uses `placeholder=` as an
example (`e.g. G, Bb, F#m`, `e.g. Grace, Romans 8:1...`) is correctly
wrapped in a real `<label>` alongside it - only these two bare search
boxes rely on placeholder as the only name.

**Trivial fix** - add `aria-label="Search plans"` /
`aria-label="Search songs and candidates"` (or a visually-hidden
`<label>`) to each; no layout change needed.

## Finding 5 - 1.4.11 Non-text Contrast (AA)

Form control borders fail the 3:1 minimum for UI component boundaries in
both themes. `--border` (`web/static/css/tokens.css:35`, light:
`hsl(218deg 28% 91%)`; dark, `tokens.css:134`/`156`:
`hsl(218deg 10% 22%)`) is what `input`/`select`/`textarea` borders use
(`web/static/css/components/forms.css:46`, `border: 1px solid
var(--border)`). Computed contrast:

| Pair | Ratio |
|---|---|
| light `--border` on `--bg` | 1.16:1 |
| light `--border` on `--surface` | 1.25:1 |
| dark `--border` on `--bg` | 1.46:1 |

All three are far under 3:1. Since form control backgrounds are
`var(--surface)` - typically the same or a very close color to whatever
they sit on (a card, the page background) - the border is, at rest, the
only visual cue that an input/select/textarea's *boundary* exists at all
(the focus-visible outline covers the focused state, not the resting
state SC 1.4.11 actually targets). This doesn't affect legibility of
label/placeholder text (already high-contrast, Finding-4 labeling aside)
but does affect a low-vision user's ability to see where a field starts
and ends before interacting with it.

**Needs a real design decision** (a new, more-contrasty border token, or
a different at-rest treatment for form controls specifically vs. the
`--border` token's other uses - card edges, table rows, dividers - which
aren't "UI components" under 1.4.11 and don't need to change) - see Open
questions. The mechanical part (swap one token reference) is trivial once
a value is picked; picking the value isn't.

## Open questions

- **Finding 1's reorder alternative - which shape?** Options seen
  elsewhere in this app's own conventions: (a) explicit "Move up"/"Move
  down" buttons added to `rowMenuItems` (mirrors the existing
  duplicate/remove/replace icon-button row - simplest to build, but
  doesn't cover "move to position N" in one step for a long service
  order); (b) making `.drag-handle` a real focusable `<button>` with
  arrow-key reordering while focused (closer to the drag metaphor,
  keyboard-only users get a `Home`/`documented`
  arrow-up/arrow-down/Enter-to-confirm idiom, but is more UI to design and
  test than two menu buttons); (c) both - buttons for a fast, obvious
  keyboard/switch path, keep the pointer drag as-is for mouse/touch. (c)
  best matches this app's own "reduce friction, don't hand the user a
  second thing to learn" standard (CLAUDE.md) since the row-menu pattern
  already exists and every row already has one - recommend it, but this
  is the user's call since it's new interaction surface, not a pure bug
  fix.
- **Finding 2's live-region verbosity for as-you-type search.**
  Announcing "12 results" on every keystroke of `#candidate-search` (300ms
  debounce already exists, `set_builder.templ:1268`) could be genuinely
  useful or genuinely annoying depending on phrasing/politeness level -
  worth trying `aria-live="polite"` with a short "N songs found" summary
  (not the raw result list re-read) rather than `aria-live` on the raw
  swap target directly, but this is a judgment call to confirm against
  the actual behavior, not something to guess blind.
- **Finding 5's replacement border value.** A straight lightness/
  saturation bump on `--border` to hit 3:1 against `--bg`/`--surface`
  changes every other place `--border` is used today (cards, tables,
  dividers - see `tokens.css`'s own token, used broadly) - possibly a
  fine, even improved, look everywhere, but needs an actual look at
  `/style-guide` with a candidate value before committing, not a blind
  swap. Alternative: a form-control-specific border token
  (`--control-border`) so this only touches inputs/selects/textareas,
  leaving cards/tables/dividers untouched - smaller blast radius, more
  tokens to maintain. Recommend trying the shared-token bump first (see
  what it does to the rest of the system at `/style-guide`) before adding
  a second token.

## Suggested phased approach

1. **Phase 1 - the trivial, no-discussion fixes** (Findings 3 and 4): skip
   link + `id="main-content"`; `aria-label` on the two search boxes. Small
   enough to be one `general-purpose (isolation: worktree)` build, or just
   a direct edit - self-contained, no design decision blocking it.
2. **Phase 2 - Finding 2's mechanical half**: `role="alert"` on the shared
   `.error` pattern (or a small wrapper component, if that reads better
   than a bare class-selector CSS/markup change) and `aria-live="polite"`
   on `#set-builder-body`/`#roster-builder-body`. Ship without waiting on
   the as-you-type search-announcement judgment call above - that's a
   separate, smaller follow-up once its own open question is answered.
3. **Phase 3 - Finding 1**: once the Open Questions answer picks a shape,
   this is a self-contained build - new `rowMenuItems` actions and/or
   keyboard handling in `scheduling-build.js`, hitting the same
   `RepositionSetSong` endpoint the pointer path already posts to. Good
   `general-purpose (isolation: worktree)` candidate once spec'd.
4. **Phase 4 - Finding 5**: pick the border value via `/style-guide`,
   then a one-token (or one-new-token) swap.

## Verification (once any phase is built)

- `go build ./...` (empty output = success) and `templ generate` if any
  `.templ` file changes.
- `make test`.
- Manual keyboard-only pass on the Build page's Songs tab (Finding 1):
  confirm a song can be moved to every position in the order without a
  mouse/touch pointer.
- Manual screen reader spot-check (VoiceOver or NVDA) on a candidate
  search and a failed form submit (Finding 2): confirm the result
  count/error is actually announced, not just visually present.
- Manual keyboard-only pass from a fresh page load (Finding 3): confirm
  the skip link is the first Tab stop and actually moves focus to
  `<main>`.
- `/style-guide` (APP_ENV=local) visual check of the new border token
  value (Finding 5) against every component that uses `--border` today,
  not just form inputs.
