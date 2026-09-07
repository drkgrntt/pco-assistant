# Plan: archive songs via PCO's own `hidden` flag, plus a "not sung in a while" nudge

**Status: shipped (2026-09-07).** All of this doc's "Suggested build
order" steps except the optional step 6 are done. Steps 1 and 2
(live-verifying and building `go-pco`'s `UpdateSong`) shipped first, per
commit `8bee9f7`. Step 3 (the archive action itself) followed: an Archive
icon-button on each rich row of the Build page's "Browse all songs" search
results (`CandidateSearchResults`, `internal/views/scheduling/
set_builder.templ`), confirm-guarded via `hx-confirm`, posting to
`sc.ArchiveSong` (`internal/server/controllers/sets_builder.go`) which
calls `pco.UpdateSong` to flip `hidden` to `true` and invalidates the
active-songs cache. Steps 4 and 5 (the "not sung in a while" nudge and its
seasonal exemption) shipped together: `notSungInLabel`
(`internal/server/controllers/sets_builder.go`) computes
`CandidateSongView.NotSungInLabel` from `Song.LastScheduledAt` at a fixed
180-day threshold, skipped entirely for a song carrying the new
`seasonal` song-tag category (migration `00039`,
`scheduling.TagCategorySeasonal`) - rendered via `songCandidateMeta`
(`set_builder.templ`) next to the reinforcement badge. Step 6 (an
optional archived-songs browse view) remains genuinely optional and is
not built - see that step's own "not yet decided" framing below, which
still holds.

Written 2026-09-05, replacing the "Retired / don't resurrect flag" brainstorm
bullet in `docs/ideas.md`'s Songs section. That bullet originally proposed a
new local concept - a `song_tag_categories` row (`status`/`retired`) plus a
notes table for *why* a song was retired - duplicating a distinction
Planning Center already models. Derek's own call on it, after actually
weighing the idea:

> I still don't see what retired/don't resurrect gets us that simply
> archiving doesn't. And I'm happy to roll out `UpdateSong` to flip the
> hidden flag rather than circumventing it in the name of not touching the
> PCO lib. I'd rather do it right.

So this plan is a real, settled direction, not a brainstorm to weigh: **no
new local schema for "retired" - add an `UpdateSong` wrapper to `go-pco`,
and let this app flip PCO's real `hidden` attribute.** It also folds in the
adjacent "Not sung in a long time" nudge bullet from the same section, which
is the natural *prompt* leading a director to actually archive something.
Not built yet; written up for later, same spirit as
`docs/plans/not-started/song-tags-setup-redesign-plan.md`.

## Current state

**PCO already has the flag, and this app already reads it.**
`internal/scheduling/pco_songs.go`'s `FetchActiveSongs` is the org-scoped,
60s-cached entry point almost every caller uses. It fetches with
`SongsParams.Hidden = false` as a server-side optimization
(`fetchSongsFromPCO`), then `filterActiveSongs` drops any `Attributes.Hidden
== true` song again client-side - this second pass is documented as the
*real* correctness guarantee, not just an optimization on top of the
server-side filter. That means: the moment a song's `hidden` attribute is
true in PCO, it already stops appearing in every list this app builds from
`FetchActiveSongs` - no new filtering logic needed anywhere downstream once
this app can set that flag itself.

**`LastScheduledAt`/`LastScheduledShortDates` are already fetched, and
already used.** `go doc github.com/drkgrntt/go-pco SongAttributes` (v0.21.0,
this repo's pinned version per `go.mod`) confirms both fields exist on
`SongAttributes`:

```go
LastScheduledAt         *time.Time      `json:"last_scheduled_at"`
LastScheduledShortDates StringOrStrings `json:"last_scheduled_short_dates"`
```

`SongsParams` (`~/go/pkg/mod/github.com/drkgrntt/go-pco@v0.21.0/songs.go`)
has no sparse-fieldset/`Fields` option at all - `GetSongs` always returns
PCO's full default attribute set, so there's no risk of these coming back
empty because of a restrictive query. This isn't a leap of faith: the
reinforcement feature already reads this exact field off this exact call,
today, in production code -
`internal/server/controllers/sets_builder.go` lines 121-124:

```go
for _, song := range songs {
    if song.Attributes.LastScheduledAt == nil {
        _ = sc.store.TrackNewSong(ctx, organizationID, song.ID)
    }
}
```

(`songs` there is `fetchActiveSongs`'s result, the controller-package
wrapper around `scheduling.FetchActiveSongs`.) So the nudge this plan adds
costs zero new PCO traffic - it's a new computation over data already in
hand on every existing render, exactly per `docs/ideas.md`'s framing.

**`go-pco` today wraps `CreateSong`/`GetSong`/`GetSongs`/`DeleteSong` -
no `UpdateSong`.** Confirmed via `go doc github.com/drkgrntt/go-pco` and
reading `~/go/pkg/mod/github.com/drkgrntt/go-pco@v0.21.0/songs.go` directly:
those four are the whole song-related surface. `go-pco` is a separate
module/repo (`github.com/drkgrntt/go-pco`), pulled into this app as a
normal tagged dependency (`go.mod` currently pins `v0.21.0`) - see
"What this is" in this repo's own `CLAUDE.md`. Adding `UpdateSong` is a
change to *that* repo, tagged and released independently, then bumped here
- not something this repo's own code changes can accomplish alone.

**No local table has a real foreign key to a song id**, so there is no
orphaning risk from archiving. `song_tags`, `song_person_preferences`,
`song_relationships` (`song_id_a`/`song_id_b`), and `song_roles` all store
`song_id` as a plain `TEXT NOT NULL` column with no `REFERENCES` constraint
to any local "songs" table - because there isn't one; songs live only in
PCO, per this app's whole model (see `CLAUDE.md`'s "What this is"). Setting
`hidden: true` on a PCO song doesn't delete or renumber that song's id, it
only changes whether it appears in `where[hidden]=false` queries - so every
existing local row referencing that song's id stays exactly as valid as it
was before, harmlessly inert until (if ever) the song is un-hidden and
becomes visible again. **Archiving needs zero local-table cleanup.**

## Existing reusable patterns

**The exact template for `UpdateSong`'s shape**: `UpdateArrangement`
(`arrangements.go`) and `UpdateItem` (`items.go`) are `go-pco`'s two
existing `Update*` wrappers, and they converge on the same idiom -
`UpdateItemParams` is the cleaner one to copy since it's a pure partial
update with no non-pointer fields at all:

```go
// UpdateItemParams is a partial update - only set (non-nil) fields are
// sent... Every field is a pointer, not a plain string/int, for the same
// reason Length always was: a zero value (0, "") is legitimate data...
type UpdateItemParams struct {
    Title           *string
    Description     *string
    ServicePosition *string
    Length          *int
    ArrangementID   *string
    KeyID           *string
}

func UpdateItem(ctx context.Context, serviceTypeID, planID, itemID string, params *UpdateItemParams) (response ItemResponse, err error) {
    ...
    attributes := map[string]any{}
    if params.Title != nil { attributes["title"] = *params.Title }
    ...
    response, err = NewRequest[ItemResponse](ctx, "PATCH", url, body)
    ...
}
```

This pointer-per-field shape is exactly what `UpdateSong` needs: `Hidden`
is a `bool`, and a plain `bool` field can't distinguish "leave hidden
alone" from "explicitly set hidden=false" (both serialize as the zero
value) - the same reasoning `UpdateArrangementParams.Notes *string` and
`UpdateItemParams`'s whole shape already codify in this library.

**The existing immediate-PCO-write controller pattern**:
`internal/server/controllers/people.go`'s `SetPositionPreference`/
`setPersonTeamPositionPreference` (backing
`PersonTeamPositionAssignment.SchedulePreference`, which per `CLAUDE.md`'s
sync-model section already writes straight to PCO with no local draft
concept) is the reference shape for "one form submit, one PCO write, no
draft/publish cycle":

```go
func (sc *SchedulingController) SetPositionPreference(c *fiber.Ctx) error {
    var body struct{ ... }
    errMsg := ""
    if err := c.BodyParser(&body); err != nil {
        errMsg = err.Error()
    } else if /* validation */ {
        errMsg = "..."
    } else if err := setPersonTeamPositionPreference(middleware.RequestContext(c), ...); err != nil {
        errMsg = err.Error()
    }
    return sc.renderPositionAssignments(c, ..., errMsg)
}
```

Parse -> call PCO via `middleware.RequestContext(c)` -> collect `errMsg` on
failure -> re-render the same fragment either way. `ArchiveSong` (below)
should match this shape exactly, not invent a new one.

**The existing "signal computed from already-fetched data, surfaced as a
badge" pattern**: `CandidateSongView.NewSongLabel`
(`internal/views/scheduling/set_types.go`) is the reinforcement feature's
badge - "Sung once"/"Sung twice" - computed in
`candidateSongView`/`reinforcementLabel` (`sets_builder.go`) from data
already in hand (`trackedNew`, `appearanceCounts`), rendered via
`songCandidateMeta` next to each row in the "Browse all songs" list
(`CandidateSearchResults`, `set_builder.templ`). The "not sung in N days"
nudge is this same idiom, mirrored: a short badge computed from
`Attributes.LastScheduledAt` (already in hand, see Current state above),
no new fetch.

**The existing hard-to-reverse-action confirm pattern**: `songDetailCard`
already uses `hx-confirm` for "Remove this song from the set?" and
"Replace %q with the selected song?" - real, user-facing confirms on
actions that touch published state. Archiving is a step further (a live
write to the org's actual PCO data, not a local draft, reversible only by
someone flipping `hidden` back - in this app or in PCO directly) and should
use the same `hx-confirm` mechanism, not a bespoke modal.

## Proposed design

### 1. `go-pco`: add `UpdateSong`

A new file addition to `go-pco`'s `songs.go` (that repo, not this one),
modeled directly on `UpdateItemParams`/`UpdateItem` above:

```go
// UpdateSongParams is a partial update - only set (non-nil) fields are
// sent, so archiving a song (Hidden) never clobbers Title/Author/etc back
// to empty. Every field is a pointer for the same reason UpdateItemParams'
// are: a zero value (false, "", 0) is legitimate data, so it can't also
// mean "leave this alone."
type UpdateSongParams struct {
    Title      *string
    Author     *string
    Admin      *string
    Copyright  *string
    CCLINumber *int
    Themes     *string
    Hidden     *bool
}

func UpdateSong(ctx context.Context, id string, params *UpdateSongParams) (response SongResponse, err error) {
    if params == nil {
        return response, fmt.Errorf("params cannot be nil")
    }

    url := fmt.Sprintf("%s/%s/%s", baseURL, songsPath, id)

    attributes := map[string]any{}
    if params.Title != nil { attributes["title"] = *params.Title }
    if params.Author != nil { attributes["author"] = *params.Author }
    if params.Admin != nil { attributes["admin"] = *params.Admin }
    if params.Copyright != nil { attributes["copyright"] = *params.Copyright }
    if params.CCLINumber != nil { attributes["ccli_number"] = *params.CCLINumber }
    if params.Themes != nil { attributes["themes"] = *params.Themes }
    if params.Hidden != nil { attributes["hidden"] = *params.Hidden }

    response, err = NewRequest[SongResponse](ctx, "PATCH", url, NewRequestBody(attributes))
    return
}
```

Every `CreateSongParams` field gets a pointer counterpart here (matching
how `UpdateArrangementParams` mirrors `CreateArrangementParams`) even
though this plan's only immediate consumer is `Hidden` - a `Hidden`-only
params struct would work for archiving alone, but a lopsided "update
struct with one field" is inconsistent with every other `Update*` in the
library and would need revisiting the moment anything else on a song
becomes editable from this app. `Notes` is deliberately left out, same
reasoning `CreateSongParams`'s own doc comment already gives ("notes
cannot be assigned" - PCO manages a song's notes as its own sub-resource).

**Not yet confirmed, flagged rather than guessed at**: the PATCH URL/verb
above (`PATCH services/v2/songs/{id}`) is inferred by strong analogy - it's
exactly `GetSong`'s own URL (`songsPath/{id}`) and exactly the same
"PATCH the resource's own show path" shape both `UpdateArrangement` and
`UpdateItem` already use - but it has **not** been verified live against
a real Planning Center org the way `SongsParams.Hidden`'s
`where[hidden]=true/false` filtering was (that one has an explicit
"confirmed live" comment in `songs.go`; `UpdateSong` doesn't exist yet, so
there's nothing to have confirmed). Per this repo's own `CLAUDE.md`
("verify live against the signed-in dev PCO account instead of guessing"),
step one of actually building this should be a one-off live PATCH against
a real dev-org song (flip `hidden` true, confirm the response and that the
song vanishes from a `where[hidden]=false` list, flip it back) before
writing `UpdateSong`'s real implementation - cheap to check, and this plan
shouldn't be read as having already confirmed PCO's write-side behavior
just because its read-side behavior is well-established.

**Cross-repo build-order step, distinct from anything in this repo**: this
requires (1) implementing `UpdateSong` in the `go-pco` repo, (2) tagging a
new release (e.g. `v0.22.0`), (3) bumping this repo's `go.mod`/`go.sum` to
that tag, *before* any controller code here can call it. That's real,
sequenced work outside this checkout, not a detail to skip past.

### 2. What "Archive" actually does, end to end

1. A director clicks an "Archive" action on a song somewhere in this app
   (see "Where it surfaces" below).
2. A confirm step fires (`hx-confirm`, matching the existing pattern - see
   "Confirmation/safety" below) since this is a real, immediate write to
   the org's live PCO data, not a local draft.
3. The controller calls `pco.UpdateSong(middleware.RequestContext(c), songID,
   &pco.UpdateSongParams{Hidden: pco.BoolPtr(true)})` (or however this
   app's own convention makes a `*bool` literal - check for an existing
   helper before adding a new one).
4. On success, call `scheduling.InvalidateActiveSongsCache(organizationID)`
   (already exported, already exists exactly for this: "any action that
   explicitly expects a guaranteed-fresh pull from PCO, not a possibly-60s-
   stale cached one") so the song disappears from every list on this app's
   very next render, not up to 60 seconds later.
5. Re-render whatever fragment the action lives in (matching the
   `SetPositionPreference` pattern above) with an error message on
   failure, nothing further needed on success - the song is simply gone
   from the list it was just archived from.
6. No local bookkeeping - per Current state above, no local table has a
   real FK to a song id, so there is nothing to clean up, and nothing that
   would go stale if the song is later un-hidden.

This is genuinely enough - there's no local "retired" state left to
reconcile, which is exactly the point of not building a second local
concept.

### 3. "Not sung in a long time" nudge

A short badge - mirroring `CandidateSongView.NewSongLabel`'s existing
idiom exactly - computed from `Attributes.LastScheduledAt`:
`time.Since(*song.Attributes.LastScheduledAt) > threshold` (never sung at
all, i.e. `LastScheduledAt == nil`, is a different, already-handled case -
`TrackNewSong`'s reinforcement path - and shouldn't also trigger this
nudge, which is about a song that *used to* get sung and has gone cold).

**Open, not decided by this plan**:

- **The threshold itself.** "90 days" was floated in discussion but never
  committed to - this needs a real number (or a config knob) chosen before
  building, not silently hardcoded here. A church's rhythm (weekly vs.
  biweekly services, a smaller vs. larger rotating library) plausibly
  wants this tunable rather than a single hardcoded constant for every
  org - worth deciding alongside the number itself, not assumed.
- **Where it surfaces.** The natural, lowest-cost fit is the Build page's
  existing "Browse all songs" panel (`CandidateSearchResults`,
  `set_builder.templ`), for two reasons: it already renders one row per
  song in the full active library, and `songCandidateMeta`'s badge slot
  already exists as the reinforcement label's home - a "hasn't been sung
  in N days" badge is the same idiom on the same row. One real wrinkle:
  the *default* (no query typed yet) view uses `SongOptionView`
  (`buildBrowsableSongs`), a deliberately lightweight, title-only struct -
  the doc comment on `CandidateSearchResults` explains this is so nothing
  *PCO-expensive* gets computed until someone actually searches. Adding a
  "not sung in N days" label to the default browsable list isn't
  PCO-expensive (the data's already in the in-memory `[]pco.SongData`
  slice - it's a per-song `time.Since` check, not a fetch), so it doesn't
  conflict with that principle, but it does mean `SongOptionView` would
  need a new field, and `buildBrowsableSongs` would need to populate it -
  a small, real addition to confirm before assuming it's free. A separate,
  not-yet-built plan
  (`docs/plans/not-started/song-tags-setup-redesign-plan.md`) proposes a
  song-picker/grouped-by-song browse view for the Song Tags setup page -
  if that ships first, its per-song groups would be an equally natural (or
  better - genuinely library-management-scoped rather than tucked inside
  one plan's build page) home for this same badge and the Archive action
  below. This plan doesn't depend on that one landing first, and shouldn't
  be blocked waiting for it, but whoever builds this should check whether
  it has landed by then rather than duplicating the browse UI twice.
- **Archive's own UI entry point** (see above) is the same open question -
  most likely the same "Browse all songs" row, an icon-button action next
  to the reinforcement/nudge badges, following `songDetailCard`'s existing
  Duplicate/Remove/Replace icon-button row precedent.

**Seasonal exemption**: `song_tag_categories` (migration `00016`) is
exactly the dynamic lookup table this needs, but there is **no existing
"seasonal"/"occasion" category** today - the three seeded categories are
`theme`, `scripture`, `idea`. This plan proposes adding a new category
(e.g. `occasion`, values like "Christmas"/"Easter" - open to whatever
label reads best, this is a naming detail not a design one) via a plain
migration inserting one row, the same "just a row here, no code change"
mechanism the table's own doc comment describes. The nudge then checks:
does this song have any tag in that category? If so, suppress the "not
sung in a while" badge regardless of `LastScheduledAt` - a long gap on a
tagged-seasonal song is expected, not a signal. This is a fresh addition
to build, not something already in place - `docs/ideas.md`'s "Occasion
reservation" bullet (a different, unbuilt idea about reserving a tag like
"Christmas Eve" to a date window) is adjacent but distinct; don't conflate
the two while building this.

The nudge is explicitly framed the same way `docs/ideas.md` already frames
it: a *prompt* to consider archiving, never an automatic suggestion or
auto-archive - a legitimately seasonal song's long gap is by design, and
nothing here should ever hide or flag it as a problem once the seasonal
exemption is in place (and should stay conservative - showing the badge
when in doubt - if the exemption isn't in place yet for a given org).

### 4. Un-archiving

**Does this app need its own "un-archive" action?** Reasoned answer: not
for v1, and possibly not ever, for a concrete reason - `hidden` is a real
PCO-side attribute, and PCO's own UI already lets anyone with access
un-hide a song directly there; nothing about this app owning the archive
*write* implies it must also own the un-archive path. The actual gap is
narrower than "no un-archive button": once a song is archived, this app's
only existing way to see archived songs at all is
`FetchAllSongsIncludingArchived` (`pco_songs.go`), which is explicitly
**uncached and unscoped by organization** - "only for `cmd/pcoreset`,"
per its own doc comment, not something any page or controller uses today.
So there is currently no "browse archived songs" view in this app for a
director to even find the song they'd want to un-archive, other than going
into PCO directly.

Given that, the honest open question isn't "should Archive have a
symmetric Un-archive button" (skip that - PCO's own UI covers the
mechanism fine) but **"is an 'Archived songs' browse view worth building
here too, for the case where someone archives a song by mistake and wants
to fix it without leaving this app?"** That's a real product-scope
question this plan flags rather than answers - it's a materially bigger
piece (a new, scoped `FetchArchivedSongs`-equivalent, org-scoped and
cached like `FetchActiveSongs`, plus a new view) than the archive action
itself, and shouldn't block shipping the archive/nudge pair above. If the
answer later turns out to be "yes," it composes cleanly with everything
here: same `UpdateSong` call, `Hidden: false` instead of `true`.

### 5. Confirmation/safety

Archiving is a real, immediate, live write to the org's actual PCO data -
unlike almost everything else on the Build page's Songs tab, it has no
local-draft/Publish step to catch a mistake before it reaches PCO (see
`CLAUDE.md`'s sync-model section: this is squarely in the "writes straight
to PCO immediately" bucket, alongside `schedule_preference`, not the
"stays local until Publish" bucket). It needs an explicit `hx-confirm`
step before the request fires, matching the existing precedent
(`songDetailCard`'s Remove/Replace buttons) rather than firing on a bare
click - something like `hx-confirm="Archive {title} in Planning Center?
It won't be deleted, just hidden - you (or anyone with PCO access) can
unhide it there anytime."` - worth being explicit in the confirm text that
this isn't a delete, given "archive" and "delete" read as interchangeable
to someone unfamiliar with PCO's own terminology.

## Suggested build order

1. **Live-verify `UpdateSong`'s real PCO shape** (PATCH verb/path,
   response on success/failure) against the signed-in dev PCO account -
   cheap, and removes the one genuinely unconfirmed assumption in this
   plan before writing library code against it.
2. **`go-pco`: add `UpdateSongParams`/`UpdateSong`**, tag a new release,
   bump this repo's `go.mod` - a complete, independent unit of work in a
   separate repo, gated on step 1's confirmation.
3. **The archive action itself**: one controller function (matching
   `SetPositionPreference`'s shape), wired to a confirm-guarded button
   somewhere in the UI (see the open "where it surfaces" question above -
   settle this before writing the template code, since it decides which
   existing fragment gets the new button/re-render target). Independent of
   the nudge below - archiving works perfectly well with no badge driving
   it, someone can just decide to archive a song they already know is
   dead.
4. **The "not sung in N days" nudge**: settle the threshold (a real number
   or an org-level setting) and where it surfaces (same open question as
   step 3, likely resolved together), then add the field/computation
   mirroring `NewSongLabel`/`reinforcementLabel`.
5. **The seasonal exemption**: the new `song_tag_categories` row
   (migration) plus the nudge's tag-check - can ship slightly after step 4
   if needed, but shouldn't ship *later* than the nudge going live broadly,
   since an un-exempted seasonal song would otherwise nag every time it
   comes up until the exemption exists.
6. **(Optional, separately scoped)** an "Archived songs" browse view with
   its own un-archive action, only if the open question in "Un-archiving"
   above is decided as worth building.

## Verification (once built)

- `go build ./...` (empty output = success) - after the `go-pco` bump
  lands, confirms the new `pco.UpdateSong`/`pco.UpdateSongParams` symbols
  resolve.
- `make test`.
- Live: archive a real (disposable/dev-org) song, confirm it disappears
  from `FetchActiveSongs`-backed lists (Build page candidate list, Song
  Tags page, etc.) immediately (i.e. `InvalidateActiveSongsCache` is
  actually being called and actually works, not just "would eventually
  expire off the 60s cache"), then confirm PCO's own UI still shows the
  song, correctly marked archived/hidden there.
- Live: confirm archiving does **not** touch any local row referencing
  that song's id (`song_tags`/`song_person_preferences`/
  `song_relationships`/`song_roles`) - query for it before and after, rows
  should be byte-identical, per this plan's "no FK, no cleanup needed"
  reasoning above.
- Confirm the nudge badge only appears past the chosen threshold and never
  for a song carrying the seasonal-exemption tag, once that tag exists.
- `docs/pco-api-risks.md`: this plan's reasoning is that archiving needs
  **no new entry there** - it's a one-off, user-triggered write (one
  `UpdateSong` call per explicit click), not a hot-path or per-item-in-a-
  loop call the way the risks doc's existing entries are about. Confirm
  that reasoning still holds once actually built (i.e. the real
  implementation doesn't end up calling `UpdateSong` from inside any loop
  or render path) rather than assuming it in perpetuity - if it ever does
  grow a loop-shaped call site, that's exactly the kind of change
  `CLAUDE.md`'s "PCO API calls" section says belongs in that doc.
