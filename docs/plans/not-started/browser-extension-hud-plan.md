# Plan: browser extension / HUD on Planning Center's own Plan page

Written 2026-09-06, expanding `docs/ideas.md`'s **"Browser extension
HUD"** bullet (Reducing friction vs. Planning Center, ~line 104-118) into
a real scoping doc. Not built, not started, and this doc does not decide
*when* it should be - see "Non-goal" at the end. Companion doc:
`docs/plans/shipped/pwa-install-improvements-plan.md`, the
lighter-weight friction item this one is explicitly sequenced behind.

## Why this doc exists now, and what it doesn't claim

Derek raised this exact caution independently while asking for this doc:
*"I still think it's worth proving out the app first."* That matches
`docs/ideas.md`'s own Open Questions section verbatim ("Whether the
browser-extension HUD is worth building at all before there's real usage
data suggesting the lighter integrations aren't enough") and its own
sequencing note on the bullet itself ("decide this only after the
lighter-weight items above are in and adoption data says it's worth it").
This doc takes that as settled, not as something to re-litigate - it
exists so that *when* the moment comes, the architecture questions are
already answered instead of being re-derived from scratch under time
pressure. Nothing here is a build order or a commitment.

This doc also does not re-research whether Planning Center offers an
official embedded-app surface for this. That question is already
answered in `docs/ideas.md`'s Open Questions section (~line 183-200):
PCO's real Add-Ons program only has insertion points in **People**
(`people.profile.custom_tab`, `people.profile.info_section`) - there is
no Services/Plans insertion point today, and that's gated on PCO
shipping one, not on this app's traction. **A browser extension/content
script is the only currently-viable way to put this app's data on PCO's
live plan UI.** Cited as settled; not re-checked here.

## 1. The core content question

Derek's own framing, which this section works through directly: *"given
a user will be able to access a chrome extension with our data from
within PCO and its plan page, what would be useful enough to have
available without being overwhelming?"*

### Candidate surfaces this app already computes

Everything below is either already rendered on the Build page today or
named in an existing not-started plan doc. None of it requires new
scheduling logic - the gap (see section 2) is *exposing* it as JSON, not
computing it.

| Signal | Where it's computed today | Protects |
|---|---|---|
| Repeat-song warning (sung too recently) | `sets_builder.go` (`RepeatWarnings`), rendered as `ConflictBadge` per row | Service |
| Avoid-pairing conflict | `rosters_candidate_warnings.go` family, rendered as `ConflictBadge` | Team |
| Key-transition warning (half-step/tritone) | `keyTransitionWarnings`, `sets_builder.go` | Service |
| Service-length warning (vs. recent plan average) | `serviceLengthWarning`, `sets_builder.go` | Service |
| Open-position count | `buildRosterBuilderData` | Service (staffing gap) |
| Blockout collision | `person_blockouts` cache + roster warnings | Team |
| Cross-team same-week conflict | `crossTeamWarning`, `rosters.go` | Team |
| Publish/draft status (does the local draft match what's live?) | `sets.go`/plan status helpers | Service (avoids confusion about which version is "real") |
| Vocal-range mismatch badge | shipped - see `docs/plans/shipped/vocal-range-plan.md` Phase 3 | Team |

### Recommended first-pass set (deliberately small)

Show, per plan, on a single compact HUD panel:

1. **A single aggregate warning count/badge** ("3 issues") rather than
   every warning's full text - the same "glance, don't read" job the
   in-place badges already do on the Build page itself
   (`docs/plans/shipped/in-place-set-warnings-plan.md`), one level more
   compressed since PCO's own UI has no row to attach a per-song badge to
   the way this app's own Songs tab does.
2. **Open-position count** ("2 positions still need someone") - a single
   number, no per-position detail.
3. **Publish/draft status** (in sync / has unpublished local changes) -
   answers "is what I'm looking at in PCO the current plan," which
   matters specifically *because* the user is looking at PCO's own UI
   right now, not this app's.
4. **The link back to the full app** - see below, non-negotiable.

**Explicitly held out of the first pass**, and why:

- **Per-warning detail text** (which two songs repeat, whose blockout
  conflicts) - this is exactly the "overwhelming" risk in Derek's own
  question. A HUD that reproduces the Build page's full warning list
  inside a cramped PCO-tab panel isn't a HUD, it's a second copy of the
  Build page - it fails the friction test ("is this the fast, obvious
  way, or a second UI pattern to learn") by becoming its own thing to
  read rather than a glance. The link back to the full app is where
  detail belongs.
- **Vocal-range mismatch, key-transition specifics, cross-team detail** -
  real signals, but each is already one click away via the link-back:
  including all of them up front multiplies the "which of these many
  badges do I actually look at" problem the aggregate-count approach in
  item 1 is designed to avoid. Worth revisiting once vocal range
  (`vocal-range-plan.md`) and song-leader (`song-leader-plan.md`) actually
  ship and there's a sense of which individual signals people actually
  act on from inside PCO's tab vs. only from the Build page.
- **Anything requiring a second glance to interpret** (a numeric score, a
  trend) - fails the same test; a HUD's entire value proposition is "you
  didn't have to leave PCO to know," which only holds if reading it costs
  less than switching tabs would have.

### The explicit link back to the full app

Per Derek's own ask, this is the actual point of entry for anything more
than a glance - not an afterthought "learn more" link, but the HUD's
primary action: one button/link, always visible, deep-linking straight to
this plan's Build page (`/scheduling/build/:serviceTypeID/:planID` - see
"Architecture" below for how the HUD would know the plan id). The HUD's
own job is to answer "should I go look," not to answer the question
itself.

### Tying this back to CLAUDE.md's tests

Every included item maps directly to protect-the-team or protect-the-
service (the table above tags each one); nothing speculative or merely
"interesting" made the first-pass list. The friction test cuts the other
way from usual here: normally "is this the fast, obvious way" argues
*for* building something inline; here it argues for showing *less*, since
a HUD that takes longer to parse than switching tabs to the real Build
page would have taken is strictly worse than no HUD at all.

## 2. Recommended architecture: route through this app's own API

**Recommendation: the extension should be a thin read-only client of
`pco-assistant`'s own (new) JSON endpoints - never call PCO's API
directly from inside the PCO tab.** This is a real architecture
recommendation, not a placeholder default, but it still needs Derek's
sign-off before anything is built against it - flagging that explicitly
per this task's own instruction not to treat an architecture call as
settled just by being written down here.

### Why this resolves both of `docs/ideas.md`'s flagged open questions

`docs/ideas.md`'s bullet (~line 111-118) names two unsolved design
questions for this feature, neither a blocker today but both real once
it's built. Routing through this app's own API resolves both at once,
by construction rather than by a separate fix:

**(a) PCO rate-limit exposure.** PCO's limit is 100 requests/20 seconds
*per user token* (`docs/pco-api-risks.md`). If the extension called PCO
directly from the content script, it would be a **second, uncoordinated
caller** sharing that exact same per-user budget with every render this
app's own server already makes on that same token - invisible to this
app's existing throttle (`pco.ConfigureThrottle`), existing caches
(`FetchActiveSongs`, `FetchServiceTypes`, the Song Info/person-data
caches), and existing response-hook logging (`pco.SetResponseHook`).
Worse, it would be polling on some interval to keep a HUD "live" while a
user has a PCO tab open - exactly the kind of sustained, independent
traffic pattern that produced the real 429 incident in
`docs/pco-api-risks.md`'s changelog (a user paging rapidly through
Prev/Next, no polling loop involved at all).

Routing through this app's own server means the extension's calls are
just more HTTP requests to `pco-assistant`'s own domain, authenticated by
whatever auth mechanism section 3 settles on - **PCO rate-limit exposure
stays exactly where it already is today**, mediated by this app's
existing caching/throttling machinery, never duplicated by a second
caller PCO's rate limiter can't distinguish from the first.

**(b) Seat-based billing attribution.** This app's billing (README) is
seat-based, keyed to a person's `pco-assistant` OAuth session. Activity
inside PCO's own tab has no natural way to attribute to that model unless
it's actually authenticated *as* that session. If the extension is a
client of this app's own API - reusing the same account, whether via a
cookie-adjacent mechanism or an API token tied to the same person record
(section 3) - every HUD request **is** activity on that person's existing
`pco-assistant` account, the same as a Build-page render would be. No new
seat-counting logic, no new billing edge case: this stays "the same user,
using the same account, from a different surface," not a second identity
this app would have to reconcile against seats.

### What this requires: new read-only JSON surface

`scheduling_api.go` (`internal/server/controllers/`) exists today, but
it's CRUD-only - song relationships and preferences, not the computed
warnings. The warnings/conflicts (`RepeatWarnings`, `ExistingConflicts`,
`crossTeamWarning`, open-position counts, publish status) are built
inside `buildSetBuilderData`/`buildRosterBuilderData` and rendered
directly as templ HTML - **never serialized as JSON anywhere in this
codebase today.** This is real, non-trivial new surface, not a small
addition:

- A new read-only endpoint (or a small group, under `/api/scheduling/` or
  a new `/api/hud/` prefix) that, given a plan id, returns the compact
  first-pass payload from section 1 - aggregate warning count,
  open-position count, publish/draft status - as JSON.
- This means either factoring a JSON-shaped summary out of
  `buildSetBuilderData`/`buildRosterBuilderData`'s existing return
  structs (straightforward, since the warnings are already computed
  Go values before being handed to templ - only the serialization path is
  missing) or computing a smaller-scoped summary independently. The
  former reuses proven logic and stays consistent with what the Build
  page itself shows; the latter risks the HUD and the Build page
  disagreeing about the same plan. Prefer the former.
- Whichever endpoint(s) this becomes, they inherit the same
  `RequireAuth`/`RequireActiveOrganization` middleware chain every other
  route does (`routes.go`) - this is new *surface*, not a new *auth
  model*, except for the cross-origin wrinkle in section 3.
- No new PCO calls are required beyond what a Build-page render already
  makes for the same plan - this is a read of already-computed data, so
  `docs/pco-api-risks.md` needs no new entry for the endpoint itself
  (only for section 3's auth mechanism, if it turns out to add one).

## 3. Auth from inside a PCO tab - needs a real decision, not a default

A content script running on `services.planningcenteronline.com` is
cross-origin from `pco-assistant`'s own domain. It cannot silently ride
an existing `pco-assistant` session cookie the way a same-origin request
from this app's own pages does - a plain `fetch()` from the content
script's page context would be subject to normal cross-origin cookie/CORS
rules, and `pco-assistant`'s session cookie almost certainly isn't (and
shouldn't be made) readable or sendable from a third-party origin.

Two real options, genuinely undecided here:

**Option A: Manifest V3 background/service-worker fetch with
`host_permissions`.** In Manifest V3, network requests made from an
extension's background service worker (not the content script's own page
context) can be granted `host_permissions` for a specific origin
(`https://pco-assistant-domain/*`), and per Chrome's extension
architecture this is understood to let the background worker's `fetch`
bypass the page-level CORS restriction a same-tab request would hit -
**this needs to be verified against current Chrome extension docs before
being relied on, not assumed from memory**, per this task's own
instruction to flag exactly this kind of claim for double-checking. If
confirmed, the content script would message the background worker
("get HUD data for plan X"), the background worker would make the actual
authenticated request to `pco-assistant`, and the result flows back to
the content script to render. This still needs *some* credential the
background worker sends - a stored session token or API token (below),
since even a permitted cross-origin fetch needs the server to recognize
the caller as a specific `pco-assistant` user, not just any caller from
an allowed origin.

**Option B: a dedicated lightweight API-token flow**, issued from this
app's own `/account` page (the same page section 2 of the PWA doc
proposes an Install action for - see
`docs/plans/shipped/pwa-install-improvements-plan.md`) rather than
relying on cookies at all. A person generates a long-lived, revocable
token there; the extension stores it (`chrome.storage`, not
`localStorage`, since a content script's `localStorage` is scoped to the
*page's* origin - PCO's - not the extension's own) and sends it as a
bearer header on every HUD request. This sidesteps the cross-origin
cookie question entirely, at the cost of new server-side surface: a
token table, an issuance/revocation UI, and a new auth-middleware path
that accepts a bearer token as an alternative to the session cookie
`RequireAuth` checks today. No such token concept exists anywhere in this
codebase currently (confirmed - no `api_token`/`PersonalAccessToken`
anywhere in `internal/auth` or `internal/server`), so this is new
product surface, not a small addition either.

**This is a real "needs a real decision, not a default" question.**
Option A is less new surface if the cross-origin fetch behavior confirms
as expected, but ties the extension's auth lifetime to whatever session
lifetime `pco-assistant` already uses (and to correctly refreshing that
session from a background worker with no request-response cycle of its
own). Option B is more explicit new surface but decouples the extension's
auth entirely from browser session mechanics, and a revocable token is
arguably a better security posture for something living in a third-party
browser extension's storage than a copy of an authenticated session
credential. Whichever is chosen, verify the actual current Chrome
Manifest V3 cross-origin fetch behavior for extension background workers
against Chrome's own current developer docs before committing - the
task's own framing ("browser extension cross-origin behavior is exactly
the kind of thing worth double-checking") applies with real weight, since
this app's entire security model rests on that answer being right.

## 4. Distribution - needs a real decision, not a default

**Chrome Web Store submission realities**: an extension requesting
`host_permissions` on a third-party site's DOM
(`services.planningcenteronline.com/*`) plus `host_permissions` on this
app's own API domain is exactly the shape of extension Chrome Web Store
review scrutinizes most closely - broad host access historically draws
real manual review, and Google's Developer Program Policies require a
published, accessible privacy policy for any extension requesting
sensitive permissions (host permissions among them). Neither of these is
a blocker, but both are real process, not paperwork to skip.

**Unlisted/private listing for early testing**: worth verifying current
Chrome Web Store policy specifically rather than assuming - **flagged
here for verification, not asserted confidently**, per this task's own
instruction. The general shape understood today is that Chrome has
progressively tightened what counts as an installable extension on
stable Chrome outside the Web Store: unpacked/sideloaded extensions are
largely blocked or heavily warned-against for non-developer end users on
stable channel, which means even a small early-tester group realistically
needs *some* Web Store presence (an "unlisted" listing - reachable only
via direct link, not searchable/discoverable) rather than a
side-load-and-share approach. Confirm this against Chrome Web Store's
current developer documentation before committing to a distribution plan
- policy in this exact area (installability outside the store,
requirements for unlisted items) has shifted before and could again.

**Cross-browser**: not addressed here beyond noting it as a real fork in
scope (see the clarifying questions below) - Manifest V3 extension APIs
are largely but not perfectly compatible across Chrome/Edge (both
Chromium-based, closest to drop-in) vs. Firefox (its own WebExtensions
implementation, different enough in places - background service workers
in particular - to need real verification, not an assumption of
parity).

## 5. What would help - and what to ask Derek

**Yes, a real Planning Center Plan page export would materially help**,
concretely:

- **A full-page screenshot** resolves where a floating panel could
  physically dock without colliding with PCO's own drag-and-drop UI -
  right now this doc has to guess at PCO's actual layout (sidebar widths,
  whether there's dead space, where a floating panel would overlap active
  drop targets) rather than design against it.
- **A saved-HTML copy of the page** (View Source / Save Page As, or a DOM
  export) resolves which selectors/classes look stable enough to anchor
  a content script's injection point to, vs. which look like generated/
  hashed class names likely to break on PCO's next redesign - `content_
  scripts.matches` in the extension manifest only needs the URL pattern
  (already known: `https://services.planningcenteronline.com/plans/*`,
  per `pcoPlanURL`'s own doc comment, live-verified), but the actual DOM
  injection point (where in the page tree to mount a panel) needs real
  markup to target reliably, not a guess.
- Together, both resolve **what PCO's page already shows natively**, so
  the HUD doesn't duplicate anything already visible on the page itself
  (no point re-showing a position's fill status if PCO's own sidebar
  already shows it inline, for instance) - the HUD's job is to show what
  PCO's own page structurally *cannot* show (this app's cross-PCO-data
  computed warnings), not to re-skin what's already there.

**Specific clarifying questions worth asking Derek before this is
built**, each because the answer changes real scope:

1. **Which PCO pages to target** - just Plan detail
   (`/plans/:id`), or also the Plan list, Song library, or People pages?
   Each additional page is a real content-script surface with its own
   design questions, not a checkbox.
2. **Chrome-only, or cross-browser (Firefox/Edge) from day one?** Chrome
   and Edge share a Manifest V3 implementation closely enough to likely
   ship together for near-zero extra cost; Firefox's WebExtensions
   support has real enough differences (particularly for background
   workers) to be a genuinely separate scope decision, not an assumed
   freebie.
3. **Any existing mockup or interaction Derek already has in mind** -
   floating draggable badge, a fixed sidebar panel, or a toolbar-icon
   popup (click the extension icon to see the HUD, vs. always-visible
   injected into the page)? Each is a materially different build, not a
   styling choice made late.
4. **Appetite for Web Store review vs. limited/internal distribution**
   for early testing - shapes both the auth story (a small, known set of
   early testers is a much easier bar for either option in section 3
   than a public audience) and the timeline (Web Store review is real
   lead time, not instant).

## Non-goal: this doc does not decide *when*

Matching Derek's own words and `docs/ideas.md`'s existing Open Questions
entry: this stays a scoping document until real usage data from the
lighter-weight integrations - deep linking (already shipped), the narrow/
compact side-by-side layout (see
`docs/plans/shipped/pwa-install-improvements-plan.md`'s companion
scope, and `docs/ideas.md`'s own bullet #3), live sync reflection, and
pre-publish nudges - suggests they aren't enough on their own. Nothing in
this doc should be read as "start building this next."
