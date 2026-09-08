# Plan: ambient promise-framing + first-run onboarding nudge

**Status: Phases 1, 3, and 4 built 2026-09-08** (not yet deployed). Phase
2 (the dismissible nudge card) was also built same-day, then fully
**superseded and removed** a few hours later once it turned out too thin
("weak sauce" feedback, widened once, still not enough) - see "Phase 2
history (superseded)" below for what existed and why it's gone, and
"Phase 3: persistent setup checklist" for what replaced it. Nothing from
Phase 2 remains in the codebase; its section further down is kept only as
design-rationale history, per this file's own convention of not deleting
superseded reasoning outright.

Phase 1: `home.templ`'s Setup section regrouped under the two promises
(including the two vocal-range pages, split across both groups per "Which
product promise this serves" reasoning below), plus the empty-state copy
pass across `person_relationships.templ`, `relationships.templ`,
`song_roles.templ`, `preferences.templ`, `position_preferences.templ`,
`vocal_range.templ`, `song_vocal_range.templ`. Still current.

Phase 3 (see its own section below) replaced Phase 1's plain Setup list
*and* Phase 2 entirely: the Setup section is now itself the checklist -
permanent, not dismissible, real per-item done/not-done state.

Phase 4 (see its own section below) is additive, not a replacement of
anything: a separate guided first-run `<dialog>` tour, pure per-person
orientation independent of the checklist's per-org data. Revised same day
(2026-09-08, "not sit in the middle... next to the back button" feedback):
Back/Next grouped in their own `.tour-actions-nav`, Next demoted from
`.button.primary` to plain `.button` so the accent color reads as
"Done"'s alone, "Let's go" renamed "Got it, thanks!" (more clearly a
dismissal), and an always-visible line pointing back to the Account page
added between the dots and the action buttons.

The marketing-copy coverage audit (previously "adjacent, deliberately out
of scope") is done - see its own section below. A further round -
per-setup-page guidance and a possible Build-page tour - was asked about
and is written up as a real recommendation, **not built** - see "Proposed,
not started" below.

Written 2026-09-08, expanding `docs/ideas.md`'s "Reducing friction vs.
Planning Center" section - specifically the already-flagged-but-unbuilt
note: *"A related but distinct problem: discoverability... a lighter-weight
onboarding nudge or empty-state prompt pointing at the highest-leverage
features rather than leaving them to be discovered by accident."* Not built
yet. Two deliberately separable phases - do Phase 1 first, decide Phase 2
only after seeing whether it's still needed.

## The gap, precisely

The "protect the team / protect the service" framing (`docs/ideas.md`'s
Vision/positioning section) is fully realized on the **public** marketing
pages - `internal/views/landing.templ` and `internal/views/how_it_works.templ`
tell that story well, with concrete before/after scenarios per feature. It
**evaporates on login**. `internal/views/home.templ` today is a flat
"Setup" list of 7 links (song relationships, roles, tags, preferences,
person relationships, position preferences), each with a one-line `muted`
description of *what* it does, never *why it matters* or what it unlocks
downstream. Same pattern in the setup pages themselves - e.g.
`person_relationships.templ`'s empty state is just `"No relationships yet."`
(`song_roles.templ`: `"No songs tagged yet."`,
`position_preferences.templ`: `"No one assigned to this position yet."`).

A director who never sets up avoid-pairing or mentorship tags gets a
product that silently does less for them on the Build page - no signal
anywhere that they're leaving value on the table. That's the specific
discoverability problem both phases below address.

## Phase 1: ambient framing (copy-only, no new UI pattern)

Lowest effort, ships first, no new state/mechanism to design - just makes
existing screens say *why*, matching the friction discipline (no second UI
pattern to learn).

**Home page (`home.templ`)**: split "Setup"'s flat list into two grouped
lists under the two promises, each item keeping its existing one-line
description but gaining a short "why" clause tying it to what Build
actually does with it. Also folds in two setup pages that shipped
(`/scheduling/vocal-range`, `/scheduling/song-vocal-range` -
`docs/plans/in-progress/vocal-range-plan.md`) but were never added to this
list - the gap this section exists to close, not unique to vocal range:

- **Protect the service**: song relationships, song roles, song tags -
  "flows well / same theme / avoid pairing" -> ties to repeat-fatigue and
  set-flow warnings on Build's Songs tab. Plus **song vocal range** - "record
  each song's sung range" -> ties to the range-mismatch warning on Build
  (`vocal-range-plan.md` Phase 3), the same "conflict caught before
  publish" shape as the key-transition warning next to it.
- **Protect the team**: person relationships, position preferences -
  "works well / mentorship / avoid pairing" -> ties to the roster-builder
  warnings/suggestions on Build's Team tab. Plus **vocal range** (person) -
  "record each vocalist's comfortable range" -> makes the "can they hit
  this note" conversation faster and less awkward to have, same register
  as a burnout/preference note (`vocal-range-plan.md`'s own "Which product
  promise this serves" section reasons through this split already - person
  range is squarely team-dignity, the song-side mismatch warning it
  enables is squarely service-outcome; treat that as the model for any
  future setup item that seems to straddle both: split by where the
  *payoff* actually lands on Build, not by which page the data happens to
  live on).

Two short section intros (one sentence each, matching `how_it_works.templ`'s
own voice) rather than a heading-only change - e.g. "Protect the service"
section: *"These feed the warnings that catch repetition and a set that
doesn't flow, before the plan goes out."* Reuses the existing `.plan-list`
card-row treatment already on the page - no new CSS.

**Empty states**: the three found earlier (`person_relationships.templ`,
`song_roles.templ`, `position_preferences.templ`) plus the two vocal-range
pages' own ("No vocal ranges set yet." / "No song vocal ranges set yet."),
plus any other bare "No X yet." row in a setup page - grep for
`class="muted"` rows reading "No ... yet." across
`internal/views/scheduling/*.templ` to catch the full set before starting,
since only these five were spot-checked here. Each gets one added clause
naming the payoff, not a rewrite of the whole empty state - e.g.
person_relationships: *"No relationships yet - add one to get a warning
before two people who shouldn't be scheduled together end up on the same
roster."*

**Scope guard**: copy-only. No new component, no new migration, no new
route. A single templ-file pass, safe to build and ship on its own without
Phase 2.

## Phase 2 history (superseded) - first-run onboarding nudge

**Superseded 2026-09-08, same day it was built - see "Phase 3" below for
what replaced it.** Kept as design-rationale history only; nothing
described in this section exists in the codebase any more (no
`onboardingNudge()`, `HomeData.ShowOnboardingNudge`,
`dismissOnboardingNudge`, `?onboarding=1`, or `AccountGettingStartedBody`).
Built once as originally specced below (option 3, `localStorage`
dismissal), widened once in place after "weak sauce" feedback (mirrored
Phase 1's full 8-item grouped link list instead of a 2-link teaser) plus a
manual-replay path (`?onboarding=1` linked from a new Account page
section) added right after for an admin joining an already-configured org
- then replaced outright, same day, once a real progress checklist turned
out to be what was actually wanted instead of a bigger dismissible card.

Bigger commitment - needs real state, so it's a genuine design decision,
not a copy pass. Only worth building if Phase 1 alone doesn't move the
needle (real signal: does song-relationship/person-relationship setup rate
among new orgs actually improve after Phase 1 ships? - worth checking
before investing here rather than assuming Phase 1 is insufficient).

### Trigger condition

Show a dismissible nudge card on `home.templ` when an org has done *none*
of the high-leverage setup yet - concretely: zero rows across
`song_relationships`, `person_relationships` for the org (both queries
already exist as store methods; this just needs a cheap `EXISTS`-style
count, not a new fetch pattern - reuse the `scheduling.Store` pattern other
home-page counts already follow, e.g. `SuggestionCount`). Tag/role setup
deliberately excluded from the trigger condition - relationships are the
two categories that unlock warnings *invisible* until Build is used with
real conflicting data (see Vision/positioning's two promises); tags/roles
are more discoverable on their own since they're visibly consumed by the
song list UI itself.

### What it says

Points at the 2-3 highest-leverage steps, not all 7 setup items - avoid-pairing
and mentorship framing pulled straight from `how_it_works.templ`'s existing
copy (reuse voice, don't invent new copy from scratch): "Add one avoid-pairing
or mentorship relationship to start seeing warnings on the Build page" with
direct links to `/scheduling/song-relationships` and
`/scheduling/person-relationships`.

### Dismissal / persistence - the real open question

Three mechanisms, roughly by cost:

1. **Session/local only** (no DB change): `localStorage` flag set on
   dismiss. Cheapest, but doesn't survive a different device/browser or a
   different admin logging in later - reappears for every new signed-in
   person at that org until each dismisses it individually. Probably fine
   given this is a *nudge*, not a gate - re-surfacing for a second admin
   who hasn't seen it isn't a bug.
2. **Per-organization DB flag** (`organizations.onboarding_dismissed_at`
   or similar, one migration): persists across people/devices for the
   org. More correct, but a full migration + store method for a single
   boolean - worth it only if reappearing per-browser (option 1) turns out
   to actually annoy people in practice.
3. **Auto-hide once the trigger condition clears** (no dismiss button at
   all - the moment the org adds one relationship of either kind, the card
   just stops rendering next load): simplest of all, no persistence
   needed, but no way to dismiss it early if someone deliberately isn't
   using those features yet - it'd sit there indefinitely for an org that
   consciously doesn't want relationship tracking.

**Recommendation**: start with option 3 (auto-hide, zero persistence) plus
a plain dismiss link that falls back to option 1 (`localStorage`) for
someone who wants it gone without setting anything up. Skip option 2
unless real usage shows people re-triggering it across devices often
enough to be annoying - don't build a migration for a hypothetical.

### Scope guard

Home page only for v1 - don't also add nudges to individual empty states
beyond Phase 1's copy pass. If Phase 2 ships and still doesn't move setup
rates, that's a signal the problem isn't discoverability at all (worth
re-opening as its own question, not assumed away).

## Phase 3: persistent setup checklist (built 2026-09-08, replaced Phase 2)

Derek's explicit direction after Phase 2's widened card still read as
"weak sauce": build the progress-checklist alternative that Phase 2's
"Card widened" note had left as a live option, rather than iterating
further on a dismissible card. Chosen over the other heavier alternative
offered alongside it (a multi-step first-run tour) as "checklist first" -
the tour remains a real follow-up option, not ruled out, just not part of
this pass.

**What it is**: the Setup section itself became the checklist - no
separate nudge card, nothing to dismiss or replay, `?onboarding=1`/
`AccountGettingStartedBody` both removed as no-longer-needed (there's
nothing to replay when the real thing is always on the page). Same two
promise groups as Phase 1, same items/order, but each trackable item now
carries a real done/not-done `components.Badge` ("published"/Done vs.
"status-pending"/Not set up) instead of a plain link, plus a "N of M set
up so far" count in the section's own intro line.

**Data model** (`internal/views/home_types.go`): `HomeData.Onboarding
OnboardingChecklist{Groups []OnboardingGroup{Title, Intro, Items
[]OnboardingItem{Label, Description, Href, Done, Trackable}}, Done,
Trackable int}`. `Done`/`Trackable` on the checklist itself count only
`Trackable` items - see next paragraph for why one item is excluded
outright rather than counted as permanently "not done".

**The one non-trackable item**: Position Preferences. Its data
(`schedule_preference`) lives entirely in Planning Center - written
straight through, never stored locally (see CLAUDE.md's Sync model
section) - so there's no local existence check available the way there is
for the other seven, and computing a live equivalent would mean a real PCO
call across every team/position on every single home page load, which
`docs/pco-api-risks.md`'s hot-render-path caution rules out. Resolved by
just not tracking it: `OnboardingItem.Trackable = false` for this one item
only, rendered as a plain link with no badge, excluded from both the
`Done` and `Trackable` counts (never counted as permanently incomplete).

**New store methods** (`internal/scheduling`, one file each, mirroring
`HasAnySongRelationships`'s existing shape - a bare `SELECT EXISTS(...)`,
no row data fetched): `HasAnySongRoles` (song_roles.go),
`HasAnySongTags` (song_tags.go), `HasAnySongPersonPreferences`
(song_preferences.go), `HasAnySongVocalRanges` (song_vocal_range.go),
`HasAnyPersonVocalRanges` (person_vocal_range.go) - plus the two that
already existed from Phase 2 (`HasAnySongRelationships`,
`HasAnyPersonRelationships`), now serving the checklist instead of the
removed nudge's trigger condition.

**Controller** (`HomeController.buildOnboardingChecklist`, `home.go`):
seven sequential local-DB existence checks (not fanned out via
`scheduling.RunConcurrently` - that's reserved for PCO calls under rate
pressure, not single indexed Postgres `EXISTS` queries), each best-effort
(a failing check just leaves that one item "not done" rather than erroring
the whole page, same tolerance as the rest of this file).

**Song Tag Suggestions** stayed outside the checklist entirely - it's a
review queue, not a one-time setup item with a done/not-done state, so it
still renders as a conditional plain `<li>` appended to the "Protect the
service" group only while `SuggestionCount > 0`, exactly as it did before
this phase (unchanged logic, just relocated into the loop's own group).

## Phase 4: guided first-run tour (built 2026-09-08)

Derek's explicit direction, right after Phase 3: build the "guided
first-run tour" option that had been offered alongside the checklist and
left deferred (see "Open questions" below, pre-edit) - not because the
checklist was insufficient, but as a genuinely separate, additive piece:
pure orientation ("what is this app, why do these two promises matter"),
distinct from the checklist's "what's configured so far."

**What it is**: a 4-step `<dialog>` (`home.templ`'s `onboardingTour`) -
welcome/two-promises framing, then one step each for "Protect the
service"/"Protect the team" (2-3 condensed scenarios apiece, same voice as
`how_it_works.templ`'s feature grids but far fewer per step - a dialog
step isn't the place for all 11 how-it-works cards), then a "Get started"
step pointing at the checklist already on the page below. Back/Next
buttons, dot indicator, a Skip (✕) button, and native dialog affordances
(Escape, backdrop click) all close it.

**First new modal pattern in this codebase** - native `<dialog>` chosen
specifically because nothing here had to be hand-built: `.showModal()`
gets focus-trapping, Escape-to-close, and `::backdrop` dimming for free.

**Trigger, deliberately different shape from Phase 3's**: this is
per-person orientation, not per-org configuration state, so it doesn't
consult any org data at all - auto-opens once per *browser*
(`localStorage` `onboardingTourSeen`, checked entirely client-side in
`scheduling.js`, no server involvement) regardless of whether the org has
anything configured yet. A new admin joining an already-set-up org still
gets it, which is the same "even an old account's new adopter should be
able to see this" principle Derek asked for on Phase 2, applied to a
different mechanism here since there's no org-level data driving this
one.

**Manual replay**: `/?tour=1` (read client-side only, via a regex against
`location.search` - no `home.go` change needed, unlike Phase 2's
server-side `?onboarding=1`) linked from a new "Getting started" section
on the Account page - same permanent-findable-home role
`AccountInstallBody` plays for the install banner, brought back
specifically for the tour (Phase 3 removed the org-checklist-flavored
version of this same idea since there was nothing left to replay once the
checklist became permanent).

**Fails closed on blocked storage, not open** - a deliberate exception to
this file's usual localStorage convention (theme, site banner, install
prompt all fail open: show it, cost nothing). A full-screen dialog
reopening on *every* home page load for someone whose browser blocks
storage would be a real annoyance, not a minor miss, so a thrown
`localStorage` read simply skips auto-opening rather than defaulting to
show. The manual `?tour=1` path is unaffected either way, since it never
touches `localStorage`.

## Marketing-copy coverage audit (done 2026-09-08)

Was noted as adjacent/out-of-scope, then actually done same day (Derek's
explicit ask). Read every `docs/plans/shipped/*.md` written since
`how_it_works.templ`'s feature grids were last touched, checked each
against the "genuinely earns a card" bar (a concrete before/after
scenario, not a feature-list bullet) rather than adding one for every
shipped thing:

- **Added** (all real, live, guardrail-shaped): "Gives an old favorite
  room to come back" (the not-sung-in-a-while nudge,
  `docs/plans/shipped/song-archive-plan.md`) and "Resurfaces the fix you
  already found" (transition notes,
  `docs/plans/shipped/transition-notes-plan.md`) to "Protect the
  service"; "Makes clear who's actually leading" (song leader,
  `docs/plans/shipped/song-leader-plan.md`, which its own "Which product
  promise" section calls primarily service-side) also to "Protect the
  service"; "Makes 'can they hit this note' a lookup" (person vocal
  range) to "Protect the team", matching
  `docs/plans/in-progress/vocal-range-plan.md`'s own promise split.
- **Deliberately skipped**: song lead sheet attachments and the song
  archive action itself - real, shipped, useful, but conveniences, not
  guardrails (no before/after "catches a problem" scenario), so they
  don't fit this page's format without stretching. Also skipped: a
  service-side **song vocal range mismatch warning** card - checked
  against `docs/plans/in-progress/vocal-range-plan.md`'s own status line
  and confirmed **Phase 3 (the actual mismatch warning) hasn't shipped
  yet** - only Phase 1/2 (the person/song range setup pages and the
  transposed-range display on Build) are live. Advertising an automatic
  mismatch check that doesn't exist yet would be a real overclaim, not
  just an aggressive one - worth watching for when Phase 3 actually
  ships, since that's exactly the kind of card this page should get then.
- `landing.templ` was checked too and left alone - its feature grid is 5
  broad *categories*, not per-scenario cards like `how_it_works.templ`;
  none of the newly-added scenarios above cleanly fit an existing
  category without stretching it, and adding a 6th category for one
  scenario apiece would unbalance a page that's deliberately kept to a
  short, categorical overview (see its own doc comment on why
  `how_it_works.templ` exists as the deeper pass).

## Proposed, not started: page-level and Build-page guidance

Derek's ask (2026-09-08): should setup pages get their own first-visit
guidance (a modal per page? walking someone through each one in
sequence?), possibly with images of how a feature surfaces on Build - and
separately, should the feature-dense Build page get its own tour - all
while explicitly not wanting to be "overbearing." Recommendation below,
not built - a real decision, not a default, per this file's own
"Suggested build order" convention of not building the heavier option
without it actually being asked for.

**Core principle: escalating disclosure, not exhaustive tours.** This
codebase already has a genuinely strong contextual-teaching mechanism -
`ConflictBadge`/`Disclosure` fire *exactly when a real warning exists*
(see CLAUDE.md's controllers section), which teaches a feature at the one
moment it's actually relevant, with real data on screen - a better
teacher than any tour step describing the same feature in the abstract
before there's anything to point at. Every proposal below is judged
against whether it fills a genuine gap that mechanism doesn't already
cover, not against "would this be nice to have."

**Per-setup-page first-visit guidance: recommend skipping, it's already
covered.** Reasons against the two options Derek raised:

- *A separate modal per setup page* (7 more dialogs) directly contradicts
  "don't want to be overbearing" - that's tour fatigue by definition, and
  each of these 7 pages is visited rarely (set up once, maybe revisited
  occasionally), not a place where repeated friction accumulates the way
  Build's daily use would justify a bigger investment.
- *Walking someone through each page in a guided sequence* forces an
  order and a time commitment most directors won't want - the checklist
  (Phase 3) already lets someone pick which 1-2 categories matter to them
  right now rather than sitting through all 7.
- What's actually needed - "what is this page for and why does it
  matter" - is already answered by Phase 1's work this same day: every
  setup page's own intro paragraph got a "why" clause, and every empty
  state names the payoff (e.g. person_relationships.templ's "add one to
  get a warning before two people who shouldn't be scheduled together end
  up on the same roster"). Adding a dismissible tip on top of copy that's
  already sitting right there, unmissable, on a page with almost nothing
  else on it, would be answering a question that's already answered.

**Images/screenshots of how a feature surfaces on Build: recommend
against, for now.** Real, not hypothetical, costs: a screenshot goes
stale silently the moment the real UI changes - no build failure, no
diagnostic, just a documentation image quietly showing an old layout
forever until someone happens to notice. Nothing in this codebase's build
pipeline re-captures or validates screenshots, so keeping one honest would
be a fully manual, easy-to-forget maintenance task layered onto every
future Build page change - the exact kind of drift `docs/pco-api-risks.md`'s
own restructuring (see CLAUDE.md's "Keeping the docs current") was
written to stop happening to *documentation*; a screenshot in *product UI*
carries the same risk with less visibility when it goes wrong. It's also a
first for this app - `how_it_works.templ`/`landing.templ` are both
text-only today - worth a deliberate decision before introducing image
assets as a maintained pattern, not something to default into while
building something else. Text description (already the house style, and
already present in Phase 1's "why" clauses) stays the lower-maintenance
choice; revisit only if actual feedback says a picture is specifically
what's missing, not as a default enhancement.

**Build-page tour: recommend yes, eventually - but hold off starting it
now.** Build is a genuinely different case from the 7 setup pages: it's
the single highest-traffic page in the app (CLAUDE.md calls it "the
combined Songs/Team build workflow" - the app's actual reason for being),
with real structural density (Songs/Team tabs, drag reorder, quick-action
rows, detail cards, publish flow) that a first-time visitor could
plausibly miss the shape of. If/when this gets built:

- Scope it to *orientation on the page's shape* - two tabs, warnings
  appear inline as you work, Publish syncs back to real Planning Center -
  not a feature-by-feature walkthrough. Individual features (a repeat
  warning, an avoid-pairing conflict) already explain themselves the
  moment they're relevant, per the core principle above; re-explaining
  them in the abstract in a tour step would be redundant with, not
  additive to, what `ConflictBadge`/`Disclosure` already do.
- Same trigger shape as the Home tour (Phase 4): once per browser via its
  own `localStorage` key (not `onboardingTourSeen` - a separate flag, e.g.
  `buildTourSeen`, since seeing one tour shouldn't silently mark the other
  seen), plus its own manual-replay query param (something like
  `?build-tour=1`, distinct from Home's `?tour=1` so the two never
  collide) linked from the same Account page "Getting started" section.
  Gate on "has this person ever opened Build," not "does this specific
  plan need it" - a returning director shouldn't get it again on a new
  plan.
- **Sequencing call, not a technical one**: don't start this next by
  default. Three onboarding surfaces will exist once this ships (tour,
  checklist, Build tour) - worth a beat to see whether the first two are
  already doing the job before adding a third, exactly the same
  "don't be overbearing" instinct Derek raised. Revisit once there's
  actual signal (a demo/onboarding call still needing to manually explain
  Build's tabs, a support question about something the checklist/tour
  should have already covered) rather than building ahead of evidence.

## Open questions

- Does Phase 1 alone measurably help, or is the nudge (Phase 2) actually
  necessary? No analytics exist in this app today to answer this
  quantitatively - would have to go by qualitative feedback (does a new
  org onboarding call/demo still need to manually point out
  relationships?) unless/until real usage tracking exists. Worth asking
  directly rather than assuming.
- Exact copy for both phases - drafts above are directional, not final;
  match `how_it_works.templ`'s voice on an actual pass rather than
  treating these as done.
- Whether the checklist (Phase 3) should also surface in an empty-Build-page
  context (e.g. first time someone opens Build with zero relationships
  configured) rather than only on Home - not built; Home-only for now,
  expanding surface is a later decision, not this pass's.
- Exact tour copy (Phase 4) - same caveat as the checklist's: drafted
  against `how_it_works.templ`'s voice, not treated as final without an
  actual read-through in the running dialog.
- Whether the tour should ever re-open itself automatically again (e.g. a
  "what's new" variant after a big feature ships) rather than staying
  purely once-per-browser plus manual replay - not built, no clear need
  yet.

## Suggested build order

Historical record of what actually happened, not a forward-looking plan
any more - everything below is built:

1. Phase 1: `home.templ`'s Setup-section regroup + intro sentences.
2. Phase 1: empty-state copy pass across the setup pages.
3. Phase 2 (nudge card): built, widened once, given a manual-replay path -
   then fully superseded by Phase 3 same day (see "Phase 2 history" above).
4. Phase 3: the seven new `HasAny*` store existence checks,
   `HomeController.buildOnboardingChecklist`, and `home.templ`'s
   checklist rendering, replacing both Phase 1's plain link list and every
   trace of Phase 2.
5. Phase 4: the `<dialog>` markup and its four content steps
   (`home.templ`'s `onboardingTour`), `scheduling.js`'s open/close/
   step-navigation functions plus the two `DOMContentLoaded` listeners
   (auto-open, close-records-dismissal), `home.css`'s `.tour-*` rules, and
   the Account page's "Getting started" section linking `/?tour=1`.
6. Phase 4 revision: `.tour-actions-nav` grouping, Next demoted off
   `.primary`, "Got it, thanks!" copy, the always-visible Account-page
   pointer line.
7. Marketing-copy audit: four new `how_it_works.templ` cards (three
   "Protect the service", one "Protect the team" - see that section above
   for exactly which and why), `landing.templ` deliberately left alone.

## Verification

- `go build ./...` and `go test ./... -count=1` both clean as of the
  2026-09-08 build - not yet spot-checked live in the running app (nothing
  in this doc's own scope has been deployed).
- Still worth a live check once deployed: the checklist's `Done`/`Not set
  up` badges match reality for a real org (add a song relationship, confirm
  its badge flips and the "N of M" count increments on next load, no page
  needed to be told this changed); Position Preferences never shows a
  badge either way; the Song Tag Suggestions `<li>` still only appears
  inside "Protect the service" while `SuggestionCount > 0`; no layout
  shift on `/` or any touched setup page's empty state (reusing
  `.plan-list`/`.muted`/`components.Badge` throughout - no new CSS beyond
  what already existed).
- Also worth a live check for the tour: opens automatically on first visit
  to `/` in a fresh browser profile (or after clearing `localStorage`),
  Back/Next/dots track the right step, Escape and backdrop-click both
  close it and record the dismissal (reload `/` afterward and confirm it
  does *not* reopen), `/?tour=1` reopens it regardless of that recorded
  dismissal, and the Account page's "Replay the intro tour" link actually
  reaches `/?tour=1`. Plus, after the revision: Back and Next visually sit
  together as a pair, Next isn't accent-colored on any step, "Got it,
  thanks!" is the only accent-colored button and only shows on the last
  step, and the "Find it on your Account page" line is visible on every
  step, not just the last one.
- Marketing copy: read `/how-it-works` and confirm the four new cards
  render in the right grid, in the right voice, and don't read as padding
  next to the existing ones.
