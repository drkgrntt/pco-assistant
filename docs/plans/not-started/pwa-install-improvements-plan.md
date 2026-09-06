# Plan: PWA install-prompt improvements

Written 2026-09-06. Smaller and more concrete than its companion,
`docs/plans/not-started/browser-extension-hud-plan.md` - this one doesn't
block on anything external and is buildable soon, not gated on adoption
data or a Derek architecture sign-off the way the extension is. Advances
`docs/ideas.md`'s "Narrow/compact side-by-side layout" bullet (Reducing
friction vs. Planning Center, ~line 99-103) indirectly: that bullet is
about *layout* once installed, this doc is about actually getting
installed in the first place - related but distinct halves of the same
"live in a PWA window beside PCO" goal.

## Current state

Confirmed in this codebase today:

- `web/static/manifest.json` - a real, complete manifest (`name`,
  `short_name`, `start_url`, `scope`, `display: standalone`,
  `background_color`/`theme_color`, 192/512px maskable icons). Linked from
  `internal/views/layout.templ`'s shared `Head` component
  (`<link rel="manifest" href="/manifest.json"/>`), so every authenticated
  page and the public landing page alike carry it.
- `web/static/js/sw.js` - a service worker, registered by
  `web/static/js/register-sw.js` (`navigator.serviceWorker.register(
"/sw.js")`), loaded from `Head` on every page.
- README already states: "Mobile-first, installable as a PWA, with
  light/dark theming."
- **No custom install UI exists anywhere in this codebase** - no
  `beforeinstallprompt` listener, no install button, no iOS instructional
  copy. Confirmed by grep across `web/static` and `internal/views`: the
  only PWA-related JS is service-worker registration. Installability
  today is entirely the browser's own native, passive affordance (an
  install icon in Chrome's omnibox, or an Android "Add to Home Screen"
  banner) - easy to miss, and never suggested to the user at a moment
  that's actually relevant to them.

## The real gap: `beforeinstallprompt` does not exist on iOS Safari

This is the single most important fact this plan is built around, and
the reason a "just capture `beforeinstallprompt` and show a nicer button"
plan alone would under-serve this app's actual users: **`beforeinstall
prompt` is a Chromium-only event (Chrome/Edge desktop and Android) - iOS
Safari never fires it, and Apple has never implemented an equivalent
programmatic install API.** The only way to install a PWA to an iPhone's
home screen is the manual **Share button -> "Add to Home Screen"** flow,
which cannot be triggered by any script - it can only be *explained*.

Given this app's actual user base (worship leaders/volunteers - a
population plausibly skewing meaningfully toward iPhones), the
instructional-UI half of this plan is not a lesser fallback for the
Chromium path, it may well be the more-used half. Treat both halves as
equally real scope, not "the real feature plus an iOS afterthought."

## Proposal

### 1. Capture `beforeinstallprompt`, drive a custom in-app prompt

On Chromium browsers (Chrome/Edge, desktop and Android), listen for the
`beforeinstallprompt` event as early as the app shell loads (a small new
script, e.g. `web/static/js/install-prompt.js`, loaded from `Head`
alongside `register-sw.js`):

```js
let deferredInstallPrompt = null;
window.addEventListener("beforeinstallprompt", (e) => {
  e.preventDefault(); // suppress the browser's own mini-infobar
  deferredInstallPrompt = e;
  // reveal the in-app Install affordance (see placement below)
});
```

Calling `e.preventDefault()` stops the browser's own passive prompt from
firing at a browser-chosen moment, deferring it until *this app* chooses
to show its own affordance and calls `deferredInstallPrompt.prompt()` -
this is the actual mechanism that lets install timing move from "whenever
the browser feels like it" to "when this app decides it's a good moment"
(see placement/trigger below). If the event never fires (already
installed, unsupported browser, or iOS), the custom UI simply never
appears - no dead button.

### 2. iOS Safari: manual instructional UI, user-agent gated

Since there's no event to listen for, detect iOS Safari specifically (a
plain user-agent check - genuinely one of the few defensible uses of UA
sniffing, since there is no feature-detectable alternative here) and, in
the same placement as the Chromium button, show static instructional copy
instead of a clickable action: *"Add Resonance to your Home Screen: tap
the Share icon, then 'Add to Home Screen.'"* Ideally paired with a small
inline diagram/icon showing the Share icon shape, since "tap the Share
icon" alone assumes the reader already knows which icon that is.

Detection needs to distinguish **standalone-already** (running from an
already-installed home-screen icon, where `navigator.standalone === true`
on iOS Safari) from **browser tab, installable** - only show the
instructional banner in the latter case, exactly mirroring how the
Chromium path only shows its button when `beforeinstallprompt` actually
fired (which itself won't fire if already installed).

### 3. Dismissal / re-nag logic

Whichever path fires, remember a dismissal in `localStorage` (this is a
genuinely per-viewer, non-critical UI preference - exactly the kind of
state CLAUDE.md/artifact guidance calls out `localStorage` as the right
fit for, not something needing server-side storage) and don't re-show
every session:

```js
const DISMISS_KEY = "installPromptDismissedAt";
const RENAG_DAYS = 14;

function shouldShowInstallPrompt() {
  const dismissed = localStorage.getItem(DISMISS_KEY);
  if (!dismissed) return true;
  const daysSince = (Date.now() - Number(dismissed)) / 86400000;
  return daysSince > RENAG_DAYS;
}
// on dismiss click: localStorage.setItem(DISMISS_KEY, String(Date.now()))
```

A real "Install" click (Chromium path) that the user actually accepts
never needs to re-nag at all - `appinstalled` (a separate event this app
should also listen for) fires once genuinely installed, at which point
the affordance should simply stop appearing, dismissal timer or not.
Wrap every `localStorage` read/write in try/catch per usual practice (a
private window or blocked site data throws rather than returning
null/undefined) and treat a throw as "show the prompt" - failing open
here costs nothing (worst case: the prompt reappears one extra time),
while failing closed would silently and permanently suppress the feature
for anyone whose browser blocks storage access.

### Placement and trigger

**Two concrete placements**, not competing options - both are worth
building, since they serve different moments:

1. **`/account` page** - a dedicated, persistent "Install this app"
   section, similar in shape to the existing "Data & Caches" section
   (`AccountCacheBody`, `internal/views/account.templ`): a `Card` with a
   short explanation and either the Chromium install button or the iOS
   instructional copy, whichever applies. This is the *findable* home for
   the feature - someone who dismissed the banner below, or who just
   wants to install deliberately, always has a stable place to do it
   (mirroring how the cache-clear actions live on Account precisely
   because "I want to do this once, deliberately" belongs on a settings
   page, not a transient banner).
2. **A dismissible in-app banner, shown after a few sessions** - not on
   first visit (a brand-new user has no reason yet to want this installed
   as an app - the value of "beside my PCO tab" only becomes obvious once
   someone's actually used the Build page a few times) and not on every
   page load once dismissed (see re-nag logic above). Simplest trigger:
   a `localStorage` session counter incremented once per day the app is
   opened (not once per page load), showing the banner starting on the
   Nth day guarded by "no dismissal recorded yet" - N itself is a small
   tuning knob (3-5 is a reasonable starting guess), not a hard
   requirement to get exactly right on the first pass.

Both read from the same `deferredInstallPrompt`/iOS-detection state, so
this is one shared script backing two render locations, not duplicated
logic - the Account page section is just always-rendered (behind the
"does this browser/OS support installing at all" check), while the banner
additionally checks the day-count/dismissal gate above.

## Rough build order

1. `web/static/js/install-prompt.js` - `beforeinstallprompt` capture +
   `preventDefault`, iOS Safari detection (`navigator.standalone` +
   UA check), dismissal/re-nag `localStorage` logic, `appinstalled`
   listener. Loaded from `Head` (`internal/views/layout.templ`) alongside
   `register-sw.js`, following the exact same cache-busted
   `?v=` + `AssetVersion` convention every other static asset there
   already uses.
2. Account page section - a new `Card` in `internal/views/account.templ`
   (near `AccountCacheBody`), server-rendered as a static shell (button +
   iOS copy both present in markup, JS toggles which is visible based on
   which install path applies) so it degrades sensibly with JS
   temporarily unavailable rather than rendering nothing.
3. Dismissible banner - reuses whatever this app's existing
   site-banner/alert component pattern is (`components/alert.css`,
   `site-banner.css` per `designSystemCSSFiles` - confirm the closest
   existing banner component before adding a new one) rather than
   inventing new banner markup; wired to the day-count/dismissal gate.
4. Manual verification pass (no automated test meaningfully covers
   browser-native install events): a real Chrome desktop session showing
   the custom button firing `prompt()` correctly and suppressing the
   native mini-infobar; a real Android Chrome session; a real iOS Safari
   session confirming the instructional copy appears when not yet
   installed and disappears once added to the home screen
   (`navigator.standalone`); confirm dismissal actually persists across a
   reload and correctly re-surfaces after the re-nag window (can
   fast-forward by editing the `localStorage` timestamp directly in
   devtools rather than waiting 14 real days).

No `docs/pco-api-risks.md` update needed - this touches zero PCO calls,
purely client-side browser install mechanics and static asset additions.
No migration needed - all state here is client-side `localStorage`, not
this app's own database.
