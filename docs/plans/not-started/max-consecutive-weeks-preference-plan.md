# Plan: "no more than N weeks in a row" scheduling preference

Not built yet - written up for review before anyone picks it up. Prompted
by direct feedback in `docs/issues.md`:

> How difficult would it be to extend person preferences, which is
> already basically just 1 for 1 with PCO, to allow for more diva-like
> preferences? For example, for a time, my preference was not more than 3
> times per month, and not more than 2 in a row.

## Current state

The two examples in the issue turn out to be very differently sized:

- **"Not more than 3 times a month" already exists, natively, today - zero
  work.** PCO's own `schedule_preference` field
  (`PersonTeamPositionAssignmentAttributes.SchedulePreference` in go-pco)
  already includes a `"Three times a month"` value
  (`pco.SchedulePreferenceThreeTimesAMonth`), and this app already reads
  and writes it straight through to PCO
  (`schedulePreferenceOptions`/`setPersonTeamPositionPreference`,
  `internal/server/controllers/people.go`, ~L286/~L339 - a
  straight-to-PCO-immediately field per CLAUDE.md's Sync model section,
  confirmed by `internal/views/home_types.go`'s own doc comment ~L66-71).
  `frequencyWarning`/`perMonthFor` (`internal/server/controllers/
  rosters_warnings.go`) already implement the check against it, reading
  `otherAssignmentDates` from the locally-cached
  `person_team_plan_history` (`person_data_cache.go`, refreshed via
  background sync, never a live PCO call at render time).
- **"Not more than 2 in a row" has no equivalent anywhere - genuinely new
  work.** PCO's `schedule_preference` enum has no consecutive-run concept
  at all (it's `Unavailable` / `Every [N] week(s)` / `[N] times a month` /
  `Once a quarter` / `Choose Weeks` - confirmed against the full enum in
  go-pco's `personTeamPositionAssignments.go`), and nothing in this app's
  own local schema shadows or extends it either - `schedule_preference`
  lives entirely in PCO with no local table alongside it. The closest
  analog in this codebase is on the *song* side (two-in-a-row/three-in-a-
  row repeat warnings, `set_types.go`/`sets_builder.go`), not anything
  person/position-scoped.

## Why the second half is new work, not a tweak

Unlike the frequency-cap half (already a PCO field this app just reads),
a consecutive-run cap needs building from scratch:

1. **New local storage.** Most natural fit: a new table scoped per
   (person, team, position) the same way `schedule_preference` itself is
   scoped - e.g. a small integer "max consecutive weeks" (or framed as
   "then requires a week off," same idea). This is a genuinely new table,
   not an extra column on an existing one - confirmed there's no existing
   local table shadowing `schedule_preference` to extend instead.
2. **A new warning function mirroring `frequencyWarning`'s shape** - walk
   `otherAssignmentDates` for that person/position, find the longest run
   of consecutive *scheduled* occurrences touching the date being
   checked, compare against the stored cap.
   - **Real design fork, not a detail**: does "consecutive" mean literal
     back-to-back calendar weeks, or consecutive occurrences of *that
     team's own service cadence*? A team that meets biweekly serving "2 in
     a row" almost certainly means two consecutive biweekly slots, not
     two calendar weeks 14 days apart - this needs an explicit answer
     before the math can be written, the same way `everyXWeeksFor`
     already had to pick a concrete interpretation for "once a quarter."
3. **UI** - a new input alongside the existing `schedule_preference`
   dropdown (`position_preferences.templ`/`people.go`), wired through the
   same warning-surfacing call sites `FrequencyWarning`/
   `BlockoutWarning`/`CrossTeamWarning` already flow through today
   (`rosters_builder.go`, `rosters_scoped.go`, `roster_builder.templ`,
   `person_detail_card.templ`).
4. **A migration** for the new table.

## Sizing

Comparable to an already-shipped local-only preference feature (e.g.
`person_relationships`' `works_well`/`avoid_pairing`/`mentorship`, or
`song_person_preferences`) - a new migration, new `Store` methods, one new
warning function, and UI wiring across 2-3 templates. This app already
has the exact "local preference layered on top of a PCO-mirrored value"
shape once - `schedule_preference` is the PCO half, this would be the
first local-extension half of that same idea - so it's well-precedented,
medium-sized work, not a quick tweak and not a novel architecture either.

## Which True North promise this serves

Protects the team - directly a burnout/preference-respecting feature, the
same promise `FrequencyWarning`/`BlockoutWarning` already serve. A
reasonable roadmap candidate on that basis alone once prioritized.

## Open questions

- **Storage shape** - confirmed above this needs a genuinely new table,
  not an extra column on something existing.
- **"Consecutive" definition** (raw calendar weeks vs. that team's own
  service cadence) - needs a real answer before the warning function's
  math can be written.
- **Scope of ambition** - the issue's phrasing ("more diva-like
  preferences," plural, with two examples) hints at possibly wanting an
  open-ended rule set over time, vs. one concrete new check the same
  shape as `frequencyWarning`. Recommend the latter for a first pass -
  `frequencyWarning` itself started as one concrete check, not a generic
  rule engine - and revisiting only if a third or fourth distinct rule
  shape actually gets requested later.

## Suggested build order

1. Settle the two open questions above with the user.
2. Migration + `Store` methods.
3. New warning function (mirrors `frequencyWarning`'s shape and tests).
4. UI wiring (position-preferences input + roster builder/candidate
   surfaces).

Self-contained enough to be a `general-purpose (isolation: worktree)`
build once step 1 is answered, per CLAUDE.md's delegation guidance.

## Verification (once built)

- `go build ./...` (empty output = success), `make test` - new tests
  mirroring `frequencyWarning`'s existing test coverage: a run under the
  cap doesn't warn, a run at/over the cap does, an unset cap never warns.
- Live: set a 2-in-a-row cap on a real position preference in a dev org,
  confirm the warning fires when building a roster for the 3rd
  consecutive occurrence and not the 1st/2nd.
