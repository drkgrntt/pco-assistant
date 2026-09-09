# Plan: tether "worship leader"/song leader to something real in PCO

Written 2026-09-09, prompted by direct feedback from a meeting with the
worship pastor (`docs/issues.md`): "worship leader is a first class
citizen in our application but it doesn't write to anything (that we
could find) in PCO. There apparently exists a 'song leader' field for
songs in PCO that we should tether it to so that the proper notifications
and whatnot can be sent and utilized."

## Current state

The "song leader" feature (`docs/plans/shipped/song-leader-plan.md`,
shipped 2026-09-07) is real and first-class *locally*: a per-occurrence
`set_songs.leader_person_id` column, a `PersonPickerField` on the song
detail card, and on Publish/Refresh a line (`"Leading: Jane Smith"`) gets
merged into whichever Item Note category the org designated as "the
vocal one" (`syncLeaderNote`/`composeLeaderNoteContent`,
`internal/server/controllers/leader_note.go`, wired into
`sets_publish.go`).

That was a deliberate, documented choice at the time (see that plan's
"Publish-time sync into the existing vocal notes category" section) - the
explicit ask then was "feed into vocal notes," not "trigger PCO
notifications." The worship pastor's new feedback is a different, bigger
ask: he wants this to actually *do something* in PCO the way a real
Planning Center assignment does - notify the leader, show up in PCO's own
scheduling UI, etc. An Item Note can't do any of that; it's an opaque
string with zero notification behavior.

## Live-verified: there is no "song leader" field to tether to

Checked directly against the signed-in dev PCO org (not just
documentation - PCO's docs have contradicted actual behavior before, per
CLAUDE.md's standing instruction on this):

- **Item** (`services/v2/.../items/{id}`) attributes: `created_at`,
  `custom_arrangement_sequence*`, `description`, `html_details`,
  `item_type`, `key_name`, `length`, `sequence`, `service_position`,
  `title`, `updated_at`. Relationships: `plan`, `song`, `arrangement`,
  `key`, `selected_layout`, `selected_background`. **No leader field, no
  leader relationship.**
- **Song** (`services/v2/songs/{id}`) attributes: `admin`, `author`,
  `ccli_number`, `copyright`, `hidden`, `last_scheduled_at`,
  `last_scheduled_short_dates`, `notes`, `themes`, `title`. No
  relationships at all. **No leader field.**
- **Arrangement** attributes: `bpm`, `chord_chart*`, `lyrics*`, `meter`,
  `name`, `notes`, `sequence*`, plus print settings. Relationships:
  `updated_by`, `created_by`, `song`. **No leader field.**

So whatever the worship pastor saw as "a song leader field" inside
Planning Center's own UI is not a literal Item/Song/Arrangement
attribute this API exposes - go-pco (checked at
`github.com/drkgrntt/go-pco@v0.23.0` and its upstream source, itself
built resource-by-resource against PCO's live API, not a blind spec
generator) has no such field either, for the same reason.

**Next step before building anything: find out, together with him, what
he's actually pointing at in PCO's UI.** The most likely real candidates:

1. **A Team + Team Position literally named "Worship Leader"** (or
   similar), assigned via PCO's normal scheduling flow - i.e. a *person*
   scheduled the ordinary way, not a per-song field at all. This is the
   one mechanism confirmed to exist and confirmed to drive real PCO
   notifications: `PersonTeamPositionAssignment`/`TeamMember`
   (`PlanPerson`) - the same resource this app already writes for every
   other roster assignment (`internal/server/controllers/
   rosters_publish.go`'s `pco.CreateTeamMember`/`pco.DeleteTeamMember`).
   PCO's own scheduling notifications (accept/decline emails/texts) are
   already wired to exactly this resource - it's what actually "sends
   the proper notifications," which an Item Note never could.
2. Something on the **Plan** resource itself (not checked yet - if (1)
   doesn't match what he's describing, pull a plan's own attributes/
   relationships live the same way, before guessing further).

## The design fork this implies

If it turns out to be (1) - **worship leader is really "assign this
person to the Worship Leader position on this plan's Team," not a
per-song field**:

- This is a genuinely different shape than today's per-*song*
  `leader_person_id` - it's per-*plan* (or per team-position-assignment),
  matching how every other roster position already works
  (`internal/scheduling/rosters.go`), not `set_songs`. A plan's worship
  leader is one person for the whole service in the common case, which
  doesn't map cleanly onto "a leader per song" (today's model exists
  specifically because a different person can lead different songs in
  the same plan - see the shipped plan's own reasoning for why it's on
  `set_songs`, not the set/plan).
- Two people are involved in different capacities: today's "who's
  singing lead vocals on this specific song" (correctly per-song, keep
  as-is) is not automatically the same person as "who is *the* worship
  leader for this service" (a Team/position concept, per-plan). Worth
  confirming with him whether he actually wants **both** to keep
  existing (per-song vocal lead unchanged, *plus* a new per-plan
  "Worship Leader" position tethered to a real PCO Team assignment for
  notifications) rather than collapsing them into one - collapsing loses
  the per-song flexibility the shipped feature was explicitly built for.
  Needs a real decision, not a default.
  - If he does want an actual per-plan Worship Leader position added:
    check whether the org's PCO Team/Position setup already has one
    (many worship-planning PCO orgs do, as a Team of size 1) - if so,
    this app's existing roster machinery (`rosters.go`,
    `rosters_publish.go`) may already support assigning to it with zero
    new code, and the only real gap is *visibility/emphasis* (surfacing
    "who's the Worship Leader" prominently, e.g. on the Build page's
    status summary or "at a glance" card, rather than treating it as
    just another position in the list). That would make this a much
    smaller build than inventing new sync plumbing.
  - If no such position exists in PCO yet, creating one is an org-side
    PCO configuration step (Teams/Positions are managed in PCO itself,
    not something this app creates), not something to build here at
    all - confirm this before assuming it's in scope.

If it turns out to be (2) or something else entirely: this plan needs a
second live-verification pass once we know what field he's actually
referencing, before any design decision is safe to make.

## Which product promise this serves

Per CLAUDE.md's "Product direction" test: **protect the service** (the
right person actually gets notified/confirmed to lead, not silently
missed because the "leader" concept lives only in this app's own
database) with a secondary **protect the team** angle (a real PCO
assignment means the worship leader gets PCO's own accept/decline
flow like every other scheduled person, instead of being invisible to
PCO's own reminder system).

## Suggested next step (not a build order yet - too much is unverified)

1. **Confirm with the worship pastor** exactly what UI element in PCO he
   was pointing at when he said "song leader field" - screenshot or a
   direct walkthrough, not a guess.
2. Once identified, live-verify its actual API shape the same way this
   doc verified Item/Song/Arrangement (don't trust PCO's docs alone, per
   CLAUDE.md's standing instruction).
3. Only then decide: extend the existing per-song `syncLeaderNote` path
   to also/instead write a real structured field (if one exists after
   all and was just missed here), or build the per-plan Team-position
   assignment path described above (most likely outcome), or something
   else the live check reveals.
4. If the Team-position path is confirmed as the right shape, the
   existing `rosters_publish.go` `CreateTeamMember`/`DeleteTeamMember`
   pattern is the implementation template - this would likely be a
   moderate build (surfacing/dedicating a "Worship Leader" position
   prominently), not a from-scratch integration.

## Verification (once a direction is picked)

- `go build ./...` / `make test` clean.
- Live: assign a worship leader through whatever mechanism gets built,
  confirm in the real PCO org that the assignment shows up as a genuine
  Team Member/PlanPerson (or whatever the confirmed real field turns out
  to be), and that PCO's own notification (if the mechanism carries one)
  actually fires.
