# Plan: dismissable warnings on the Build page

Not built yet - written up for review before anyone picks it up. Prompted
by direct feedback in `docs/issues.md`: "Can we make warnings on songs
dismissable? Like, if you click it, it pulls up a modal saying the issue,
and you can choose to click 'Dismiss' or not, and if you dismiss it, maybe
it's just a faded bubble with the warning icon where you can hover or
click to see what those warnings were and/or re-enable them."

## Current state

`components.ConflictBadge(message string)` (`internal/views/components/badge.templ`)
is a plain, stateless `⚠ { message }` span - no id, no click handler, no
per-instance identity at all. It's used in two families of place today:

- **Songs tab** (`set_builder.templ`, both the in-set rows and the
  candidate list): "avoid pairing with X" (`ConflictTitles`, computed by
  `ExistingConflicts`) and "sung in each of the last two plans"
  (`RecentlyRepeated`, computed by `RepeatWarnings`) - both in
  `internal/server/controllers/sets_builder.go`. The Phase 3 vocal-range
  mismatch badge (docs/plans/shipped/vocal-range-plan.md, being built
  now alongside this doc) will be a third member of this same family, same
  file.
- **Team tab** (`roster_builder.templ`, `person_detail_card.templ`):
  `FrequencyWarning`/`BlockoutWarning`/`CrossTeamWarning` per assigned
  person - a structurally similar but separately-computed family in
  `rosters_candidate_warnings.go`/wherever `AssignedPersonView` gets built.

None of these carry a stable per-instance id today - they're strings,
recomputed fresh on every render from live PCO + local data, with no
existing concept of "this specific warning, dismissed." The issue's own
wording ("warnings on songs") points at the Songs-tab family specifically,
but the Team-tab family is structurally identical and a user reading "why
does this song's warning stay dismissable but this person's doesn't" would
have a fair question - see "Open questions" below on scope.

The closest existing "dismiss" precedent in this app is the install-prompt
banner (`installPromptRecordDismissal`, `web/static/js/install-prompt.js`)
- but that's deliberately per-viewer `localStorage`, not shared state
(dismissing it on your phone shouldn't dismiss it for your desktop, let
alone for a teammate). A warning dismissal is the opposite: if one person
on the team looks at a conflict and decides "we're fine with this
repetition, ignore it," everyone building this same plan should see that
same faded/re-enable-able state, not just the person who clicked Dismiss.
That means this needs real backend storage
(`scheduling.Store`/Postgres), not `localStorage` - a materially bigger
piece than the install banner's client-only precedent.

## Why this needs real design decisions before it can be built

Every one of these is a genuine fork, not a detail to fill in while
coding:

- **Scope of a dismissal - per plan, or permanent for that song/pairing?**
  "Avoid pairing with X" is a property of the *song pair*, independent of
  which plan it shows up on - dismissing it once probably should mean
  "stop warning me about this pairing, anywhere," which argues for
  storage keyed on (organization, warning-kind, and whatever identifies
  the underlying thing - e.g. the two song ids). "Sung in each of the
  last two plans" and the vocal-range mismatch, by contrast, are
  properties of *this specific occurrence* (which plan, which roster) -
  dismissing "sung twice in a row" on this plan shouldn't silently
  suppress the same warning appearing (correctly) on some future plan
  with a genuinely different repeat. These two shapes need different
  dismissal keys, not one generic "song id + warning text" row. This is
  the single biggest open question and should be settled first, since it
  drives the storage schema.
- **Which warning families does this cover?** Songs tab only (matches the
  issue's literal wording), or Songs + Team tab both (matches "this
  should feel like one consistent feature," not "some warnings are
  dismissable and some inexplicably aren't")? Recommend starting Songs-tab
  only for a first pass (smaller, matches what was actually asked), with
  the storage/UI shape built so Team-tab warnings are a straightforward
  second pass rather than a redesign - but this is the user's call.
- **Does a stored dismissal ever need to expire or get invalidated?** e.g.
  if "avoid pairing with X" gets dismissed, then the underlying
  `song_relationships` "avoid pairing" row between those two songs gets
  deleted entirely (so the warning would never fire again anyway) - does
  the now-orphaned dismissal row matter, or is it harmless clutter to
  leave behind? Probably harmless (next `HasAny*`-style check just never
  matches it again), but worth a conscious "yes, harmless, leave it" answer
  rather than silence.
- **The modal's content**: "pulls up a modal saying the issue" - for a
  single-message warning (most of today's) this is barely more than what
  the badge's own text already says. Worth confirming the modal earns its
  place over a simpler click-to-expand (this app already has
  `components.Disclosure` for exactly "click a toggle, see more detail" -
  `keyTransitionWarnings` uses it today for a very similar "here's why"
  popover) rather than introducing this app's first real modal dialog for
  a single sentence of text. A modal is likely to earn its keep once a
  song can carry *multiple* warnings and the ask is "review and
  dismiss/keep each one in turn" (see the Build-page chip-decluttering
  work landing alongside this doc, docs/issues.md) - one combined
  review surface rather than one modal per badge might be the more
  useful shape once several warnings are chip-collapsed together. Worth
  deciding after that decluttering work is live and its own shape is
  visible, not necessarily before.
- **The faded re-enable bubble**: needs its own small visual language
  (a muted/reduced-opacity variant of `.conflict-badge`, an icon-only
  collapsed form) and its own affordance for "show what this was" (hover
  title vs. click-to-expand) and "re-enable" (a button inside that
  expanded state). Small, but it's new UI, not a reuse of an existing
  pattern - needs an actual look at `/style-guide` conventions before
  committing to specifics.

## Sketch of one plausible shape (not a commitment - the open questions
above need real answers first)

A new `scheduling.Store` table, migration `00042_dismissed_warnings.sql`
(or similar), org-scoped, holding something like `(organization_id,
warning_kind, subject_key, dismissed_by_person_id, dismissed_at)` -
`warning_kind` distinguishing e.g. `"avoid_pairing"` /
`"recent_repeat"` / `"vocal_range_mismatch"`, `subject_key` being
whatever composite identifies the specific instance per the scope
decision above (e.g. `songA_id+":"+songB_id` for a pairing-scoped
warning, `plan_id+":"+song_id` for a per-occurrence one). `ConflictBadge`
gains an optional dismissed/dismiss-handler variant (or a sibling
component), and every warning-computing function in the
`ExistingConflicts`/`RepeatWarnings` family checks the dismissed-set
before rendering the loud badge, same shape a lot of this app's
`Has*`/lookup-map patterns already use (fetch once per render, check
inline). This is a sketch to make the open questions concrete, not a
spec - the real spec depends on answering them first.

## Suggested next step

Once the scope/schema questions above have real answers (from the user,
not guessed), this becomes a normal `general-purpose (isolation:
worktree)` build - self-contained (new migration, new Store file, a
`ConflictBadge` variant, wiring into the warning-computing functions) and
fully speccable ahead of time the way this project's own worktree-agent
candidates need to be.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`, plus new tests covering: dismissing a warning hides it,
  re-enabling brings it back, a dismissal doesn't leak across
  organizations, and (depending on the scope decision) doesn't leak across
  plans when it shouldn't.
- Live: confirm a dismissal made by one signed-in person is visible to a
  second signed-in person on the same org (this is the entire point -
  shared state, not per-viewer `localStorage`).
