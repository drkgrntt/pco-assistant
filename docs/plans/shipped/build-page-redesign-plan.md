# Plan: Build page visual language closer to Planning Center's own

**Status: shipped 2026-09-05.** This is the doc two existing code comments
(`internal/server/controllers/sets_detail.go`,
`internal/views/scheduling/set_builder.templ`) already forward-referenced
as "the build-page redesign plan" before it existed.

Written and shipped the same day, prompted by `docs/ideas.md`'s
brainstorm item #5 ("Bring the plan-page layout closer to PCO's own") and
a side-by-side comparison against fresh screenshots of PCO's own Plan
page (Order/Teams tabs, Songs list, People/Teams list).

## What the comparison found

The Build page's overall structure was already close to PCO's: an
underline-style Songs/Team tab switcher (`components/build-tabs.css`)
matching PCO's own Order/Teams/Rehearse tabs, an order-of-service table
with section-divider rows matching PCO's "PRE SERVICE / MUSIC / MESSAGE"
groupings, and a team → position grouping on the Team tab matching PCO's
Band/Vocals/instrument breakdown. Row/cell padding (`list-item.css`,
`table.css`) was already close to PCO's own density (~8px).

Three concrete gaps stood out:

1. **No avatars anywhere.** PCO shows a circular avatar (a real photo, or
   PCO's own generated colored-initials image) next to every person's
   name - on the Teams tab and the People list. This app rendered every
   person as plain text.
2. **Team tab stacked full-width.** PCO lays multiple teams (Band,
   Vocals, ...) out as side-by-side columns ("My Teams"); this app
   stacked one full-width `.team-group` card per team, meaning a lot of
   scrolling to see more than one team on a typical service.
3. **Unfilled positions were muted text only.** PCO shows an open slot as
   a small red "N Needed" pill; this app only said "(2 of 3 filled)" in
   muted gray, with nothing pulling the eye to what still needs a
   decision.

**Deliberately left alone**, per the same comparison: the Songs/Order
tab's table structure, and the Songs/Team tab bar's own styling - both
already read as close enough to PCO's equivalents that changing them
would just be motion, not a real gap closed.

## What shipped

- **Real PCO avatars**, not locally-generated initials. PCO's
  `PersonAttributes.Avatar` is a URL PCO always populates - a real photo,
  or its own generated colored-initials placeholder - so rendering it
  directly (`components.Avatar`) needed no local initials/color-hash
  logic. It rides along on the exact same batched `People#index` call
  `personNameMapForIDs` already made for names
  (`personLookupMapForIDs`, `people.go`), so this cost zero additional
  PCO calls - see `docs/pco-api-risks.md`. Wired into the Team tab's
  assignment/candidate/suggestion rows (`components.PersonListItem`,
  a `ListItem` variant) and the person-picker search dropdown used
  elsewhere in the app (`PersonSearchResults`).
- **Team tab grid.** `RosterBuilderBodyContent`'s team-group cards now
  sit in a `.team-groups-grid` (`display: grid; grid-template-columns:
  repeat(auto-fit, minmax(20rem, 1fr))`), side by side on a wide viewport
  and stacked on a narrow one - same cards, same htmx swap targets,
  just a new ancestor wrapper. Widened the Build page's own `<main>` cap
  (`main:has(.build-layout)`, `build-page.css`) from 75rem to 90rem to go
  with it - at 75rem, `.build-main` only had ~56rem left after the 15rem
  glance sidebar, room for 2 grid columns but not 3, which most services'
  team counts need.
- **"N needed" badge.** `positionHeading` now adds a small danger-toned
  `components.Badge("status-needed", ...)` pill alongside the existing
  "(X of Y filled)" text whenever a position is short - kept the text too
  (more informative for partially-filled/optional-extra positions than
  PCO's binary needed-count), just added the same visual pop PCO's red
  pill gives an at-a-glance scan.

## Addendum: 2026-09-05, functional glance card + compact rows

Same comparison exercise, one more pass after living with the grid/
avatars for a day: two more gaps against the reference screenshots.

- **"Team, at a glance" was read-only text.** PCO's own Order-tab sidebar
  "Teams" widget is a live worklist (avatars, per-team confirmed/declined/
  unconfirmed counts, a "Send" action) - ours was a static comma-joined
  name list with a single "Switch to Team" button as the only way in.
  `TeamGlanceList` (`status_summary.templ`) now renders avatars, a
  confirmed/declined/unconfirmed rollup per team (`teamStatusRollup`,
  shown only when non-zero - same "only what needs attention" idiom as
  `RosterStatusSummary`'s own chips), and every position/person as a real
  `jumpToTeamPosition(this)` button (`scheduling-build.js`) that switches to the
  Team tab, force-opens that exact position's card, and scrolls to it -
  built entirely from `AssignmentView.Status`, already on the page (it's
  PCO's own `PlanPerson.status`, imported verbatim on Revert - see
  `rosters_publish.go`). Zero new PCO calls.
- **Assignment rows repeated their own detail card.** Every row showed a
  Status badge (PCO's raw letter code, not a label), a Published badge,
  and up to three full-text conflict pills - all already shown in full on
  that same person's sidebar detail card (`personBadgeSpans`,
  `person_detail_card.templ`) once the row is clicked. Rows now skip the
  Status badge entirely for the common Confirmed case (kept for Declined/
  Unconfirmed, via a shared `statusBadge` helper that also fixed the raw-
  code display on the detail card), and collapse the conflict pills into
  one compact "N conflicts" badge whose title carries the full messages.
  Deliberately left alone: the inline remove button, and
  positionGroupCard's own expand-when-understaffed behavior.

Same-day follow-up, after living with the above for a few minutes: the
jump now leaves a brief `.jump-highlight` flash on the landed-on card
(`card.css`/`scheduling-build.js`) since a scroll alone didn't confirm landing
on the right position among several already-open ones; each assigned
person under a glance-card position now stacks on their own line
(`.glance-assigned`, `build-page.css`) instead of wrapping in a
comma-separated row, matching `PersonListItem`'s own avatar-plus-name row
shape everywhere else on the page; the glance sidebar's own flex-basis
grew from 15rem to 18rem now that `.main`'s cap is 90rem, giving those
rows room; and `positionHeading` (`roster_builder.templ`) stopped
repeating an open position's "who's on it" summary in its own heading -
that's pure duplication with the assignment-list rows already visible
right below it once a position starts expanded, and was the single
heaviest line on an open (i.e. needs-a-decision) position's card. The
eligible-candidate list stays fully visible by default on purpose (not
tucked behind its own toggle) - it's the one-click "here's who to add"
suggestion, and collapsing it would add friction to the most
action-oriented part of the page for the sake of looking shorter.

Known caveat, not new: `AssignmentView.Status` is only ever set at
Revert/import time, never refreshed after - the rollup above is exactly
as fresh as the row's own status badge already was, not a live feed.

## What didn't change, and why

- The accent color, typography, and dark-mode tokens (`tokens.css`)
  weren't touched - the goal was transferable layout/density/affordances
  a director already knows from PCO, not a reskin into PCO's own palette.
- The Songs/Order tab's table and the underline tab bar were left as-is
  (see "What the comparison found" above).
- Row hover/selection states needed no change - `.assignment-list
  li:hover`/`.selected` (`list-item.css`/`builder-rows.css`) already gave
  every person row the same "this is clickable" affordance PCO's own row
  hover does.
