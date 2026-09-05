# Plan: surface song attachments (lead sheets) on the Build page

Written 2026-09-04, prompted by revisiting the "song vocal range" idea in
`docs/ideas.md` - specifically whether PCO's lead sheets could feed an
*automatic* range calculation. Not built yet; written up for later, same
spirit as `docs/plans/shipped/pco-roster-cold-start-plan.md`/`docs/plans/shipped/pco-throttle-plan.md`.

## Why automatic range extraction is out of scope

- `Arrangement.ChordChart`/`Lyrics` (already wrapped in `go-pco`) are text
  only - chord symbols and lyrics, no melodic notation, nothing a range
  could be computed from.
- PCO's real "lead sheet" mechanism is **Attachments** - arbitrary files a
  church uploads to a song (PDF from SongSelect/PraiseCharts, scans,
  audio), labeled with an org-defined "attachment type." `go-pco`'s own
  doc comment on `SongData` confirms this app doesn't wrap that endpoint
  yet, and PCO's docs confirm there's no structured notation (MusicXML
  etc.) behind it - just a file blob. Extracting a range from that would
  mean optical music recognition on arbitrary, often-scanned PDFs - a
  different order of engineering than anything else in this codebase, and
  out of scope. If this ever gets revisited, it's a standalone R&D spike,
  not an extension of the feature below.

## What's actually worth building: attachment links as a manual aid

Not automated extraction - just surfacing a song's PCO attachments as
reference links on the Build page, so a director can glance at the real
lead sheet/chord chart while setting a key or (once it exists) a vocal
range. Stands on its own value independent of vocal range ever getting
built (any director building a set benefits from a one-click link to the
actual chart) - vocal range itself (person or song) doesn't exist in this
codebase yet (confirmed - no `vocal_range`/range storage anywhere).

Side finding worth recording: `Arrangement.Lyrics` **is** already exposed
and already wrapped - this resolves the "needs checking against the live
API" gap on `docs/ideas.md`'s Lyric-derived tags bullet (not being built
now, just now verified as possible). Worth updating that bullet
independently of whether this plan ever gets built.

## Approach

Same shape as the existing arrangement-info cache
(`song_arrangement_cache`/`fetchAndCacheArrangementInfo`/`SyncSongInfo`):
fetch is cached at sync time, never live in a render path, per CLAUDE.md's
PCO rate-limit doctrine. A song can have zero, one, or many attachments,
and there's no reliable cross-org way to know which one is "the lead
sheet" (the attachment-type label is org-defined, not a fixed enum) - so
this surfaces **all** of a song's attachments as a small "Files" list
(filename + link), not a single guessed "Lead Sheet" field.

**Verify live first** (per CLAUDE.md's "verify against the signed-in dev
PCO account" rule): confirm the actual attachments endpoint path/attribute
names against a real song, and specifically whether the attachment's `url`
is a long-lived link or an expiring pre-signed one - that determines
whether the URL itself is cacheable or needs to be fetched fresh on click.

### 1. `go-pco` (separate repo, `~/projects/go-pco`)

Add `attachments.go`, mirroring `arrangements.go`'s shape:
- `GetAttachments(ctx, songID, params)` - list, read-only only (this app
  never creates/edits attachments).
- `AttachmentData`/`AttachmentAttributes` covering filename, content type,
  file size, `url`, created/updated timestamps - exact field names from
  the live check above.
- Tests mirroring `arrangements_test.go`'s style.
- Tag a new version (next after v0.20.0) once merged.

### 2. Database

New migration `internal/database/migrations/00034_song_attachment_cache.sql`
- a new table (not new columns on `song_arrangement_cache`, since that
table is one-row-per-song and a song can have multiple attachments):

```
song_attachment_cache(organization_id, song_id, pco_attachment_id,
  filename, content_type, cached_at)
```

Deliberately **not** caching `url` if the live check confirms it's
expiring - see step 4.

New `internal/scheduling/song_attachment_cache.go`, same shape as
`song_arrangement_cache.go`: `UpsertSongAttachments` (replace-all for one
song, since a stale attachment removed in PCO shouldn't linger),
`SongAttachments(ctx, organizationID) (map[string][]SongAttachmentInfo,
error)` - bulk map read for a whole page render, matching
`SongArrangementInfo`'s existing bulk-read pattern used by
`buildSetBuilderData`.

### 3. Sync (`internal/server/controllers/song_arrangement_cache.go`)

Extend `fetchAndCacheArrangementInfo` (or add a sibling call alongside it
in the same call sites: `resolveArrangementInfo`'s cache-miss path,
`runSongInfoSync`, `RefreshSetSongArrangementInfo`) to also fetch and
upsert this song's attachments - one more PCO call per synced song. Update
the cost comments this touches (`SyncSongInfo`'s doc comment says "two
live PCO calls per uncached song" - becomes three; `songInfoSyncBatchPause`'s
math comment assumes 2 calls/song and needs re-deriving for 3).

### 4. Attachment link/URL freshness

If the live check shows `url` expires: don't cache it. Add a small
click-triggered route (e.g.
`GET /scheduling/sets/:service_type_id/:plan_id/set-songs/:set_song_id/attachments/:attachment_id`)
that live-fetches a fresh URL from PCO and redirects to it - a single
on-demand call gated behind an explicit click, same rate-limit posture as
`RefreshSetSongArrangementInfo`'s existing "Update from Planning Center"
button, not a render-path cost. If it's long-lived, cache it directly in
`song_attachment_cache` and link straight to it (simpler - prefer this if
the live check allows it).

### 5. View (`internal/views/scheduling/song_detail_card.templ` +
`set_types.go`)

Add `Attachments []SongAttachmentView` to `SetSongView` (mirrors
`ArrangementInfo string`'s existing field), populated in
`sets_builder.go`'s `buildSetBuilderData` from the new bulk map, same call
site as the existing `ArrangementInfo: formatArrangementInfo(info)` lines.

In `songDetailCard`, add a small "Files" block right under the existing
arrangement-info paragraph (`#song-arrangement-info-<id>`) - one link per
cached attachment (filename as link text), nothing shown when there are
none. Also extend `SongArrangementInfoRefreshFragment` to re-render this
block, same as it already re-renders the arrangement-info paragraph, since
"Update from Planning Center" is the one button that forces a live
refresh.

### 6. Docs

- `docs/pco-api-risks.md`: add an entry for this new per-song call
  (extends the existing sync's cost, doesn't introduce a new hot
  render-path risk - same "cached at sync time" pattern as arrangement
  info).
- `README.md`: note the new "Files" links under whatever section
  documents the Build page / Song Info sync.
- `CLAUDE.md`: no architectural shift, likely no change needed unless a
  new `internal/` package boundary gets crossed differently than
  described here.
- `docs/ideas.md`:
  - Mark the Lyric-derived tags "not yet verified" line as verified
    (`Arrangement.Lyrics` is real and already wrapped) - still not built,
    just resolve the open question.
  - Once this feature actually ships, strike the relevant line under
    **Song vocal range (relative to key)** noting the lead-sheet-link
    piece landed (per CLAUDE.md's "mark `~~struck~~` with a pointer"
    convention) - do this as the last step of implementation, not before.
  - Mark this plan itself with a status line at the top once shipped or
    superseded, same convention as `docs/plans/shipped/pco-throttle-plan.md`'s "Status:
    shipped ..." line.

## Verification (once built)

- `go build ./...` in both repos (empty output = success - IDE
  diagnostics on `.templ`-generated code lag behind, per CLAUDE.md).
- `make test` (church repo) and `go test ./...` (go-pco repo).
- Live: open the Build page for a real plan in the dev PCO org, confirm a
  song with a real uploaded attachment shows its file link and it opens
  the actual file; confirm a song with no attachments shows nothing
  broken; click "Update from Planning Center" on one song and confirm the
  Files list refreshes.
- Re-check `docs/pco-api-risks.md`'s math against the actual
  3-calls-per-song batch pacing once implemented, not just estimated.
