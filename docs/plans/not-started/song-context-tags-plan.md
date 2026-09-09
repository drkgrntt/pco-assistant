# Plan: "where this song gets sung" (big church / youth / other campus)

**Open question resolved 2026-09-09**: confirmed directly with Derek that
the three contexts do **not** map 1:1 to this org's actual PCO Service
Types - so **Option B below is the path**, not Option A. This doc is
otherwise unchanged from when it was written (Option A's reasoning is
kept for context, not because it's still live) - still not-started, no
code written yet. `song_context_types`/`song_contexts` (the org-editable
lookup + join table) is the next real open question before building:
see Option B's own note on why it likely needs its own tiny CRUD rather
than copying `song_role_types`' fixed-lookup precedent verbatim.

Written 2026-09-09, prompted by direct feedback from a meeting with the
worship pastor (`docs/issues.md`): "He asked if there's a way to mark
songs as 'we do this here for big church', 'here youth', and 'other
campus'. Right now, the best way is tags, but it's not super elegant. We
need a new solution for this."

## Current state

The only thing resembling this today is the generic `song_tags` system
(`internal/scheduling/song_tags.go`, migration `00016`) - `theme`/
`scripture`/`idea`/`seasonal` categories, each holding free-text values,
purely for later lookup ("find this song by sermon theme"). That
migration's own comment draws the exact line this feedback is bumping
into:

> Song tags are separate from `song_roles` ... roles feed the set
> builder's candidate filtering, **tags are purely descriptive**.

A "big church / youth / other campus" value stuffed into a tag category
is exactly that: a label with no operational effect. It doesn't filter
the candidate list when building a youth-service plan, doesn't warn if a
big-church-only song gets added to the wrong context, and doesn't
restrict anything - it just sits there to be read, which is why it reads
as inelegant. The feedback isn't "tags are ugly," it's "this fact should
*do* something."

`song_roles` (migration `00004`/`00015`, `song_role_types` lookup table
`00015`/`00020`) is the existing precedent for a classification that
*does* something: a small, DB-driven lookup of codes (`communion`,
`prayer`, `reflection`, `baptism`, ...), a `song_roles` join table, and
real UI wiring - `set_builder.templ`'s role-filter chips
(`toggleRoleFilter`, ~line 1168-1175) let a director filter the "Browse
all songs" candidate list down to just songs tagged with a role, live in
the set builder, not on a separate setup page nobody visits mid-build.

## Live-verified: PCO has no help to offer here either

Checked directly against the signed-in dev PCO org (`services/v2/songs`,
`/arrangements`) - a Song's own attributes are `admin`, `author`,
`ccli_number`, `copyright`, `hidden`, `last_scheduled_at`,
`last_scheduled_short_dates`, `notes`, `themes`, `title` - no campus/
context/congregation field, no relationship to Service Types at all. So
there's no PCO-side "which service type is this appropriate for" concept
to hook into; whatever ships here is purely local, same as `song_roles`.

One thing PCO *does* already model, and this org already uses: **Service
Types** (`services/v2/service_types` - this dev org has "Sunday Service"
and "Wednesday Night"). That's almost certainly a closer match to "big
church" / "youth" than anything this app would invent - the worship
pastor's three contexts likely map either directly to existing Service
Types, or to a finer split *within* one Service Type that PCO has no
concept of at all (e.g. two campuses sharing one Service Type). Worth
confirming directly with him before building: **do "big church" /
"youth" / "other campus" already correspond 1:1 to this org's actual PCO
Service Types**, or are they a different, finer axis?

## The real design fork

**Option A - reuse Service Types as the vocabulary.** If the three
contexts really are "which Service Type," there's nothing to build at
the data-model layer at all - a plan already belongs to exactly one
Service Type (`serviceTypeID` is already threaded through every Build
page route). The actual gap would just be: does a song's *eligibility*
for a Service Type need to be tracked explicitly (so the candidate list
for "Youth" can be filtered/warned down to youth-appropriate songs), or
is "which Service Type a song gets used in" adequately visible already
via `last_scheduled_at`/history? If explicit eligibility is wanted, this
becomes "a `song_service_type_eligibility` table, one row per
(song, service_type)" - structurally identical to Option B below, just
keyed on the org's real Service Type ids instead of a new invented
vocabulary, and with zero new setup UI needed to name the contexts (PCO
already named them).

**Option B - a new `song_contexts` classification, `song_roles`-shaped.**
If the three contexts are a real axis PCO doesn't model (e.g. "other
campus" isn't its own Service Type, or a church wants campus/context
tracking independent of however many Service Types they happen to have
configured), build the direct parallel to `song_roles`:

- `song_context_types` lookup table (code/label/sort_order), seeded with
  sensible defaults (`big_church`, `youth`) but org-editable the way nothing
  in `song_role_types` currently is - this one probably *should* be
  editable per-org from the start, since "which contexts exist" is
  exactly the kind of thing that varies wildly per church (a single-site
  church has none of this; a five-campus church needs five). Compare
  against `docs/plans/shipped/song-tags-setup-redesign-plan.md`'s decision
  to keep tag categories as a fixed lookup - a context list is more like a
  campus list than a tag taxonomy, so this may need its own tiny CRUD
  rather than copying that precedent verbatim. Needs a real decision, not
  a default.
- `song_contexts` join table (`song_id`, `context_code`), same shape as
  `song_roles`.
- Candidate-list filter chips in `set_builder.templ`, identical UI idiom
  to the existing role-filter chips (`toggleRoleFilter`) - a director
  building a Youth plan filters the browsable list down to
  youth-appropriate songs the same click away role filtering already is.
- Optional follow-on (not required for v1): a soft warning, in the same
  family as the existing repetition/pairing warnings
  (`internal/scheduling`'s warning algorithms), if a song gets added to a
  plan under a Service Type/context it isn't tagged for - "this song is
  tagged Youth only" - protecting the service the same way those warnings
  already do, rather than silently allowing a mismatch.

**Resolved: Option B.** Confirmed directly - the three contexts don't map
1:1 to this org's Service Types, so this is a real axis PCO doesn't
model, not just a filter over existing Service Type ids. The Option B
build below is the path: a `song_roles`-shaped classification, not a
`song_tags` variant - the whole point of this feedback is that a passive
label isn't what's needed; a real filter/warning is.

## Which product promise this serves

Per CLAUDE.md's "Product direction" test: **protect the service** -
picking the wrong song for the wrong context (a big-church anthem in a
stripped-down youth set, or vice versa) is exactly the kind of
service-flow mistake this app exists to catch before it reaches a plan,
not a team/relational concern.

## Suggested build order (Option B)

1. Decide `song_context_types` editability first (Option B's own note
   above) - a fixed lookup like `song_role_types`, or a small per-org CRUD
   like a campus list. This changes migration 1 below either way.
2. Migration(s): `song_context_types` lookup table + `song_contexts` join
   table, following `song_role_types`/`song_roles`' exact shape (adjusted
   for editability per step 1).
3. `scheduling.Store` methods mirroring `song_roles.go`'s existing
   `Add`/`Remove`/`List` shape.
4. Candidate-list filter chips in `set_builder.templ`, reusing
   `toggleRoleFilter`'s pattern (JS + hidden input + htmx-driven refetch)
   rather than inventing a second filtering mechanism alongside it -
   ideally the same filter row, not a second row of chips competing for
   attention.
5. If org-editable: a small setup surface for naming contexts, following
   whichever of this app's existing per-org settings surfaces fits best
   (check `account.go` before inventing a new one, same instruction
   CLAUDE.md gives for other per-service-type settings).
6. Optional: the plan-mismatch warning, once the base classification
   exists and has real usage to judge whether it's actually wanted.

## Verification (once built)

- `go build ./...` / `make test` clean.
- Live: tag a song for one context only, confirm it does (or doesn't,
  depending on the warning decision) show up when building a plan under a
  different context; confirm the filter chips actually narrow the
  candidate list the same way role filters do today.
