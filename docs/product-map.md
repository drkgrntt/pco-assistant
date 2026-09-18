# Product Map

**Generated: 2026-09-18.** This is a point-in-time snapshot of Resonance's
site map, feature set, and cause-and-effect mechanics, assembled by reading
the actual code and docs rather than transcribed from memory. It will drift
as the app changes - treat it the way `README.md`/`docs/ideas.md` are
treated elsewhere in this repo (update it when a change it describes lands,
or regenerate it wholesale if it's gone stale). Built originally as raw
material for demo-video scripting, but kept here as a general-purpose
reference to the whole product's shape.

For the product's *why* (positioning, the two promises every feature is
checked against, the friction-reduction discipline), see `docs/ideas.md`'s
"Vision / positioning" section - summarized below, but that doc is the
source of truth, not this one.

---

## 1. Product vision (source: `docs/ideas.md`)

Resonance is a **supplement** to Planning Center Services, not a
replacement - it exists only because of gaps PCO leaves for worship
scheduling: song relationships, avoid-pairing/mentorship-aware team
building, repeat-song and burnout warnings, before-you-publish conflict
detection.

**True North** - turning the relational and musical context a director
already carries in their head (or a sticky note) into instant,
before-you-publish guardrails. Every feature should serve one of two
promises:

- **Protect the team** - prevent burnout, respect stated preferences,
  avoid known relational friction, encourage mentorship pairing.
- **Protect the service** - prevent song-repetition fatigue, catch a
  broken set flow, surface conflicts before the plan goes out, not after.

This is a marketing/positioning test for *which* features belong on the
roadmap. Friction reduction is a separate, standing discipline for *how*
every feature gets built regardless of which promise it serves: fewer
clicks, no new UI pattern where an existing one already works, no reason
to bounce back to PCO.

A concrete implication: features that duplicate something PCO can already
do directly - setting a song's key, building the order of service itself -
aren't disqualified by that duplication. They're the point. The premise is
"plan here, then Publish hands PCO a complete result" - so any gap between
what this app lets someone set and what Publish actually pushes means the
user has to go clean it up by hand in PCO, which defeats the whole premise.
That makes the sync/publish code the **highest-friction-bar code in the
app**, not incidental plumbing.

The wedge is worship teams specifically, not "volunteer scheduling" in
general.

The Home dashboard's onboarding checklist literally groups the 8 setup
pages under headers "Protect the service" / "Protect the team" - this
through-line is surfaced in the UI itself, not just a positioning doc.

---

## 2. Site map

Request pipeline: `RequestID` -> `Recover` -> `SecurityHeaders` -> `CORS`
-> static assets served unauthenticated -> `RequireAuth` ->
`RequireActiveOrganization` -> `PopulateNavState` -> controllers.
`RequireAuth` allowlists `/health`, `/login`, `/auth/*`, `/static/*`,
`/piano-samples/*`, the marketing pages, and `/billing/webhook` (Stripe
signs it instead). `/` is soft-auth: Landing when signed out, dashboard
when signed in.

Full-page routes render the whole `Layout` wrapper; nearly every
POST/DELETE and nested GET under `/build/`, `/sets/`, `/rosters/` returns
a narrower htmx fragment swapped into the existing page instead (so typing
in a search box never clobbers itself mid-keystroke - see `build.go`'s
`BuildPlansSearch` doc comment).

### Main app (`cmd/api`, PCO OAuth gated)

| Area | Routes | Notes |
|---|---|---|
| Auth | `GET /login`, `GET /auth/pco/start`, `GET /auth/pco/callback`, `POST /auth/logout` | OAuth start/callback rate-limited 20/min |
| Health | `GET /health` | JSON DB check |
| Home/marketing | `GET /`, `GET /pricing`, `/how-it-works`, `/privacy`, `/terms`, `/sitemap.xml`, `/robots.txt` | `/` branches Landing vs dashboard by auth state |
| Account | `GET /account`, `POST /account/cache/song-info`, `/account/cache/person-data`, `POST /account/sessions/:id/sign-out` | identity, billing, cache clears, seat management |
| Billing | `GET /billing`, `POST /billing/checkout`, `/billing/portal`, `/billing/webhook` | real Stripe Checkout/Portal/webhook |
| Feedback | `GET/POST /feedback` | POST rate-limited 10/min, sends synchronously |
| Dev-only (`APP_ENV=local`) | `GET /pco/people`, `/pco/plans`, `/pco/songs`, `GET /style-guide` | raw PCO JSON explorers + live design-system reference |
| Setup pages (8, see §6) | `/scheduling/song-relationships`, `/song-preferences` (+`/people-search`), `/song-roles`, `/song-contexts` (+ types CRUD, bulk), `/song-tags` (+ bulk, suggestion ignore), `/person-relationships`, `/vocal-range`, `/song-vocal-range`, `/position-preferences` (+ `/assignments`) | each: `GET`=full page, `POST`/`DELETE`=htmx table fragment |
| Build page - plan picker | `GET /scheduling/build`, `/build/plans`, `/build/plans/search`, `POST /build/plans/:st/:plan/note` | Upcoming/Past tabs, search, local-only plan note |
| Build page - plan view | `GET /scheduling/build/:st/:plan` (+ `/songs-tab`, `/team-tab`), `/run-sheet`, `/vocal-range-matrix` | Songs+Team tabs, lazy-loaded via htmx |
| Build page - song candidates | `GET .../candidates/search`, `/candidates/:song_id/details`, `POST .../candidates/:song_id/archive` | search/detail/archive |
| Build page - plan items | `POST/PUT/DELETE .../items[/:id][/reposition]` | non-song items, write straight to PCO, no local draft |
| Build page - quick add | `POST .../song-tags`, `/song-relationships`, `/song-roles`, `/song-contexts`, `/song-vocal-range`, `/person-relationships`, `/position-preferences` | inline setup-entity creation from within Build |
| Build page - transition notes | `POST/DELETE .../transition-notes[/:id]` | debrief/talking points, local only, never touches PCO |
| Roster | `POST/DELETE /scheduling/rosters/:st/:plan/assignments[/:id]`, `GET .../candidates/:person_id/warnings`, `POST .../assignments/:id/refresh-data`, `POST .../publish`, `/revert`, `/refresh-team-data` | |
| Set songs | `POST /scheduling/sets/:st/:plan/songs`, `DELETE .../set-songs/:id`, `POST .../replace`, `/reposition`, `/duplicate`, `/description`, `/key`, `/leader`, `/refresh-arrangement-info`, `GET .../attachments/:id` | per-occurrence local-draft mutations |
| Set publish/sync | `POST .../items/:id/notes` (PCO immediate), `POST .../publish`, `/refresh-info`, `/revert`, `/merge`, `GET .../sync-check` | the core Publish/Discard/MergeSet trio + 30s live-sync poll |
| JSON API | `POST/GET/DELETE /api/scheduling/song-relationships`, `/song-preferences` | separate from htmx controllers |

### Admin app (`cmd/admin`, separate binary, `ADMIN_TOKEN` HTTP Basic Auth, Tailscale-bound in prod)

| Route | Purpose |
|---|---|
| `GET /` | redirects to `/orgs` |
| `GET /feedback` | every feedback submission, across every org |
| `GET /orgs` | every org's billing/trial status + activity + 8-category setup-completion checklist |
| `POST /orgs/:id/extend-trial` | extend a trial (direct column write or Stripe API) |
| `GET /people` | durable cross-org person directory (email, sign-in count) |

---

## 3. Feature map

Grouped by area; each item flagged **read-only**, **local-write** (this
app's DB only, until Publish), or **PCO-immediate** (writes straight to
real PCO, no local draft concept exists for it).

### Songs

- **Song Relationships** (flows_well / same_theme / avoid_pairing, with
  notes) - *local-write* - drives Build-page suggestions/conflict warnings.
- **Song Roles** (opener, response, communion, prayer, reflection,
  baptism, extensible) - *local-write* - badges + filter on Build page.
- **Song Contexts** (per-org custom list, e.g. big church/youth/campus) -
  *local-write* - badges + filter.
- **Song Tag Suggestions** - *read-only* display (title-word-sharing +
  PCO Themes field, deliberately non-AI) with a *local-write* dismiss
  action.
- **Song Info cache** (key/tempo/meter/length/structure/chord-chart
  badge) - cached from PCO, hourly background refresh + manual refresh,
  occasional live backfill on miss.
- **Song attachments** (lead sheets/chord charts) - cached metadata, but
  clicking always does a live PCO fetch for a fresh URL.
- **Song Vocal Range** - *local-write* - tap-to-hear playback (piano
  samples + Web Audio fallback); transposition preview is display-only.
- **Song/Person Preferences** - *local-write*.
- **Song notes ("This week's note")** - *local-write* until
  Publish/Refresh syncs as the PCO item's Description.
- **PCO note-category boxes** (per service-type category) -
  *PCO-immediate*.
- **Per-occurrence key** - *local-write* (`KeyOverride`) until
  Publish/Refresh resolves/creates a real PCO Key relationship.
- **Song leader (per-occurrence)** - *local-write* until
  Publish/Refresh syncs to PCO's real ItemAssignment "Song leader" field.
- **Debrief log / talking points** - *local-write only, never syncs to
  PCO* - persists across plans, resurfaces as a badge, prints on the Run
  Sheet, writable even on read-only past plans.
- **Build page - Songs tab** - the core set builder: drag-and-drop
  reorder, recommendation engine, avoid-pairing conflict badges,
  repeated-song warning, plan-length warning, key-change warning,
  new-song reinforcement nudge, stale-song nudge, first-song anchor
  memory, relate-after-add prompt, Quick Add, browse/search. **Publish**
  pushes everything to PCO in one sync; **Discard** reverts the draft to
  live PCO; **MergeSet** ("Pull in from Planning Center") adds PCO-side
  additions without discarding local changes.
- **Non-song plan items** (headers, sermon slots, etc.) - always
  *PCO-immediate*, no local draft concept.

### People

- **Person Relationships** (works_well, mentorship [directional],
  avoid_pairing) - *local-write*.
- **Position Preferences** - *PCO-immediate* (backs the real PCO
  `PersonTeamPositionAssignment.schedule_preference` field - PCO has no
  draft concept for it).
- **Vocal Range (person)** - *local-write* - tap-to-hear reference tone.
- **Build page - Team tab** - fills open positions from live PCO
  eligibility; avoid-pairing warnings, works-well/mentorship suggestions,
  serving-frequency warnings, cross-team double-booking warnings,
  blockout-date warnings (live PCO read), quick-add. Publish/Discard
  mirror the Songs tab. Both tabs' summary + Publish/Discard stay visible
  in the sidebar regardless of which tab is active.

### Cross-cutting

- **Past plans & notes** - drafts persist indefinitely; past plans open
  read-only (no Publish/Discard/sync), but can still carry a searchable
  local note and writable debriefs.
- **Run sheet** - one-click print page combining order of service,
  key/tempo/length, item notes, talking points, and roster grouped by
  team/position with short-handed flags - pure read aggregation, no new
  PCO calls.
- **Vocal range matrix** - one-plan grid (roster x set songs), reuses the
  same fit/tip/mismatch logic as per-song badges - pure read.
- **Account** - identity + billing + Data & Caches (two non-destructive,
  self-refilling cache clears) + active-sessions list with per-session
  sign-out (frees a billed seat) + "Install this app" section.
- **PWA / install** - mobile-first, light/dark theming, custom install
  flow for Chromium (`beforeinstallprompt`) and instructional copy for
  iOS (no native event exists there); banner appears after ~3 days' use,
  14-day localStorage cooldown after dismissal.

---

## 4. Domain concepts & cause-effect chains

All computed in `internal/scheduling/*.go` (pure, testable) and rendered
via `internal/server/controllers/sets_builder.go` /
`rosters_builder.go` / `rosters_warnings.go` /
`rosters_candidate_warnings.go`.

| Mechanism | Trigger | Effect |
|---|---|---|
| **Avoid-pairing (songs)** | two songs tagged `avoid_pairing` | `ConflictBadge` on both songs' rows when both are in the draft set |
| **Avoid-pairing (people)** | two people tagged `avoid_pairing` | conflict badge on affected roster rows when both are assigned |
| **Works-well / mentorship** | people tagged `works_well`/`mentorship` (directional) | candidate suggestions when filling an open position near an existing roster member |
| **Repeated-song warning** | song sung in each of the last two published plans for that service type (live PCO history) | "RecentlyRepeated" badge on the row |
| **New-song reinforcement nudge** | song published only 1-2 times since first tracked | "Sung once/twice" badge + "Could use one more sing" section |
| **Stale-song nudge** | sung before, not in ~6 months, not tagged Seasonal | "Not sung recently" badge (soft prompt) |
| **Plan-length warning** | draft's total length deviates from that service type's recent average | standalone banner near the top of the Build page |
| **Key-change warning** | two back-to-back songs are a half-step or tritone apart | badge + expandable suggested-move disclosure on the second song |
| **First-song anchor** | user places a plan's first song after a specific item once | next new plan's first song auto-lands at that same relative position (tracked per service type) |
| **Song tag suggestions** | songs share a significant title word, or PCO's Themes field | dismissible suggestion group on the Song Tags setup page |
| **Vocal range mismatch/tip** | assigned singer's stored range doesn't cover a song's transposed range | hard "mismatch" badge, or softer "tip" if an octave shift fixes it |
| **Serving-frequency warning** | PCO `schedule_preference` (e.g. "Every other week") violated by real history | warning badge on the candidate row |
| **Cross-team warning** | person already filling another position/team the same week | warning badge |
| **Blockout-date warning** | live PCO blockout date covers the plan's date | warning badge, refreshable on-demand per person |
| **Debrief resurfacing** | same song, or same directional song pairing, recurs in a new draft | latest debrief/talking point resurfaces as a badge and prints on the Run Sheet |

---

## 5. Sync model: Publish / Discard / MergeSet

The central cause-effect chain of the whole app. Draft sets/rosters live
**only** in this app's Postgres until Publish pushes them to real PCO
plan items/records in one sync.

**PublishSet** - three ordered phases:
1. **Deletions first** - tombstoned rows (from remove/replace) get their
   PCO item deleted, fanned out concurrently, fully completing before
   phase 2 starts.
2. **Creates** - every song with no PCO item yet gets created (title,
   type, sequence, song/arrangement/key IDs, description, length,
   backfilled from the cache). Song leader synced. A `KeyOverride` that
   fails to resolve doesn't block the create - it's surfaced as a
   partial-failure message, never silently dropped. Already-published
   songs with a pending note/key edit get a plain update here too.
3. **Single reorder call** - PCO's own `item_reorder` action (per-item
   sequence PATCH is rejected outright) places every tracked song in
   local order, appended after untracked ("foreign") items which keep
   their own relative order. Title is set only at creation, never
   touched by reorder, so a title edited directly in PCO survives.

Never touches a PCO item this app didn't create itself; a failure on one
song doesn't stop the rest - the summary reports per-step counts and
failures; a failed reorder still shows "Discard changes" available for
retry rather than falsely claiming sync succeeded.

**RevertSet (Discard)** - PublishSet's mirror: does a fresh PCO read and
rebuilds the local set entirely from PCO's current state. Never calls a
PCO write endpoint - pure local reset.

**MergeSet ("Pull in from Planning Center")** - the third, non-destructive
direction. Pulls in PCO-side-only additions since the draft was last
touched, *without* discarding local changes (unpublished adds/removals/
reordering/pending edits - what Discard would wipe). Also reconciles
already-tracked items whose PCO description/key changed externally, and
clears the local reference for any tracked item deleted in PCO rather than
erroring. Flags if the tracked-item order itself drifted in PCO.

**SyncCheck (live-sync-reflection banner)** - polled every 30s while the
Songs tab is open. Compares a content fingerprint of live PCO items
against the one last rendered; unchanged is a no-op; changed renders a
dismissible banner offering to run MergeSet. Low-priority-tagged so it
never delays interactive traffic; a failed poll is treated as "unchanged,"
not an error.

**PublishRoster/RevertRoster** - same shape, simpler (no reorder step -
position assignments have no sequence).

**Plan status** (shown on the Build-page picker): `planned` (draft exists,
nothing unpublished), `pending` (draft has unpublished changes),
`todo` (no local draft, but PCO already has content), or empty (neither).

**Read-only past plans** - a plan whose PCO date has passed is
server-side locked: Publish/Discard/sync actions disappear, though
debriefs/talking points stay writable.

Framing (from `CLAUDE.md`): *"This is the highest-friction-bar code in the
app, not incidental plumbing... Any gap between what this app lets someone
set and what Publish actually pushes means the user has to go clean it up
by hand in PCO afterward, which defeats the entire premise of planning
here at all."*

---

## 6. Onboarding / setup flow

The Home dashboard's onboarding checklist tracks exactly 8 items, grouped
under the two vision promises (see §1) - this is the UI's own explicit
statement of the product's through-line:

**Protect the service:**
1. Song relationships - flows well, same theme, avoid pairing
2. Song roles - opener, response, or any role you add
3. Song contexts - big church, youth, another campus
4. Song tags - theme, scripture, idea tags for search/filter
5. Song vocal range - each song's sung range

**Protect the team:**
6. Song/person preferences - who's preferred/discouraged for a song
7. Person relationships - works well, mentorship, avoid pairing
8. Vocal range (person) - each vocalist's comfortable range

(Position preferences lives entirely in PCO's own data with nothing local
to check, so it's not one of the trackable 8, even though it has its own
setup page.)

Each item's Done/not-done state is a cheap local-DB existence check, not
a PCO call. The Home page shows an "N of 8" summary; `cmd/admin`'s
`/orgs` page shows the same 8-category completion across every customer
org.

---

## 7. Billing

Seat-based: a "seat" is a distinct admin who *builds* sets/rosters -
viewing/receiving a published schedule is always free. Every PCO org gets
an automatic 30-day free trial on first sign-in.

| Tier | Price | Included seats | Overage |
|---|---|---|---|
| Standard | $14/mo | 2 | none - non-blocking upsell nudge |
| Multi-Campus | $28/mo | 5 | $5/mo per extra seat, real billed overage |

All genuinely implemented, not just described: real Stripe Checkout,
real Stripe Billing Portal, a signature-verified webhook handling
`checkout.session.completed` / `customer.subscription.updated` /
`customer.subscription.deleted`, and a daily cron pushing actual seat
count into Stripe as the subscription quantity for Multi-Campus overage.
If Stripe env vars are unset, the trial/paywall gate still works - only
the checkout/portal buttons don't function.

Access gating is separate from billing: signing in requires the PCO
account to have *some* Services access (a coarse probe - can't
distinguish PCO's Viewer/Scheduler/Editor/Administrator levels).

---

## 8. Other notable features

- **Admin tooling** (`cmd/admin`) - exists because the main app has no
  "operator" role concept at all (every signed-in org member is a full
  equivalent admin within that org). Deliberately a separate binary
  rather than an `is_operator` flag bolted onto the main auth chain, to
  avoid introducing a role concept into a system designed not to have
  one. Gated by a shared `ADMIN_TOKEN` (HTTP Basic Auth) and bound to the
  operator's Tailscale IP in production - the tailnet is the real access
  control, the token is defense-in-depth. Live today: feedback (all
  orgs), org list with trial extension + setup completion, person
  directory. "Log in as an org" and further features are still open.
- **Background PCO sync jobs** - roughly hourly cron, for any org with
  someone currently signed in: Song Info cache, person-data cache,
  recent-history discovery. Reuses the same enqueue mechanics as an
  ordinary page render, tagged low-priority so it never delays
  interactive traffic. Other crons: session cleanup (24h), Stripe seat
  sync (daily), production DB backup to Cloudflare R2 (daily).
- **PCO rate-limit visibility** - a 429 surfaces as a calm page-level
  banner (in addition to always being logged), backed by an opt-in
  request throttle and opt-in 429 retry, both enabled in production
  after a real September 2026 incident.
- **Feedback submission** - authenticated (not anonymous), sends
  synchronously (bounded 10s timeout) so a real send failure surfaces to
  the submitter rather than silently showing success - changed after a
  stale Mailgun key failed silently in production. Always writes to the
  `feedback` table regardless of email success. No in-app read view in
  the main app (deliberately write-only there); `cmd/admin` is the only
  read path, across all orgs.

---

## Key file references

- Routes index: `internal/server/routes.go`
- Setup-page handlers: `internal/server/controllers/scheduling.go`
- Build page logic: `internal/server/controllers/sets_builder.go`, `rosters_builder.go`
- Publish/Discard/Merge: `internal/server/controllers/sets_publish.go` (`PublishSet`, `MergeSet`, `RevertSet`, `SyncCheck`), `rosters_publish.go`
- Warning algorithms: `internal/scheduling/sets.go` (`AvoidPairings`), `internal/scheduling/rosters.go` (`AvoidPairingConflicts`, works-well/mentorship), `internal/server/controllers/rosters_warnings.go` (frequency/blockout/cross-team)
- New-song/first-song/tag-suggestion mechanics: `internal/scheduling/new_songs.go`, `first_song_anchors.go`, `song_tag_suggestions.go`
- Onboarding checklist: `internal/server/controllers/home.go`, `internal/scheduling/admin_setup_completion.go`
- Vision doc: `docs/ideas.md` ("Vision / positioning")
- Billing: `internal/billing/stripe.go`, `webhook.go`
- Admin app: `cmd/admin/main.go`
- PWA install: `web/static/js/install-prompt.js`
- Live-sync design rationale: `docs/plans/shipped/live-sync-reflection-plan.md`
- Admin tooling design rationale: `docs/plans/in-progress/admin-tooling-plan.md`
