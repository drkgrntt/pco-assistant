# Ideas & product notes

This used to be two files - a ROADMAP.md for product/business thinking and
this file for an unvetted feature brainstorm. Consolidated into one: the
first few sections below are the settled-or-still-open product thinking
that used to live in ROADMAP, the rest is the original feature brainstorm.
Update the product-thinking sections as decisions actually get made, and
prune anything that becomes fully settled *and* already documented
elsewhere (the README, the code itself) rather than leaving it stale here.
The brainstorm sections stay what they always were: not a commitment, not
sequenced, nothing here has a plan yet - grounded against what's actually
available where noted, and deleted once it's actually being built.

## Vision / positioning

PCO Assistant is a **supplement** to Planning Center Services, not a
replacement - it only exists because of the gaps PCO leaves for worship
scheduling: song relationships, avoid-pairing/mentorship-aware team
building, repeat-song and burnout warnings, before-you-publish conflict
detection. See the README for the current feature set.

**True North**: turning the relational and musical context a director
already carries in their head (or a sticky note) into instant,
before-you-publish guardrails. Every feature should serve one of two
promises:

- **Protect the team** - prevent burnout, respect stated preferences,
  avoid known relational friction, encourage mentorship pairing.
- **Protect the service** - prevent song-repetition fatigue, catch a
  broken set flow, surface conflicts before the plan goes out, not after.

PCO is the system of record - it stores what's already decided. This app
is where the decision actually gets made: nothing here replaces PCO, it
just means PCO never sees the set/roster until it's already been checked
against everything PCO itself has no way to know.

The wedge is worship teams specifically, not "volunteer scheduling" in
general - see [Expansion strategy](#expansion-strategy) for why, and why
that's a sequencing choice, not a ceiling.

Pricing (seat-based, hybrid tiers) is decided and live - see the README's
Setup section for the current tiers/prices.

## Reducing friction vs. Planning Center

The core adoption risk: this is a second tab, a second login, a second
place to check - no matter how good the features are, that friction
competes with just staying in PCO. Ideas below, roughly ordered by
leverage vs. build effort, not commitment order. (Deep linking - the
lowest-effort item on this list - already shipped: see `pcoPlanURL` and
the builder page's "Open in Planning Center" link.)

1. **Live sync reflection.** Right now sync is one-directional at publish
   time (this app → PCO). If someone edits a plan directly in PCO while a
   draft is open here, nothing reflects that until the next load. PCO
   webhooks + htmx's existing swap model could close that loop so the app
   never shows stale-vs-PCO state.
2. **Pre-publish nudges.** Automated summary 24-48h before a service -
   open positions remaining, unresolved song-pairing conflicts - via email
   or a Slack/Discord webhook, so checking this app isn't something someone
   has to remember to do. `jobs.PCOJobs.Register` is already an empty
   placeholder scheduler waiting for exactly this kind of job.
3. **Narrow/compact side-by-side layout.** Already installable as a PWA
   (see README) and already mobile-first responsive, but not yet
   deliberately tuned for a true ~350px floating-sidebar width - worth
   doing so it's usable beside a PCO tab, not just full-width or
   full-mobile.
4. **Browser extension HUD.** Highest effort, highest ceiling: inject a
   panel directly into `services.planningcenteronline.com/plans/*` showing
   this app's warnings (conflicts, repeats, blockout collisions) as an
   overlay on PCO's own UI, so the native PCO drag-and-drop workflow never
   has to leave PCO at all. A real product bet, not a quick add - decide
   this only after the lighter-weight items above are in and adoption data
   says it's worth it (also see Open questions below).

**Framing that's live in onboarding copy:** PCO is the live broadcast;
this app is the private rehearsal studio - a safe place to test lineups
and resolve conflicts before pushing the final version live. It's the
centerpiece of the public landing page at `/` (`internal/views/landing.templ`).

## Pricing follow-ups

The pricing model itself (hybrid seats) is decided and live - see the
README. One piece of it never got built:

- **"Pay what you can" path** - a quiet option for church plants (<2 years
  old) or financially struggling churches: a low-key link on the pricing
  page, not an application process - email and get a free account, no
  interrogation. Not yet implemented anywhere in the app.

## Expansion strategy

**Prove the worship-team wedge first.** Worship leaders are the
tightest-knit subculture in a church - they compare notes with peers at
other churches and feel song-repetition/team-chemistry pain acutely, which
makes for a clean, undeniable pitch. Trying to sell a generic
"volunteer-ops platform" to every ministry head at once on day one muddies
the message and multiplies who has to say yes.

**But the underlying data model is already ministry-agnostic** - see
`internal/scheduling/person_relationships.go`, teams, and positions in the
scheduling package: `works_well`/`mentorship`/`avoid_pairing` tags, position
preferences, and serving-frequency warnings don't know anything about
"worship" specifically.
Kids' ministry, student ministry, and production leads fight the exact same
scheduling-fatigue and mentorship-pairing problems. Once worship
directors are bought in, expanding to another ministry should mean
unlocking a view for that department's teams/positions - not rewriting the
engine underneath it.

Sequencing: **Phase 1** - worship only, prove people actually adopt and pay
for this. **Phase 2** - once that's proven, open department-specific views
for other volunteer-heavy ministries on the same backend.

## Open questions

- Whether the browser-extension HUD is worth building at all before there's
  real usage data suggesting the lighter integrations aren't enough.
- When Phase 2 (non-worship ministries) actually starts - deliberately not
  scheduled yet; the plan is "after worship PMF," not a fixed date.

## Planning annotations

What a worship leader might want to attach to a song while actually
planning, beyond what's there today - split by whether PCO already has
somewhere to put it.

**Already in PCO's data model, just not surfaced in this app:**

- ~~Per-service item notes~~ - **shipped**, as "This week's note" on the
  song detail card (local per-occurrence, reaches Planning Center as that
  item's Description on the next Publish/Refresh). See
  `SetSongView.Description`, `UpdateSetSongDescription`.
- ~~Arrangement notes~~ - built, then replaced before shipping: turned out
  Arrangement.Notes isn't the notes surface anyone actually looks at in
  Planning Center - what people mean by "song notes" in the Plan view is
  a different resource entirely (see the next bullet).
- ~~Item Notes (Audio/Visual, Band, Vocals, etc.)~~ - **shipped.** Real,
  categorized per-item notes (`pco.ItemNoteCategory`/`ItemNote`, go-pco
  v0.14.0) - one box per category on a published song's detail card, each
  saving straight to Planning Center immediately. This is what actually
  shows up in Planning Center's own Plan view; Arrangement.Notes (above)
  didn't. See `SetSongView.Notes`/`SongNoteView`, `buildSongNoteViews`,
  `UpdateSongNote`.
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
- **Visual flow/energy curve** - a step further than the key-transition
  warning above: plot each song's tempo (already cached - see Song Info)
  as a simple curve across the set, so a jarring energy drop (e.g. a fast
  opener straight into a slow ballad with nothing between them) is visible
  at a glance while dragging songs into place, not just flagged after the
  fact in text. Same underlying data as the warning above, just a
  different (and more involved) presentation of it - worth doing only if
  the text warning alone doesn't feel like enough once that ships.
- **Lead-time warning for new/complex songs** - the flip side of the
  reinforcement feature already built: if a `tracked_new_songs` song (or one
  tagged "hard") gets added to a plan less than N days before the service
  date, warn that the team may not have time to learn it. Reuses the table
  added for reinforcement.
- **Rehearsal complexity budget** - a step past the lead-time warning above:
  instead of flagging one new/hard song added too close to the date, score
  a whole set's rehearsal load (count of `tracked_new_songs`/hard-tagged
  songs, key changes, first-time lead/pairing on a song) against how much
  rehearsal time is actually available, and flag when the set is asking for
  more than the slot has - "~85 min of rehearsal load for a 60 min slot."
  Every input already exists somewhere in this file's data (hard tag,
  tracked_new_songs, key/tempo); the actual gap is a rehearsal-length field
  to check against (service type? set-level?) and a scoring formula, which
  is a calibration problem, not a plumbing one.
- **CCLI/reporting completeness** - flag a song missing CCLI number/author
  before publish, so licensing reports don't silently have gaps. Needs
  checking what `pco.SongData` actually exposes for CCLI fields before
  promising this - not yet verified.
- **Retired / "don't resurrect" flag** - distinct from `avoid_pairing` (which
  is about *pairing*, not benching a song entirely). Every song list picks
  up a handful the team quietly agreed to stop doing - "never landed," "too
  hard live," "dated." The flag itself is close to free (a new
  `song_tag_categories` row, e.g. `status`/`retired`, same fully-dynamic
  mechanism as the instrumentation idea above), but `song_tags` has no notes
  column, so capturing *why* it was retired needs either a small schema
  addition or a new table with notes, not a free ride on tags alone.
- **Per-person key fit** - "this key works for Sarah, too low for Mike."
  A different axis than the existing preferred/discouraged binary (that's
  about *wanting* to sing it, not *being able to* in that arrangement's
  key). Nearly free: `song_person_preferences` already carries a `Notes`
  field alongside its dynamic `preference` type (`song_preference_types`,
  migration 00015) - a new preference code (e.g. `key_note`) gets the
  free-text note for free, no schema change. The payoff needs one more
  piece to actually fire when it matters, though: surface that note right
  next to the arrangement's key the moment someone gets dragged onto lead
  for a song, not just on their profile somewhere. Free text alone can only
  ever be *shown*, not checked - flagging an actual mismatch ("Sarah leads,
  arrangement's in C, her note says A") needs the note to be a structured
  key rather than prose, worth doing only if surfacing alone doesn't feel
  like enough once that ships.
- **Occasion reservation** (Christmas Eve only, baptism/communion-specific) -
  largely already possible today: `song_roles` already has `communion`/
  `baptism` roles (migration 00020), and `song_tag_categories` already
  ships a general-purpose tagging mechanism. The actual gap is a *warning*,
  not storage - flag when a role/occasion-tagged song hasn't shown up at a
  matching service in a while, or turns up somewhere it shouldn't (e.g. a
  Christmas Eve tag in July).

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
- **Untested/long-since-paired suggestion** - today's works-well/mentorship
  suggestions only fire for pairs that *already* have a relationship row
  recorded. Nothing prompts for two people who've simply never served
  together, or haven't in 90+ days, when there's no existing relationship
  to key off of at all. Would need a scan of past assignment history per
  position (same shape as `otherAssignmentDates` in rosters.go) to find
  pairs with zero/stale overlap, then surface them as an "untested
  pairing" suggestion - a genuinely different signal (novelty/coverage)
  from mentorship or works-well (quality of an already-known pairing).
- ~~Cross-team burnout~~ - **shipped.** A warning on the Team roster
  builder flags when someone's already filling another position/team in
  the same Mon-Sun week, alongside the existing per-position frequency/
  blockout warnings. See `crossTeamWarning`/`crossTeamAssignmentsThisWeek`
  in `rosters.go`.
- **Thin-bench warning** - flag a position whose eligible-and-actually-used
  pool has shrunk to 1-2 people, as an early flight-risk signal rather than
  discovering it the week nobody's available.
- **Admin-only pastoral/season-of-life flag** - "go easy on scheduling this
  month," recently had a baby, walking through something hard. Worth being
  more than a go-easy binary: a specific soft-capacity restriction ("BGVs
  only through October") the app can actually act on - a gentle nudge when
  someone carrying that flag gets dragged onto a lead role, not just a note
  someone has to remember to go read. Needs new local storage and, more
  importantly, real thought on visibility - this has to be scoped strictly
  to scheduling admins, unlike everything else here that mirrors PCO's more
  broadly-visible notes.
- **Ranked "call first" sub list per position** - separate from formal PCO
  eligibility, which says who *can* fill a position, not who to try *first*
  when someone cancels Saturday night. Not a person-to-person relationship
  like `works_well`/`mentorship`, so it doesn't fit the existing
  `person_relationships` pattern - needs a new person+position+rank table.
- **Off-book flexibility inventory** - "can play acoustic or keys in a
  pinch," beyond whatever positions someone's formally eligible for in PCO.
  Matters most exactly when the ranked sub list above gets used - last-minute
  gap-filling. New local storage, no existing table to piggyback on.
- **Growth/aspiration notes** - "wants to try leading," "ready to go solo on
  harmony." Real pipeline info that currently just evaporates between
  conversations. Simplest as a free-text field per person, not tied to any
  existing relationship/preference structure.

## Service-level

- **Prep-lead-time for role-tagged items** - since song roles already model
  communion/baptism generically (no code change to add a new role), a
  "needs N days' notice" flag on those roles could warn if communion is
  added to a plan too close to the date for elements to be ordered.
- **Recurring special-occasion calendar** - knowing in advance which
  upcoming dates are baptism/communion/guest-speaker weeks, rather than
  reacting to whatever's already in a plan. This is what would let the
  prep-lead-time warning above actually fire early enough to matter,
  instead of only after someone's added a communion item close to the
  date. New local storage - nothing today models "occasion" at the
  plan/date level, only at the song/role level.
- **Stage/gear setup notes per service type or campus** - mic count,
  in-ear mix quirks, anything that changes room-to-room. Matters most for
  the multi-campus tier the pricing model already targets. New local
  storage, no existing table to extend.
- **Stage density / band-balance check** - PCO shows a position as filled,
  not what filling it *means* musically: three electric guitarists on a
  week meant to be a stripped-back acoustic set, or two rhythm-only players
  assigned when the arrangement needs someone who can carry a lead line.
  A different axis than the instrumentation/gear tag idea above (Planning
  annotations) - that's "what the song needs," this is "what the roster
  actually adds up to." Needs two new things neither PCO nor this app
  currently model: a per-position/per-person capability tag (lead-capable
  vs. rhythm-only, or similar) and a density/style profile per service type
  or arrangement to check the roster against. More build than most items
  here - it's a new axis on both sides of the check, not a warning layered
  on data that already exists.

## Run sheet (day-of output)

Most of what a worship leader needs for the printed/PDF page carried into
rehearsal and handed to the sound booth was already tracked somewhere in
this app - order, key/tempo/length, item notes, roles, roster. Capo and
gear needs are *not* actually tracked anywhere (see Planning annotations
above) - correcting an earlier version of this section that assumed they
were.

- ~~Combined service/run sheet~~ - **shipped**, scoped to what's actually
  tracked (order, key/tempo/length, item notes, roles, roster/open spots -
  not capo or gear needs, since neither exists in this app's data model).
  A one-click printable page (Print button, no PDF library - the
  browser's own print-to-PDF covers it), linked from the Build page. See
  `internal/server/controllers/run_sheet.go`,
  `internal/views/scheduling/run_sheet.templ`. Gets more valuable as the
  song-level ideas above (key fit, retired flag) land, since it becomes
  the one place they all surface together.
- **Day-of ephemeral note** - "running 10 late," "swapped bass last
  minute." Distinct from the persistent item/arrangement notes above -
  this is throwaway-after-Sunday info that currently lives in a text
  thread or nowhere. Smallest new storage in this file: one note field
  tied to the local `sets` row.
- **Post-service debrief log** - distinct from the ephemeral note above,
  which is throwaway; this is meant to be found again. A 60-second
  post-Sunday note ("the bridge in Gratitude ran long," "transition out of
  communion felt abrupt") tagged to a song or a specific song-to-song
  transition, that resurfaces automatically the next time that song (or
  that pairing back-to-back) shows up in a draft set - so the same mistake
  doesn't get repeated three months later because nobody remembered the
  conversation. Needs a new table (note text plus what it's keyed to - a
  song id, or a song-pair for transition-specific notes) and, for the
  pairing case, matching logic that recognizes "these two songs are
  adjacent again" while building a set.

## Rough leverage-to-cost read

Item notes and arrangement notes (Planning annotations, above) were the
cheapest ideas in this whole file, and are now shipped - both fields
already existed and were already writable in the SDK, so it was
display/edit UI, not new plumbing. Service length + key transitions are
next (zero new PCO calls, same items already fetched for `ServiceOrder`)
and are the closest cousins to the reinforcement feature already shipped.
First-time-position pairing is
probably the most valuable of the rest but costs a PCO call per
assignment, same tier as today's serving-frequency check.

Of the later additions, the **run sheet** was the standout - no new
storage, just a view over data that already exists, directly replacing
the paper artifact a worship leader builds by hand - and is now shipped,
same as **cross-team burnout** below on the People side (one new PCO call
per assigned/candidate person, same cost tier as the existing frequency/
blockout checks). **Per-person key fit** is nearly as cheap - `song_person_preferences` already has a
free-text `notes` column sitting next to its dynamic preference type, so a
new preference code is most of the work. **Occasion reservation** barely
needs building at all today, since `song_roles`/`song_tag_categories`
already model most of it - the gap is a warning, not storage. Everything
on the People side added above (pastoral flag, call-first list, flexibility
inventory, growth notes) is genuinely new local storage with no existing
table to lean on, and the pastoral flag specifically needs a visibility
decision before it needs any code.

A later round of additions splits the same way. **Surfacing per-person key
fit at assignment time** is nearly free, same as the preference row it
extends - it's UI over data described above, not new plumbing, unless
auto-flagging a mismatch turns out to matter enough to warrant a
structured key field over free text. **Rehearsal complexity budget** sits
in the middle: every input it'd sum from already exists elsewhere in this
file, so the real cost is designing the scoring formula and adding a
rehearsal-length field, not fetching new data. **Stage density/band-balance
checking** and the **post-service debrief log** are the priciest of the
group - density needs a genuinely new capability/profile axis on both
people and services with nothing to extend, and the debrief log needs new
storage plus adjacency-matching logic to resurface a note at the right
moment, not just store it.
