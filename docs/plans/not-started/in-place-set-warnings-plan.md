# Plan: in-place warnings instead of a top-of-page alert list

Written 2026-09-05, expanding item 6 of `docs/ideas.md`'s "Reducing
friction vs. Planning Center" section into a real plan. Not built yet.
Item 5 right above it in that list (bringing the Build page's layout
closer to PCO's own) already shipped - see
`docs/plans/shipped/build-page-redesign-plan.md` - but that redesign
touched Team-tab person rows and side-by-side team cards, not the Songs
tab's row layout or its warning banner, so there's nothing from it to
build on here; this is a genuinely separate piece of the page.

## Current state

Three warning-producing functions in
`internal/server/controllers/sets_builder.go`, plus one more site (avoid-
pairing) computed inline in `buildSetBuilderData` itself, all feeding
three `[]string` fields on `views.SetBuilderData`
(`internal/views/scheduling/set_types.go`):

- **`ExistingConflicts`** (`sets_builder.go` ~L337-346): from
  `sc.store.AvoidPairings`, one string per pair of songs *already both in
  the set* that have an `avoid_pairing` relationship - `"%s conflicts
  with %s - both are already in this set."` The two songs are not
  necessarily adjacent in the order of service; this is a same-set
  pairing check, not a positional one.
- **`RepeatWarnings`** (~L354-362): one string per set song that was sung
  in each of the last two past plans for this service type (from
  `fetchRecentPlanHistory`'s `RepeatedSongs`) - `"%s was sung in each of
  the last two plans..."`. Purely about **one song**, no second song
  involved at all.
- **`PlanWarnings`** (~L266-269): two unrelated checks bundled into one
  field, listed as one item in `docs/ideas.md` but structurally two
  different shapes:
  - `serviceLengthWarning` (L1111): whole-plan total length vs. this
    service type's recent average. Not attributable to any one row or
    pair of rows at all - it's a property of the *set as a whole*.
  - `keyTransitionWarnings` (L1209): flags two songs that are **truly
    back-to-back** in the merged order (`rows[i-1]`/`rows[i]`, nothing
    else between them - see `mergeServiceOrder`) whose keys are a
    half-step or tritone apart. Unlike `ExistingConflicts` above, this
    check is inherently positional - it only fires on adjacent rows, not
    any two songs in the set.

So there are really **four** warning shapes, not two:

| Warning | Subject | Positional? |
|---|---|---|
| `RepeatWarnings` | one song | single row |
| `keyTransitionWarnings` | two songs | strictly adjacent rows only |
| `ExistingConflicts` (avoid-pairing) | two songs | anywhere in the set, not necessarily adjacent |
| `serviceLengthWarning` | the whole set | no row at all |

All four functions return pre-formatted, already-`fmt.Sprintf`'d message
strings with titles baked in - `keyTransitionWarnings` takes a
`titles map[string]string` lookup and interpolates title+key text
directly into the string it returns. None of them return anything that
identifies *which row* (song ID, or set-song row ID) the message is
about - the string *contains* a title as English text, but nothing
structured points back at a `SetSongView`/`ServiceOrderRow`. That's the
single biggest fact this plan has to reckon with: moving these inline is
not purely a template change, because the row-attribution information
doesn't exist as data today, only as prose.

**Rendering today**
(`internal/views/scheduling/set_builder.templ` `songWarnings`/
`songWarningsOOB`/`songWarningSpans`, L100-133): all three fields are
concatenated into one `<div id="song-warnings">` rendered once, right
above `<h2>Order of service</h2>` and its table
(`SetBuilderBodyContent`, L312-313), each message a flat
`<div class="warning">{ msg }</div>` (styling: `.warning` in
`web/static/css/components/alert.css`). There is no per-row hook today -
a set song's own `<tr class="song-row">` renders title +
`song-meta` (arrangement info line) in one `<td>`, and `songBadges`
(role/published/moved/info-pending badges) in the next - warnings never
touch that row at all.

The count also surfaces a second place: `SongStatusSummary`
(`status_summary.templ` L78-94) shows a `"Warnings: N"` status chip
(`len(ExistingConflicts) + len(RepeatWarnings) + len(PlanWarnings)`),
which is also mirrored into the "Songs, at a glance" sidebar card via
`songGlanceContent`. This chip is a *count only* today - clicking it does
nothing (no deep link/scroll).

`songWarnings`'s own OOB variant exists specifically because
`KeyFormFragment` (song_detail_card.templ) - editing a song's key
override - can flip `keyTransitionWarnings`' verdict on the *songs next
to it* without a full-page `SetBuilderBody` re-render; the doc comment on
`songWarnings` calls this out explicitly as the reason a narrow-fragment
response still needs to push a fresh warnings list via OOB swap. Any
inline redesign has to preserve this: an edit on one row's detail card
can change what's shown on an *adjacent* row, so the OOB-refresh
discipline doesn't go away, it just needs to target more than one node
(the warnings container plus however many rows are now affected)
instead of one shared container.

## Existing pattern that's most of the answer for two of the four

`components.ConflictBadge` (`internal/views/components/badge.templ`
L21-25) already exists and already renders exactly this shape - `<span
class="conflict-badge">⚠ { message }</span>` - and is already used
**inline, per-row**, just not on set songs yet. `songCandidateMeta`
(`set_builder.templ` L201-227), the badge cluster for the "Browse all
songs"/"Recommended next" candidate lists, already calls it twice:

```go
if len(cand.ConflictTitles) > 0 {
    @components.ConflictBadge(fmt.Sprintf("avoid pairing with %s", strings.Join(cand.ConflictTitles, ", ")))
}
if cand.RecentlyRepeated {
    @components.ConflictBadge("sung in each of the last two plans")
}
```

This is the same avoid-pairing and repeat-song logic as
`ExistingConflicts`/`RepeatWarnings` above, just computed and rendered
for *not-yet-added* candidates instead of *already-in-the-set* songs
(`CandidateSongView.ConflictTitles`/`RecentlyRepeated` vs. the set-level
string lists). In other words: this app already ships one half of "move
the warning onto the row it's about" for the pre-add candidate list - the
open half is applying the identical badge to a song *already in the set*
(`SetSongView`), which today gets no such treatment; its warnings only
ever show up in the shared top-of-page list. `ConflictBadge` needs no
changes to be a fit for `RepeatWarnings` (single-row) and
`ExistingConflicts` (pairing, but rendered on either side individually -
`ConflictTitles` is already a list per song, exactly what a per-row badge
needs); it says nothing about *where* on a row it renders, so it's a
genuine reusable primitive here, not something to duplicate.

No other candidate component looked at came closer: `song_detail_card.templ`
has no existing warning/badge surface of its own beyond what
`songBadges` already provides (role/published/moved/info-pending), and
`status_summary.templ`'s chips are page-level counts, not per-item.

## Proposed design

This only needs to be a **view-layer + light data-shape change**, not new
backend logic - all four checks already run every render and already have
the underlying identifiers (`SetSong.SongID`, row index) available at the
point they're computed; what's missing is threading an identifier through
to the render instead of collapsing straight to a formatted string. No
new PCO calls, no new stored/queried data, no migration.

### 1. Single-row warnings (`RepeatWarnings`) - straightforward

Add a `RecentlyRepeated bool` (mirroring `CandidateSongView`'s existing
field of the same name) to `SetSongView`, set it from the same
`repeated[ss.SongID]` map `RepeatWarnings` is built from today. Render it
with `components.ConflictBadge` right in `songBadgeSpans` (or its own
sibling span next to it), the same call site `songCandidateMeta` already
uses for the identical case. This is the easy, no-open-questions half of
the change.

### 2. Adjacent-pair warnings (`keyTransitionWarnings`) - needs a "between two rows" treatment, not a single-row badge

A half-step/tritone key change is about the *transition*, not either
song alone - putting the same badge on both `rows[i-1]` and `rows[i]`
would read as "this song has a key problem" on two rows that are each
individually fine. `keyTransitionWarnings` should stop returning
`[]string` and instead return a structured
`[]KeyTransitionWarning{AfterRowID int64; Message string}` (or similar) -
keyed by *which row the transition happens after* - and the table render
loop should check "is there a transition warning after this row" and
render a distinct inline marker between that row and the next (a thin
banner row between `<tr>`s, or a badge on the *second* song captioned
"vs. previous song" rather than a bare warning) - not simply reuse
`ConflictBadge` unmodified on one of the two songs. This is a real UI
piece to design, not just a data change - flagged as an open question
below.

### 3. Non-adjacent pairing warnings (`ExistingConflicts`) - already half-solved, needs the "which titles" data threaded through

Same shape `CandidateSongView.ConflictTitles` already solves for
candidates: add a `ConflictTitles []string` to `SetSongView`, populate it
from the same `AvoidPairings` result `ExistingConflicts` is built from
today (that call already returns both songs of each conflicting pair;
today it's flattened into one string per pair rather than attributed back
to each song). Render via `ConflictBadge` in `songBadgeSpans`, identical
to how the candidate list already does it. Since the two conflicting
songs aren't necessarily adjacent, each gets its own badge naming the
other by title (`"avoid pairing with %s"`) - no "between two rows" UI
needed here, unlike the key-transition case.

### 4. Whole-plan warnings (`serviceLengthWarning`) - has no row to attach to

This one can't move inline at all - there is no row it's "about." It
should stay exactly where it is today (top-of-page banner, or folded into
the status-summary line), possibly the *only* thing left in the
top-of-page list once the other three move inline. Worth calling this out
explicitly in the UI copy/heading once the list is mostly empty, so a
single remaining message doesn't look like a leftover bug.

### Open questions - real decisions needed, not resolved here

- **Does the top-of-page list disappear entirely, or stay as a secondary
  summary?** Moving warnings onto rows means losing "see every warning on
  this plan at a glance without scrolling the table" - for a long order of
  service that's a real regression, not a cosmetic one. Two candidate
  resolutions, neither chosen here: (a) keep the existing
  `"Warnings: N"` status chip (`SongStatusSummary`) as the single
  standing summary, and make it a real deep link/scroll-to-first-warning
  control instead of inert text as it is today, or (b) keep the full list
  rendered but demote it to a collapsed `<details>`/`Disclosure`-style
  summary that's closed by default, expandable on demand. Either way,
  `serviceLengthWarning` (point 4 above) needs a home regardless of what
  else survives, since it has no row.
- **Interaction model for the inline badge itself.** `ConflictBadge` as it
  exists today renders a plain `<span title="...">`-free pill with the
  message as its own visible text (see `songCandidateMeta`'s usage) - it's
  always-visible short text, not a hover tooltip, which sidesteps the
  keyboard/screen-reader gap a hover-only tooltip would introduce. That's
  probably right to keep as-is for `RepeatWarnings`/`ExistingConflicts`
  (their messages are already short), but `keyTransitionWarnings`'
  message text is a full sentence
  (`"Amazing Grace (G) into How Great Thou Art (Ab) is a half-step key
  change with no transition - consider a pad/bridge, or adjusting one
  song's key."`) - too long for an always-visible inline pill without
  either truncating it or wrapping awkwardly in the row. Whether that one
  needs a `title=` attribute (hover, mouse-only) vs. a
  `components.BadgeTitled`-style short label plus a click-to-expand
  `Disclosure` for the full sentence (keyboard/screen-reader accessible,
  this app's existing pattern for exactly this tradeoff - see
  `internal/views/components/disclosure.templ`) is a real design decision,
  not answered here. Lean toward `Disclosure` for the long message given
  CLAUDE.md's own note that hover-only access is a real accessibility gap
  this app already has the componentry to avoid.
- **Where exactly does the key-transition marker render structurally?** A
  `<tr>` between two other `<tr>`s (its own thin warning row, spanning all
  columns) vs. a badge on the second song's row captioned to reference the
  first (e.g. "half-step from previous song") are both plausible; the
  former reads more clearly as "this is about the gap between these two"
  but is more template work (`TableWrap`'s current markup has no notion
  of an inter-row spacer); the latter reuses the existing per-row badge
  slot but needs careful wording so it doesn't read as a warning about the
  song itself. Not resolved here - needs a mockup before building, same
  as the song-tags redesign plan's own "needs an actual mockup" call on
  its analogous layout question.
- **OOB refresh scope once warnings live on rows.** Today `songWarnings`/
  `songWarningsOOB` push one shared container; once warnings live on
  individual rows, an edit that changes one row's warning state (the
  `KeyFormFragment` case the current doc comment calls out by name) needs
  to OOB-refresh that row's own badge markup (probably folding into the
  existing `songBadgesOOB` per-row fragment, which already exists for
  exactly this kind of narrow update) *and*, for the key-transition case,
  its *neighbor's* row too, since a key edit on song N can flip the
  verdict on the N-1/N and N/N+1 transitions simultaneously. Whatever
  survives of the top-of-page summary (see the first open question) may
  also need refreshing in the same response. This needs an actual list of
  "which fragments does each mutation handler now need to touch," not an
  assumption that `songBadgesOOB` alone covers it.

### Mobile / narrow layout

CLAUDE.md's friction-reduction roadmap separately calls out a ~350px
floating-sidebar target width, not yet deliberately tuned. An inline
badge scheme adds visual width to what's already a tightly-packed row
(title + arrangement-info line + role/status badges in one `<td>` per
`SetBuilderBodyContent`'s markup) - at 350px, cramming a same-set conflict
badge plus a repeat badge plus role badges onto one row risks either
wrapping badly or becoming illegible. Out of scope to actually tune here
(no work has gone into the 350px target generally yet, per that roadmap
item's own wording), but the row markup this plan proposes should degrade
gracefully at that width the way `songBadgeSpans`' existing badges already
have to - flagged so it isn't silently forgotten, not solved.

## Suggested build order

1. `RepeatWarnings` -> `SetSongView.RecentlyRepeated` +
   `ConflictBadge` (point 1 above) - lowest-risk, reuses an existing
   component completely unchanged, no open questions blocking it.
2. `ExistingConflicts` -> `SetSongView.ConflictTitles` +
   `ConflictBadge` (point 3) - same shape as step 1, slightly more data
   plumbing (attributing a pair back to each side) but still no open
   design questions.
3. Decide the two open questions that gate everything else: whether the
   top-of-page list survives in some form, and the interaction model
   (plain badge vs. `Disclosure`) for a long message.
4. `keyTransitionWarnings`'s structural change (point 2) - the real
   design work, once step 3's decisions are made; likely the biggest
   single piece, since it needs a genuinely new "between two rows"
   rendering, not just an existing component reused.
5. Wire up whatever the top-of-page summary becomes (deep link/scroll, or
   collapsed disclosure) and confirm `serviceLengthWarning` (point 4)
   still has a clear, non-orphaned home once the rest has moved inline.
6. Audit every OOB-refresh call site currently touching
   `songWarningsOOB`/`songBadgesOOB` (`KeyFormFragment` and any other
   narrow-fragment handler) against the new scope from the last open
   question above.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test` - `sets_builder_warnings_test.go`'s existing tests
  (`TestServiceLengthWarning*`, `TestParseKeyPitchClass*`,
  `TestSemitoneDistanceWrapsAround`, `TestKeyTransitionWarnings*`) exercise
  the pure calculation functions directly (pitch-class math, threshold
  logic) and should keep passing unchanged regardless of how the result is
  rendered - only `keyTransitionWarnings`' *return type* changes (point 2
  above), so those specific assertions (`got[0]` containing both song
  titles, etc.) will need updating to match the new structured return
  value, not the underlying behavior they're checking.
- New tests needed: `RecentlyRepeated`/`ConflictTitles` correctly populate
  on `SetSongView` for the same fixtures `sets_builder_test.go` already
  uses for `TestServiceOrderLengthSecondsMixesForeignItemsAndSongs`-style
  scenarios; a rendering-level check (existing templ tests, if any, or a
  manual page check) that a key-transition warning attaches to the right
  pair of rows and not their neighbors.
- Live check: open a plan with a real avoid-pairing conflict, a real
  repeated song, and a real half-step key transition all at once, confirm
  each renders on the correct row(s) and that editing a key override on
  the detail card correctly updates the adjacent row's badge without a
  full page reload (the exact scenario `songWarningsOOB`'s doc comment
  says motivated the OOB fragment in the first place).
- Confirm no PCO call was added - this is local-computation-plus-rendering
  only, so `docs/pco-api-risks.md` needs no update.
