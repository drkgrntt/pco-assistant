# Plan: redesign the Song Roles setup page as role buckets

Not built yet - written up for review before anyone picks it up, same
spirit as `docs/plans/shipped/song-tags-setup-redesign-plan.md` (that doc
is this one's direct model; read it first, it's cited throughout below).

Prompted by direct feedback in `docs/issues.md`: "I'm ready to start
thinking about a redesign of the song roles page. A table is fine, but
given the more or less static nature of the roles, I feel like buckets
might make more sense here. I want to take inspiration from how we're
doing it on the tags page, because I like how we did that, but given that
this is a little different, I could see that difference meaning something
visually different."

## Current state (`internal/views/scheduling/song_roles.templ`,
`internal/server/controllers/sets_roles.go`, `internal/scheduling/song_roles.go`)

Pre-redesign Song Tags' own shape, basically unchanged since: one plain
`<select name="song_id">` of every active song, a `<select name="role">`,
Add - then `SongRolesTable`, a single flat `SortableTableWrap` with one
row per **tag** (a song/role pair), sorted `created_at DESC`. A song
tagged with two roles appears as two separate rows, each repeating its own
title. Same edit/remove-icon-button-per-row mechanism as every other
setup page (`data-edit-form`/`editRow`/`resetSetupForm`).

The role vocabulary itself is small, fixed, and DB-driven, not
user-authored free text like a tag value: `song_role_types` currently
holds exactly six rows (migrations 00015/00020) -
**Opener, Response, Communion, Prayer, Reflection, Baptism** - each with a
`sort_order`. `ListSongRoleTypes` is the only place that ever reads this
table; adding a seventh role is a migration, not a UI action, and nothing
here needs to guess at that number or hardcode it - the page already
renders generically over whatever `ListSongRoleTypes` returns.

## Why "buckets," and why that's a different shape than Song Tags' cards

Song Tags' redesign (`song-tags-setup-redesign-plan.md`) grouped its page
**by song** - a persistent card per song, tags as chips inside it - because
a tag's own vocabulary is unbounded free text (`song_tags.value` is
whatever anyone types), so "which songs have which tags" only makes sense
browsed from the song's side; there's no fixed, enumerable list of tag
values to build a stable page structure around.

Song Roles is the opposite shape: the role side is the small, fixed,
enumerable one (six buckets today, DB-driven if that ever grows), and the
song side is the large, open one (the whole active library). That's the
"a little different" the issue calls out - the natural grouping here is
**by role, not by song**: one persistent `components.Card` per role type
(Opener, Response, Communion, ...), each showing every song currently
tagged with that role as chips, with its own small inline "+ Add a song to
this bucket" form scoped to that role (mirrors `SongTagCard`'s own inline
add-form pattern, just with the fixed field being `role` instead of
`song_id`). Same visual language as Song Tags (`components.Card`, chip
rows, inline `<details>`-disclosed add form, edit/remove icon buttons per
chip) - the redesign inspiration the issue asked for - just transposed
onto the axis that's actually static here.

This also answers a real usability gap the current flat table has: "what
are our good opener songs" (the actual question someone building a set
shape has in mind) today means scanning/sorting the whole table for
`role = opener` rows and reading off titles - a bucket-per-role page
answers it by construction, no scanning needed.

## Proposed redesign

`SongRolesTable` (or its replacement) reshapes to one card per
`SongRoleType`, in `sort_order` (matches how the vocabulary already
displays everywhere else - the entry form's `<select>`, the Build page's
role-filter chips), each rendering:

- The role's label as the card title (`components.Card(t.Label)`).
- Every song currently tagged with that role, as chips - `SongTagCard`'s
  chip-with-inline-edit/remove-buttons treatment, minus the
  category/`BadgeTitled` styling (a role bucket has no sub-categorization
  the way a tag chip needs to show which category it's in - the bucket
  itself already says everything a tag chip's badge would).
- An inline "+ Add a song" `<details>`/form scoped to that role code
  (hidden `role` input, a song picker for `song_id` - see "Open questions"
  below on which song-selection widget to use).
- An empty-state line ("No songs tagged yet") for a bucket with nothing in
  it, same as `SongTagCard`'s `song-tag-empty` treatment - every bucket
  should render even at zero, the same reason Song Tags renders every song
  even untagged: seeing the empty buckets is itself useful ("we don't have
  a baptism song on file yet").

```
Opener
  Amazing Grace   How Great Thou Art   [+ Add a song]

Response
  10,000 Reasons   [+ Add a song]

Communion
  [No songs tagged yet]   [+ Add a song]

...
```

Six persistent cards (today's count) is a fundamentally different render
cost than Song Tags' "one card per active song" (could be hundreds) - no
filter box, no client-side hide-non-matching-cards mechanism needed the
way `filterSongTagCards` exists for Song Tags' page. That's the concrete
"visually different" consequence of buckets being the static axis:
this page doesn't inherit Song Tags' filter-chip header at all.

## Open questions to settle before building, not answered by this plan

- **Song selection inside each bucket's add-form**: a plain
  `<select>` of every active song (today's approach, same as every other
  setup page) is more defensible here than it was on the old Song Tags
  page - one bucket's add-form only needs *a* song, not a search
  experience for "does this song already have tags," and there's no
  `SongPickerField` component to reuse (the Song Tags plan proposed one
  but never built it - see that doc's top-of-file note: it turned out
  unnecessary once Song Tags went to unconditional per-song cards).
  Still worth asking directly: does a `<select>` of the whole active
  library feel fine inside a small inline form, or does this need its own
  lightweight filtered picker? Recommend starting with the plain
  `<select>` (cheapest, matches every sibling setup page today) and only
  building a picker later if it proves painful in practice.
- **A song already in one bucket, added to another**: today's flat table
  allows a song to hold multiple roles (multiple rows). Buckets should
  keep that - the same song's title can legitimately appear as a chip in
  more than one card (e.g. a song that works as both Opener and
  Response). Nothing here changes the data model (`song_roles` stays a
  song/role pair, unique per pair per `CreateSongRole`'s `ON CONFLICT`) -
  just confirming the UI shouldn't try to prevent or warn about this,
  since it's an intended, ordinary case.
- **Moving a song between buckets**: today's per-row Edit button
  (`data-edit-form="song-role-form"`) lets you retarget a tag's `song_id`
  *or* its `role` from one shared form. With one form per bucket instead
  of one shared form, does editing a chip need to be able to change which
  bucket it's in (a `<select name="role">` inside the edit fields,
  defaulting to the current bucket), or is "remove from this bucket, add
  to the other" an acceptable two-step replacement? Needs a real answer,
  not an assumption - either is defensible, but they render differently.
- **Sort order within a bucket's chips**: alphabetical by title (cheap,
  predictable) vs. `created_at` (today's implicit order, "most recently
  tagged first") - Song Tags doesn't have this question since its chips
  are a song's own tags, already small; a popular bucket here could hold
  many songs. Recommend alphabetical for scan-ability, but flag for a real
  decision rather than defaulting silently.
- **Does the Build page's own role-filter chips or quick-add ("Mark a
  role") need any change?** Almost certainly not - both already read
  generically from `ListSongRoleTypes`/`ListSongRoles` and don't render
  this setup page's markup - but worth a quick check once the setup page
  itself is rebuilt, so nothing here creates a mismatch between how roles
  look on the two pages.

## Suggested build order, once the open questions above are answered

1. Rebuild `SongRolesTable` (rename to something like `SongRoleBuckets`)
   as one `components.Card` per `SongRoleType`, grouping `[]SongRole` by
   role code (a small in-Go grouping helper, mirrors `tagsBySong`'s
   grouping-by-song shape but keyed the other direction) - no store/schema
   change needed, `ListSongRoleTypes`/`ListSongRoles` already return
   everything this needs.
2. Each bucket's inline add-form, scoped `role` hidden input, reusing
   `CreateSongRole`/`sc.store.CreateSongRole` unchanged.
3. Whatever the edit-in-place answer above turns out to be - adjust
   `data-edit-form`/`data-edit-fields` accordingly (likely one edit form
   per bucket, same shared-form-per-card pattern `SongTagCard` uses for
   its own per-song add form, rather than one global shared form the old
   flat table used).
4. `songRolesHelp`'s copy update if the page's shape changed enough to
   warrant it (currently describes it correctly regardless - "tag songs as
   good openers or good response songs" - probably needs no wording
   change, just confirm).

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`.
- Live: confirm a song can hold multiple roles and shows correctly in
  each bucket it belongs to; confirm add/edit/remove all still work;
  confirm the Build page's existing role-filter chips and "Mark a role"
  quick-add still work unchanged (no schema change here, so this should be
  a non-event, but worth confirming directly).
