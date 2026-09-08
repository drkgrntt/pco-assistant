# Plan: Help system + header/nav consolidation

**Status: built 2026-09-08** (not yet deployed).

Derek's ask, right after
`docs/plans/in-progress/onboarding-and-ambient-framing-plan.md`'s Phase 4/5
tours shipped: a persistent, on-demand **Help** button that opens whichever
tour belongs to the current page - never auto-opening, only on click - plus
consolidating the header's loose theme-toggle/Account/Feedback/logout row
into a single dropdown, and a page footer to put Help (and Feedback)
somewhere. Two decisions Derek made explicitly when asked, both load-bearing
for what follows:

- **Help lives in the new footer, not the header** - even though that means
  scrolling to reach it on a long page, not a one-click header affordance.
- **The 8 individual setup pages get real per-page help content**, not a
  generic fallback - reversing that onboarding plan's own "Proposed, not
  started" recommendation to skip per-setup-page guidance as too
  "overbearing." See "Why the setup-page recommendation flipped" below for
  why a different trigger mechanism changes that calculus.

## What changed

**Header** (`internal/views/layout.templ`): `.site-actions` used to be a
flat row - a theme-toggle button, `<a href="/account">`, `<a
href="/feedback">`, an `.account` div stacking org name over person name,
and a logout `<form>`. It's now one `components.Disclosure` - literally the
same `"nav-dropdown"`/`"nav-dropdown-toggle"`/`toggleNavDropdown(this)`
instance the nav's "Setup" dropdown already used, not a new mechanism (see
"Why the account dropdown reuses `.nav-dropdown` verbatim" below). Toggle
button shows the person's name + `IconChevronDown`; the popup holds the
theme toggle (moved in unchanged), the org name (when known, still
`.account-org`), the `/account` link, and the logout form, in that order.
Feedback moved out entirely, into the new footer.

**Footer** (`internal/views/layout.templ`, new `.site-footer` in
`header-nav.css`): a `<footer>` after `<main>`, holding `<a
href="/feedback">Feedback</a>` and `<button onclick="openPageHelp()">Help
</button>`. Both gated on `personName != ""` - `/feedback` requires auth
(registered behind `RequireAuth` in `routes.go`, same as every other
authenticated route), so an unauthenticated page (only `login.templ` calls
`Layout` with an empty `personName`) would otherwise show a Feedback link
that just bounces to sign-in, and Help has nothing to help with there
either.

**The Help dispatcher** (`web/static/js/scheduling.js`'s `openPageHelp`):
checked in order -

1. `#onboarding-tour` present -> `onboardingTourController.open()`.
2. `#build-tour` present -> `buildTourController.open()`.
3. A `.page-help-dialog` present -> `openHelpDialog(id)` on it directly.
4. Otherwise -> `openHelpDialog("generic-help-dialog")`.

Dispatch is by **presence of an element**, not a data attribute on `<body>`
or a second argument threaded through `Layout`'s own signature - see "Why
presence-of-element dispatch" below.

**Two new stateless helpers**, `openHelpDialog(id)`/`closeHelpDialog(id)`
(`scheduling.js`) - a plain `.showModal()`/`.close()` pair (with the same
`open`-attribute fallback the tour controllers use), no config object, no
step state. Back every single-step dialog: the 8 setup-page ones and
`genericHelpDialog`. See "Why these don't go through `createTourController`"
below for why that factory - built for the Home/Build tours' auto-open +
localStorage-dismissal + forced-reopen machinery - would be overkill here.

**`genericHelpDialog`** (`internal/views/layout.templ`): the last-resort
fallback for every page with no tour of its own (Account, Billing, the
style guide, ...) - a single step, "Need a hand?", pointing at `/account`
and `/feedback`. Rendered unconditionally inside `Layout` itself (not
gated on `personName`) so it always exists in the DOM regardless of which
page's `{ children... }` rendered - the same "exists on every page,
findable by id" shape `installPromptBanner` already used for a different
purpose.

**8 new setup-page dialogs**, one per page, each `<dialog id="<page>-help"
class="tour-dialog page-help-dialog">` with exactly one `.tour-step` (the
page's own `<h1>` text as the heading, one paragraph of "what this is for
and what Build does with it" copy) and a single `.button.primary` "Got it"
- no dots, no Back/Next, closed via `closeHelpDialog('<page>-help')`.
Placed right after each page's `@views.Layout(...) {` opens, same spot
`@onboardingTour()`/`@buildTour()` sit in `home.templ`/`build.templ`:

| Page | Dialog id | File |
|---|---|---|
| Song Relationships | `song-relationships-help` | `relationships.templ` |
| Song Roles | `song-roles-help` | `song_roles.templ` |
| Song Tags | `song-tags-help` | `song_tags.templ` |
| Song / Person Preferences | `song-preferences-help` | `preferences.templ` |
| Person Relationships | `person-relationships-help` | `person_relationships.templ` |
| Position Preferences | `position-preferences-help` | `position_preferences.templ` |
| Vocal Range | `vocal-range-help` | `vocal_range.templ` |
| Song Vocal Range | `song-vocal-range-help` | `song_vocal_range.templ` |

`song_tag_suggestions.templ` (the review-queue page) deliberately does not
get one - same reasoning the onboarding checklist already applied to
exclude it from trackable setup items: it's a queue to work through, not a
one-time setup item with "what is this page for" as an open question.

**Account page** (`internal/views/account.templ`): the "Getting started"
section's two tour-replay links (`/?tour=1`, and the
`forceBuildTourReplay()` Build-tour link) are gone, replaced by one
sentence: *"Look for **Help** at the bottom of any page for a quick tour of
what's there."* Both links are now genuinely redundant, not just
duplicated - Help finds and opens the exact same dialogs from wherever
someone already is, without a trip to Account first. The underlying
replay mechanisms (`?tour=1`, `?build-tour=1`,
`forceBuildTourReplay`/`sessionStorage` handoff) are **not** removed - they
still work if linked to by hand - only the in-app links pointing at them
are gone; see `home.templ`'s `onboardingTour` and `build.templ`'s
`buildTour` doc comments for where their own "how someone gets back here"
notes were updated to match.

**Both tours' footnote**: "Need this again later? Find it on your Account
page." became "Need this again later? Look for Help at the bottom of any
page." (plain text, no link - Help isn't a URL to link to, it's a
same-page button).

## Design rationale

### Why the account dropdown reuses `.nav-dropdown` verbatim

`toggleNavDropdown`/`positionNavDropdown`/`closeNavDropdowns` and the
document-level outside-click/Escape handlers in `scheduling.js` were
already written against `document.querySelectorAll(".nav-dropdown...")`,
not a single hardcoded instance - verified before reusing it rather than
assumed. That means a second `Disclosure("nav-dropdown", ...)` call gets
positioning (including the narrow-viewport `align-right` flip),
outside-click-close, and mobile collapse behavior for free, and the two
dropdowns don't fight: each tracks its own `.open` class independently
(scoped via `.closest(".nav-dropdown")` in every handler), and clicking
one closes the other through the same "anything outside `.nav-dropdown`
closes every open one" listener that already existed for the single Setup
dropdown - nothing new to write or reconcile. The alternative (a parallel
`"account-dropdown"` class family, `components/disclosure.css`-style)
would have meant re-deriving the exact same CSS and JS for zero behavioral
difference - genuinely the same interaction pattern twice.

One real constraint from doing it this way: `wrapperClass + "-menu"` inside
`components.Disclosure` is a plain string concatenation, so a wrapper class
can't safely be two space-separated classes (e.g. `"nav-dropdown
account-dropdown"`) - that would make the *toggle* wrapper carry both
classes but the *menu* only carry `"nav-dropdown account-dropdown-menu"`
(two classes: `nav-dropdown` and `account-dropdown-menu`, not
`nav-dropdown-menu`), breaking `positionNavDropdown`'s
`dropdown.querySelector(".nav-dropdown-menu")` lookup for that instance.
So `header-nav.css`'s few new rules for the account dropdown's own popup
contents (theme toggle spacing, org-name padding) are scoped by ancestry
(`.site-actions .nav-dropdown-menu ...`) instead of a second class - the
Setup dropdown lives inside `.site-nav`, so that scoping cleanly reaches
only the account instance without needing a distinguishing class at all.

### Why Help lives in the footer, not the header

Derek's explicit call, made with the scrolling tradeoff named up front: a
header Help button is one click away on every page load, a footer one
needs a scroll on any page with real content above it. Chosen anyway -
this doc doesn't relitigate it, just records that it was a deliberate
choice, not a default. (No competing recommendation was made either way -
this was asked and answered directly, not proposed and revised.)

### Why setup-page tours are single-step, not multi-step

The onboarding plan's original tours (Home, Build) exist to orient someone
across a *page's whole shape* - multiple tabs, a sidebar, several classes
of warning - genuinely multi-step material. Each setup page is one form
plus one table: there's exactly one thing to say ("what is this for, what
does Build do with it"), so a second step would be padding, not content.
Mirrors `createTourController`'s own `renderTourStep`-style step-hiding
logic being entirely unnecessary here: with one `.tour-step`, there's
nothing to hide or navigate between.

### Why these 8 (+ the generic fallback) don't go through `createTourController`

`createTourController` exists specifically for three behaviors the
Home/Build tours need and these don't: auto-open once per browser
(`localStorage` "seen" flag), a forced-reopen query-string/session-storage
handoff, and multi-step render/dot-indicator state. Every one of the 9 new
dialogs here (8 setup pages + the generic fallback) is Help-triggered
only - never auto-shown - and has exactly one step. Routing them through
the factory would mean passing a `storageKey`/`queryParam` that's never
consulted (nothing ever auto-opens them) just to get `open()`/`close()`
methods a two-function pair already provides more simply. `openHelpDialog`/
`closeHelpDialog` are the right-sized tool: a plain `.showModal()`/`.close()`
wrapper, no closure state, no config object - reused verbatim by all 9
dialogs rather than each getting its own copy.

### Why presence-of-element dispatch, not a data attribute or a `Layout` param

`openPageHelp()` decides which dialog to open by asking the DOM "does
`#onboarding-tour` exist? `#build-tour`? a `.page-help-dialog`?" rather than
reading a `data-help-target` attribute on `<body>` or taking a new
parameter through `Layout`'s own call signature. `Layout` is called by
every page in the app (`Home`, `Account`, `Billing`, `BuildPlan`, all 8
setup pages, the style guide, `login.templ`, ...) - changing its signature
would mean touching every one of those call sites for a lookup the DOM can
already answer directly. Every page's own tour/help dialog already renders
somewhere inside `{ children... }` (or, for the generic fallback, inside
`Layout` itself) - `document.getElementById`/`querySelector` finds it
regardless of where in the page's markup it actually sits, so there's
nothing to thread through call signatures at all. Simplicity, not a
performance or correctness difference - a `data-` attribute approach would
have worked too, just adds a second thing (the attribute) that has to stay
in sync with the first (which dialog actually exists), for no benefit.

### Why the setup-page recommendation flipped

`onboarding-and-ambient-framing-plan.md`'s "Proposed, not started" section
recommended *against* per-setup-page guidance, reasoning that a modal
popping on first visit to each of 7 (now 8) rarely-visited pages is tour
fatigue by definition - "don't want to be overbearing" was Derek's own
framing at the time. That reasoning holds for an **auto-popping** modal;
it says nothing against an **on-demand, click-to-open** one. A button that
only ever does something when clicked can't be overbearing - it's inert
until someone wants it. The original recommendation is left in place in
that doc (not deleted), with a note pointing here, per this codebase's own
convention of annotating superseded reasoning rather than erasing it (see
that doc's "Phase 2 history (superseded)" section for the precedent).

### Why the Account page's replay links became redundant, not just duplicated

Before this change, "replay the tour" only existed as an Account-page
link - the *only* way back into either tour once auto-open had already
fired once. Now Help finds and opens whichever tour belongs to wherever
someone already is, without a detour through Account first - strictly more
convenient, not an alternate path to the same convenience. Keeping the old
links would have meant two ways to reach the same dialog, one of them
slower, which is worse than one way - so they're removed, not left as a
redundant option alongside Help.

## Verification

- `templ generate` then `go build ./...` - clean.
- `go test ./... -count=1` - clean (no test in this repo asserts against
  rendered header/footer/dialog HTML, so nothing needed updating for it -
  confirmed by grepping `internal/server/controllers/*_test.go` and every
  `internal/views/**/*_test.go` for `site-actions`, `/account`,
  `/feedback`, `onboarding-tour`, `build-tour`, `Replay the`, and
  `tour-footnote` before assuming so).
- Grepped the whole repo for `?tour=1`/`forceBuildTourReplay`/"Replay the
  intro tour"/"Replay the Build page tour" to confirm no other in-app link
  still points at the now-removed Account-page replay UI (doc comments
  describing the still-functional underlying mechanisms are expected hits,
  not bugs).
- Still worth a live check once deployed:
  - Help opens the right dialog per page type - Home (`onboarding-tour`),
    a real plan's Build page (`build-tour`), each of the 8 setup pages
    (its own `<page>-help`), and a page with none of those (Account,
    Billing, the style guide) falls through to `generic-help-dialog`.
  - The account dropdown opens/closes/positions correctly (including the
    narrow-viewport `align-right` flip) and doesn't fight with the Setup
    dropdown - open one, then the other, confirm the first one closes;
    click outside both, confirm both close; Escape closes whichever is
    open.
  - Mobile collapse (`toggleMobileMenu`) still shows both the nav and
    account dropdowns correctly once the hamburger menu is open.
  - The footer renders on every authenticated page (spot-check a few: a
    setup page, Build, Account) and does *not* render on the sign-in page.
  - The Account page no longer shows either old tour-replay link, and its
    "Getting started" section still reads sensibly with just the one
    sentence pointing at Help.
