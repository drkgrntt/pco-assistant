# Feature ideas

Unvetted brainstorm of "problems Planning Center doesn't solve" in the same
vein as the relationship/repeat/reinforcement features that already exist -
not a commitment, not sequenced, and nothing here has a plan yet. Grounded
against what's actually available where noted; promote an idea to the
ROADMAP once it's actually being considered for real, and delete it from
here.

## Planning annotations

What a worship leader might want to attach to a song while actually
planning, beyond what's there today - split by whether PCO already has
somewhere to put it.

**Already in PCO's data model, just not surfaced in this app:**

- **Per-service item notes** - `pco.CreateItemParams`/`UpdateItemParams`
  already have a writable `Description` field, and this app already calls
  both when publishing - it's just never set. A note tied to *this one
  occurrence* of the song ("cut the bridge this week," "new keys player,
  go slow"), not the song forever. The cheapest idea in this whole file:
  no SDK work, just a text field on the set builder and one more param on
  publish.
- **Arrangement notes** - `Arrangement.Notes` is likewise already
  writable (`CreateArrangementParams`/`UpdateArrangementParams` both have
  it), and the arrangement is already fetched for key/tempo (see Song
  Info). A persistent note tied to the arrangement itself ("acoustic
  version, no drums," "we usually do the bridge twice") - same cheapness
  as item notes, just read+display+edit instead of write-only.
- **Song structure** - `Arrangement.Sequence`/`SequenceShort` ("Intro,
  Verse 1, Chorus..." / "Intro×2, V1, C, V2, C, Bridge, C, Outro") -
  already fetched alongside key/tempo, currently discarded. Read-only in
  the SDK today (not in either Create/UpdateArrangementParams), but
  useful for flow/pacing at a glance without opening PCO.
- **Whether a chord chart exists** - `Arrangement.HasChordChart` - a
  simple "has chart" badge linking into PCO, so it's known before
  rehearsal whether one's even there.
- **A lightweight reference link** - PCO's Attachment resource supports
  `linked_url`/`remote_link` (just a URL, no file upload) - "here's the
  recording to learn this from." Much cheaper than the full file-upload
  design considered (and declined) for the Song Info feature - this piece
  alone doesn't need any of that machinery.

**Not in PCO's data model at all - would need local storage, same
pattern as `song_tags`/`song_relationships`:**

- **Capo notes** - confirmed this session: PCO has zero API field for
  capo anywhere. A local note ("capo 2, shapes of G") is the only way to
  capture it at all.
- **Instrumentation/gear needs** - "needs a cajon," "no electric this
  week." Already possible with *zero code changes* today -
  `song_tag_categories` is fully dynamic (see README), so this could just
  be a new tag category added from the setup page right now, no feature
  build required.
- **Who typically leads this song** - distinct from the existing
  preferred/discouraged binary; more like "this is Sarah's song."

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
  reinforcement feature already built: if a `tracked_new_songs` song (or one
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

Item notes and arrangement notes (Planning annotations, above) are the
cheapest ideas in this whole file - both fields already exist and are
already writable in the SDK, so this is display/edit UI, not new plumbing.
Service length + key transitions are next (zero new PCO calls, same items
already fetched for `ServiceOrder`) and are the closest cousins to the
reinforcement feature already shipped. First-time-position pairing is
probably the most valuable of the rest but costs a PCO call per
assignment, same tier as today's serving-frequency check.
