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

**Naming**: "PCO Assistant" is a placeholder/working title, not a final
product name. "Resonance" is the current front-runner if/when it's time
to name the product for real, with "Preflight" and "Cadence" as other
candidates under consideration. Not decided - just noted so it isn't
re-litigated from scratch later.

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

The True North above is a marketing/positioning test - it answers *which*
features earn a place on the roadmap. **Friction reduction is not a
third promise to add to that list** - it's a separate, standing
*development* discipline that applies to *how* every feature gets built,
regardless of which promise it serves: fewer clicks, no new UI pattern to
learn where an existing one already works, no reason to leave PCO or
double-check something across two tabs when the app could just handle it.
This app only earns its second-tab, second-login tax if reaching for it
stays close to free - so friction is a question to ask at build time on
every feature ("is this the fast, obvious way to do the thing"), not a
gate on whether a feature belongs on the roadmap in the first place. See
"Reducing friction vs. Planning Center" below for where that shows up
concretely.

PCO is the system of record - it stores what's already decided. This app
is where the decision actually gets made: nothing here replaces PCO, it
just means PCO never sees the set/roster until it's already been checked
against everything PCO itself has no way to know.

A concrete implication of the friction discipline above: features that
duplicate something PCO can already do directly - setting a song's key,
building the order of service itself - aren't disqualified by that
duplication, and don't need to justify themselves as something new PCO
can't do. They're the point. The whole premise is plan here, then let
Publish hand PCO a *complete* result, so PCO can go on being the actual
logistics engine and nobody has to open PCO afterward to clean anything
up by hand. That makes these specific, PCO-mirroring actions the
highest-friction-bar features in the app, not the lowest - any gap in
what Publish actually pushes (a field this app lets someone set but
doesn't sync, a PCO state that ends up not matching what was planned)
turns "plan here" into "plan here, then also go fix it in PCO," which is
worse than not having the feature at all. See CLAUDE.md's Sync model
section for what Publish/Discard/MergeSet actually cover today.

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
   says it's worth it (also see Open questions below). Two things to design
   for whenever this is actually picked up, neither a blocker today: (a) an
   extension calling PCO live from inside the PCO tab competes for the same
   per-user-token rate budget this app's own page loads already push close
   to the ceiling on - see `docs/pco-api-risks.md` - so it needs its own
   request budget, not an assumption of headroom; (b) this app's seat-based
   billing (README) is keyed to its own OAuth session, which activity
   happening inside PCO's own tab doesn't naturally attribute to - unsolved
   design question, not just an implementation detail.
5. **Bring the plan-page layout closer to PCO's own.** Less decision
   fatigue if the layout a director already knows from PCO transfers
   directly - order of service, song rows, where actions live - rather
   than asking them to learn a second layout from scratch just to get at
   the checks this app adds on top.
6. **In-place warnings instead of a top-of-page alert list.** Key-change,
   repeat-song, and length warnings currently render as a stack of alerts
   above the set (`PlanWarnings`, `keyTransitionWarnings`/
   `serviceLengthWarning` in `sets_builder.go`). Moving each one inline -
   a tooltip/badge on the specific song row it's actually about - would
   put it where the eye already is instead of a separate list to
   cross-reference against the set below it.

**A related but distinct problem: discoverability, not friction.** A fair
amount of what actually pays off here (tag/relate suggestions, preference
notes, avoid-pairing) only helps once someone's found it - it's not on the
beaten path of "open a plan, drag songs around." Worth a lighter-weight
onboarding nudge or empty-state prompt pointing at the highest-leverage
features rather than leaving them to be discovered by accident.

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
- ~~Whether Planning Center exposes any real third-party plugin/embed
  surface (vs. just its public API)~~ - **checked**: PCO's official
  "Add-Ons" program (`api.planningcenteronline.com/docs/overview/add-ons`,
  the `@planningcenter/add-ons`/`add-ons-cli` packages) is real - OAuth
  apps with React UI rendered into PCO's web app at defined "insertion
  points," in a sandboxed remote-ui/Web Worker environment (not an iframe,
  but the same practical limit: no access to PCO's own DOM or drag-and-
  drop, can't annotate an existing item card). More importantly for us:
  the only insertion points that exist publicly today are in **People**
  (`people.profile.custom_tab`, `people.profile.info_section`) - there is
  no Services/Plans insertion point at all yet, which is the surface our
  warnings/set-builder would actually need. New insertion points are added
  via public PR against PCO's own repo, but getting an add-on itself
  approved still starts with reaching out to PCO directly per their docs.
  Net: the browser-extension HUD above remains the only currently-viable
  way to put this app's warnings on PCO's live plan UI; the "official
  embedded add-on" path isn't gated on this app's traction, it's gated on
  PCO shipping a Services insertion point they haven't announced.
- When Phase 2 (non-worship ministries) actually starts - deliberately not
  scheduled yet; the plan is "after worship PMF," not a fixed date.
- ~~A "nuclear" full-wipe-and-resync button for the Song Info / person-data
  caches~~ - **shipped**, on the new `/account` page's "Data & Caches"
  section (`internal/views/account.templ`'s `AccountCacheBody`,
  `scheduling.Store.ClearSongArrangementCache`/`ClearPersonDataCache`),
  once that page existed to put an org-wide action like this on.

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
- ~~Song structure~~ - **shipped**, on the song detail card (Build page):
  `Arrangement.Sequence`/`SequenceShort` joined into one display string by
  `formatSongStructure`, preferring the abbreviated `SequenceShort` form.
  See `SongArrangementInfo.Structure`, `SetSongView.Structure`.
- ~~Whether a chord chart exists~~ - **shipped**, as a "Chord chart" badge
  on the song detail card - `Arrangement.HasChordChart`. See
  `SongArrangementInfo.HasChordChart`, `SetSongView.HasChordChart`.
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

- ~~Service length warning~~ - **shipped.** Sums the draft's total length
  (`serviceOrderLengthSeconds` - a foreign item's own `Length`, or a set
  song's cached arrangement length) against the average of the service
  type's last two past plans (`fetchRecentPlanHistory`, which now also
  backs the existing repeat-warning check - one PCO history fetch serves
  both), and flags a difference past 15%/3 minutes as a `PlanWarnings`
  entry. See `serviceLengthWarning` in `sets_builder.go`.
- ~~Key-transition awkwardness~~ - **shipped, the "jarring interval" half.**
  Flags a half-step key change between two songs that are truly
  back-to-back (nothing between them - a header/announcement in between
  means a transition already exists there) as a `PlanWarnings` entry -
  see `keyTransitionWarnings`/`parseKeyPitchClass`. Compares the draft's own
  *effective* key (local override, falling back to the arrangement's
  cached default - same value `SetSongView.Key` already shows), not the
  raw published `KeyName`, so it reacts to an in-progress edit immediately.
  The "all one key/tempo family, needs contrast" half of this idea isn't
  built.
- **Key-change tip surfaced inline** - a step past the key-transition
  warning above: once it fires, suggest the actual move that makes the
  transition work - e.g. hit the V7 (dominant seventh) of the *next* key
  right before the change, which pulls strongly toward that key's tonic.
  Cheap: a static lookup off the same interval the warning already
  computes (`parseKeyPitchClass`), not new music-theory code - just a
  message per interval size shown alongside the existing warning.
- ~~Import/merge/revert seeding the wrong key for a multi-arrangement
  song~~ - **the display bug fixed** (a plan with real Planning Center keys
  "B A A A" was showing "B A Bb A" after a revert): `deriveImportRows`/
  `deriveMergeImportRows` (`sets_builder.go`) now seed each imported row's
  `KeyOverride` from that item's own live `KeyName` instead of leaving it
  empty, and `PublishSet`'s pending-edit push no longer sends the cached
  `ArrangementID` as a side effect of saving a Description/key edit
  (`sets_publish.go`) - see the comments there for why. **Still open,
  deliberately not tackled by that fix**: this app's whole model is "one
  arrangement per song" - `song_arrangement_cache` caches a single
  arrangement per song org-wide (`fetchAndCacheArrangementInfo`'s
  `GetArrangements(PerPage: 1)`, whichever Planning Center happens to
  return first), with no way to represent "this song has several
  arrangements and this plan uses a non-default one." A real fix would add
  a per-occurrence `ArrangementID`/`KeyID` to `set_songs` itself (populated
  from the live item on import, used instead of the org-wide cache
  wherever a song's arrangement matters), likely plus an arrangement
  picker on the song detail card - real schema/feature work, not a
  one-file fix.
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
  - Note from Derek, perhaps to reevaluate next time you see this,
    could we not just leverage PCO's existing archive functionality?
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
  like enough once that ships. See **Song vocal range (relative to key)**
  below - it's the structured version of this same idea, one axis wider
  (a range, not a single key) and paired with an actual per-person range
  to check against instead of a free-text note.
- **Song vocal range (relative to key)** - the natural next step past the
  per-person key fit idea above: instead of a free-text note about
  whether a key works for someone, actually know the song's sung range
  and compare it against a person's own vocal range (see People, below)
  to flag a real mismatch automatically, not just show a note someone has
  to read. Two real design pieces, not one:
  - **A key engine.** A song's range only means something relative to the
    key it was measured in, so it has to be stored as an interval
    (semitone offsets from a reference key), then transposed using the
    same pitch-class math the key-transition warning already has
    (`parseKeyPitchClass` in `sets_builder.go`) to get the effective
    range for whatever key the arrangement is actually in right now.
    That reuses existing logic rather than inventing new music-theory
    code, but it's currently inlined for one warning check - a second
    consumer is a real reason to factor it into an actual shared "key
    engine" (canonical pitch-class representation + transposition
    helper) rather than copy-pasting the parser again.
  - **Crowdsourced default.** Most directors won't know a song's range
    offhand and won't want to work it out by ear, so most songs would sit
    blank forever without some way to pre-fill a guess. If it could be
    seeded from what *other* organizations have already set for the same
    song, it could arrive pre-filled instead - not attributed ("Grace
    Church set this"), just an aggregate suggestion ("this looks like the
    range" - e.g. the most common range across every org that's set one,
    normalized through the key engine above so it transposes into
    whatever key this org's arrangement is in). This is a genuinely new
    pattern for the app: everything today is org-scoped (see CLAUDE.md's
    Organization scoping), aside from a couple of deliberately shared
    vocabulary tables. Doing this for real needs a reliable cross-org
    join key (CCLI number, not title/artist text, which collides and
    varies) and a real consent story (opt-in to contribute, never expose
    which org set what) before it's just "share the data" - the framing
    that matters here is "is this the range?", not "church X does this,"
    so the design has to actually earn that framing, not just claim it.

  Together, per-person range and per-song range are the prerequisite for
  the bigger payoff this keeps pointing at: a **preferred-key** suggestion
  per person/song pair, and eventually a **vocal range/key matrix** view
  across a whole team - "who can actually sing this song in this key" at
  a glance. Worth sequencing as: person range first (small, standalone,
  useful immediately for "can Mike hit this note" conversations even with
  zero song data - see People, below), then song range + key engine, then
  the matrix as a presentation layer over both once they both exist.
- **Occasion reservation** (Christmas Eve only, baptism/communion-specific) -
  largely already possible today: `song_roles` already has `communion`/
  `baptism` roles (migration 00020), and `song_tag_categories` already
  ships a general-purpose tagging mechanism. The actual gap is a *warning*,
  not storage - flag when a role/occasion-tagged song hasn't shown up at a
  matching service in a while, or turns up somewhere it shouldn't (e.g. a
  Christmas Eve tag in July).
- **Sermon topic/passage matching** - one of the original seed ideas
  behind song tags in the first place: given a sermon's topic, passage, or
  theme, surface which songs in the library actually speak to it.
  `song_tag_categories` is already fully dynamic (see README), so a
  `topic`/`theme` category costs nothing new to store - the real gap is
  the *picking* UX, not storage: search/filter the library by topic tag
  while building a set, not just view tags on a song row after the fact.
  **Partially shipped**: the candidate search already matched typed text
  against tag values as well as titles (`candidateMatchesSearch`,
  `sets_builder.go`), but discoverability was the missing half - nobody
  could see which tags existed without already knowing what to type. The
  set builder's "Browse by tag" chips (`set_builder.templ`) now list
  every distinct value actually in use, grouped by category behind a
  collapsible section per category, and clicking one just types that
  value into the same search box - no new filter logic. **Still open,
  deliberately not tackled by that fix**: this only surfaces tags someone
  already applied by hand; the two angles below (a structured scripture
  field, lyric-derived tags) are unaffected.
  Two angles worth chasing before committing to scope:
  - **Scripture reference as a structured field**, not a free-text tag -
    would let "what covers Romans 8" work as a real filter instead of
    hoping someone tagged it consistently. A bigger schema lift than a
    tag category, only worth it if topic tags alone feel too loose in
    practice.
  - **Lyric-derived tags** - whether Planning Center's API exposes
    song lyrics/chord chart text at all (so a topic/word could be pulled
    automatically instead of tagged by hand) needs checking against the
    live API before promising it - not yet verified.

  Worth treating as more than a brainstorm bullet if it gets built: the
  current UX (view tags on a song, filter by an existing tag) doesn't yet
  deliver the "start from a sermon, work backward to songs" workflow that
  was the actual point of building tags at all.

## People

- **Vocal range per vocalist** - lets the app know what someone can
  actually sing, not just whether PCO says they're eligible for a
  position. Simplest as a person-level field, not position-level: PCO's
  position eligibility already answers *who can serve* a position, this
  is a separate axis of *what range they're comfortable in*, and a
  vocalist's range doesn't change depending on which team/service
  they're on, so storing it once per person covers it - no need to scope
  it per-position or re-enter it per team. New local storage: a
  lowest/highest comfortable note is enough for a first pass, but it
  needs to be stored as something transposable (a pitch-class/semitone
  pair, not a display label) from day one so it can actually be compared
  against a song's range once that exists - see **Song vocal range**
  below, which this is a direct prerequisite for. Step 2, later:
  reference sound bytes per note, so a director unsure what a "G4" sounds
  like can just tap and hear it. This doesn't need to be per-org data at
  all - a single shared static audio sample set (one recorded note per
  semitone) covers every organization, so it's a one-time asset addition
  to the app, not a feature that scales with usage or needs its own
  storage/upload machinery.
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
- **History/analytics dashboard** - a step past the plain browsable
  past-plans archive (~~shipped~~ as an Upcoming/Past tab in the Build
  page's plan picker, read-only rendering of existing local sets/rosters -
  see `issue-plan-archive-and-notes`): an aggregate view over that same
  history - most-played songs, who's served how often, repetition/serving-
  frequency trends over a season rather than just per-draft warnings. The
  underlying computation already exists (the `scheduling` package's
  repetition/serving-frequency logic already computes per-song/per-person
  history internally to power the existing warnings), so this is mostly a
  presentation-layer build - a new page aggregating data that's already
  being calculated - rather than new tracking. Deliberately deferred out of
  the past-plans archive build (user chose the smaller browsable-list scope
  first); worth reconsidering once the archive's been used for a while and
  there's a concrete sense of what's actually worth aggregating.
- **Multi-week matrix view (people x dates, cross-plan)** - prompted by a
  worship pastor's own feedback: PCO's matrix (Services > a team >
  Schedule > Matrix - a grid of people down one axis, upcoming plan dates
  across the other, cells showing scheduled/declined/needs-response per
  position) is apparently where he basically lives day to day. Nothing
  here today gives that season's-eye view - the Team Builder
  (`internal/views/scheduling/roster_builder.templ`) is deliberately
  scoped to one plan at a time, so getting the multi-week picture means
  tabbing back to PCO, exactly the second-tab friction "Reducing friction
  vs. Planning Center" above is about. Two very different builds hide
  under one name, though:
  - **A bare clone** (who's scheduled where, across N weeks) would just
    re-render data PCO already shows better, natively, live-updating - it
    doesn't protect the team or the service, it's pure navigation
    convenience, and re-implementing PCO's own UI is a weaker bet than the
    deep-linking/narrow-layout ideas already above aimed at that same
    friction.
  - **An annotated matrix** - the same grid, but with this app's own
    signals layered onto each cell/row instead of PCO's plain
    scheduled/declined status - is a real, unduped win. A season laid out
    at a glance would surface a frequency-preference violation
    (`frequencyWarning`), a blockout conflict (`blockoutWarning`), a
    same-week cross-team commitment (`crossTeamWarning`), an avoid-pairing
    conflict, and (once built) a thin-bench position - all of which today
    only surface one plan at a time, inside the roster builder, for
    whichever plan happens to be open. That's both promises at once:
    burnout/friction patterns invisible one week at a time jump out across
    a grid, and staffing gaps get caught with lead time to fix them
    instead of the week of. This is the wedge PCO's own matrix
    structurally can't have - it has no idea any of this app's
    relationship/preference data exists.

  Cost is the real gate, and it's a bigger fan-out than anything shipped
  so far - `docs/pco-api-risks.md` already flags the single-plan roster builder
  as ~100-200 sequential PCO calls before mitigation; a matrix is N plans
  x M people of exactly that same shape (blockouts, frequency history,
  cross-team checks), not one plan's worth. Bounded scope needs to be
  there from the start, not retrofitted under load the way the roster
  builder was: default to a small fixed window (next 6-8 weeks, roughly a
  planning horizon) rather than a whole season, build on
  `scheduling.RunConcurrently`/`PCOFanOutLimit` from day one, and cache
  per-person blockout/history data across the whole grid render - a person
  appearing in 6 weeks' worth of cells today means 6x the calls it
  actually needs. Likely the single most expensive read-only page this app
  would build - worth prototyping against a real multi-team dev org before
  committing to the full cross-plan version, not just estimating from the
  single-plan numbers above.

  Distinct from two existing ideas worth not conflating: the **vocal
  range/key matrix** (Songs, above) is a person x song grid about vocal
  fit - unrelated axis, same word by coincidence. The **browser extension
  HUD** (Reducing friction, above) is the alternate way to deliver this
  same idea - injecting these same annotations directly onto PCO's own
  live matrix UI instead of building a native page here - worth weighing
  against a native page once this is actually prioritized: the HUD keeps
  the director inside PCO's own real-time grid (arguably a stronger
  version of "protect the team/service without leaving the workflow they
  already live in") but was already flagged as the highest-effort,
  highest-ceiling item on that list, gated on adoption data from the
  lighter integrations first.

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
- **Post-service debrief log / transition notes** - distinct from the
  ephemeral note above, which is throwaway; these are meant to be found
  again. Two related but distinct kinds of note, both keyed to a song or a
  specific song-to-song transition and both meant to resurface
  automatically the next time that song (or that pairing back-to-back)
  shows up in a draft set, so nothing has to be rediscovered by trial and
  error three months later:
  - **Debrief (retrospective)** - a 60-second post-Sunday note ("the
    bridge in Gratitude ran long," "transition out of communion felt
    abrupt") capturing what happened, for next time.
  - **Talking points (prospective)** - what to actually say bridging out
    of one song into the next (or into/out of a specific song) - carried
    into the service, not just a lesson learned after it. Worth
    prefilling/suggesting from whatever was used last time the same song
    or pairing came up, same as the debrief note.

  Needs a new table (note text plus what it's keyed to - a song id, or a
  song-pair for transition-specific notes, plus a kind so debrief and
  talking-point don't collide) and, for the pairing case, matching logic
  that recognizes "these two songs are adjacent again" while building a
  set.

## Rough leverage-to-cost read

Shipped items' cost/leverage tradeoffs are noted inline at their own
`~~struck~~` bullets above, not repeated here - this section is scoped to
what's still open. Cheapest remaining: **per-person key fit** (a new
`song_person_preferences` code - the free-text `notes` column already
exists) and **occasion reservation** (`song_roles`/`song_tag_categories`
already model the storage; the gap is the warning, not the data).
**First-time-position pairing** is probably the most valuable of the rest,
but unlike those two, it costs a real PCO call per assignment (same tier
as today's serving-frequency check).

**Vocal range** splits by half: per-person range is cheap and standalone
(one small new table, useful even with zero song data). Per-song range
costs more - needs a real key engine factored out of the key-transition
warning's one-off pitch-class parser - and its crowdsourced-default piece
is the priciest single item in this file: the first genuinely cross-org
data pattern this app would have, needing a reliable join key and a real
consent story before it's shippable. The eventual key matrix is
presentation-only once both ranges exist.

Priciest of the rest: **stage density/band-balance checking** (a new
capability/profile axis on both people and services, nothing existing to
extend) and the **post-service debrief log** (new storage plus
adjacency-matching logic to resurface a note at the right moment).
Everything on the People side not covered above (pastoral flag,
call-first list, flexibility inventory, growth notes) is genuinely new
local storage with no existing table to lean on - the pastoral flag
specifically also needs a visibility decision before it needs any code.
