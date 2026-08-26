# Feature ideas

Unvetted brainstorm of "problems Planning Center doesn't solve" in the same
vein as the relationship/repeat/reinforcement features that already exist -
not a commitment, not sequenced, and nothing here has a plan yet. Grounded
against what's actually available where noted; promote an idea to the
ROADMAP once it's actually being considered for real, and delete it from
here.

## Songs

- **Service length warning** - every plan item already carries a `Length`
  (see `pco.ItemAttributes.Length`, fetched for free alongside everything
  else in `ServiceOrder`). Sum it per plan and flag when a service runs
  noticeably longer/shorter than that service type's recent average, before
  publish rather than after rehearsal runs long.
- **Key-transition awkwardness** - items also carry `KeyName`
  (`pco.ItemAttributes.KeyName`). Flag two adjacent songs a jarring interval
  apart (a cold key change with no transition), or a set that's all one
  key/tempo family and could use contrast. Same "free, already-fetched"
  shape as the reinforcement feature.
- **Lead-time warning for new/complex songs** - the flip side of the
  reinforcement feature just built: if a `tracked_new_songs` song (or one
  tagged "hard") gets added to a plan less than N days before the service
  date, warn that the team may not have time to learn it. Reuses the table
  added for reinforcement.
- **CCLI/reporting completeness** - flag a song missing CCLI number/author
  before publish, so licensing reports don't silently have gaps. Needs
  checking what `pco.SongData` actually exposes for CCLI fields before
  promising this - not yet verified.

## People

- **First-time-in-this-position pairing** - mirrors the new-song bootstrap
  trick: the first time someone's assigned to a position with no prior
  history there (via the same PCO history call `servingFrequency` already
  makes - see `pco.GetPersonPlanPeople` in rosters.go), auto-suggest pairing
  them with a mentor for that position from existing `mentorship`
  relationships. Mentorship suggestions exist today but nothing detects
  "this is actually their first time," so the nudge fires generically
  instead of exactly when it matters most. Costs a PCO call per assignment,
  same cost tier as today's serving-frequency check - not free like the
  song-side ideas above.
- **Cross-team burnout** - `schedule_preference`/frequency warnings today
  are scoped per position. Someone could be within preference on vocals but
  also serving photography and greeting the same week - no aggregate "how
  much is this person actually doing this month" view exists yet.
- **Thin-bench warning** - flag a position whose eligible-and-actually-used
  pool has shrunk to 1-2 people, as an early flight-risk signal rather than
  discovering it the week nobody's available.

## Service-level

- **Prep-lead-time for role-tagged items** - since song roles already model
  communion/baptism generically (no code change to add a new role), a
  "needs N days' notice" flag on those roles could warn if communion is
  added to a plan too close to the date for elements to be ordered.

## Rough leverage-to-cost read

Service length + key transitions are the cheapest (zero new PCO calls, same
items already fetched for `ServiceOrder`) and closest cousins to the
reinforcement feature just shipped. First-time-position pairing is probably
the most valuable but costs a PCO call per assignment, same tier as today's
serving-frequency check.
