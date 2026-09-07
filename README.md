# PCO Assistant

A self-hosted scheduling assistant for [Planning Center](https://www.planningcenteronline.com/)
Services, covering the parts PCO doesn't help with directly: relating songs
and people to each other, tracking scheduling preferences, and warning you
about conflicts and repeats *before* you publish a plan - not just recording
what you already decided.

Sign-in is "Sign in with Planning Center" (OAuth) - there's no separate user
system. Everything the app knows about songs, people, plans, teams, and
positions is read live from your PCO account; this app only stores the
domain data PCO has no concept of (relationships, roles, draft
sets/rosters) and syncs it back to real PCO records when you publish.
Every PCO organization gets a 30-day free trial automatically the first
time it signs in (a row in `organizations` - name, `status`, `plan`,
Stripe ids), enforced on every request by `RequireActiveOrganization`.
Once the trial ends (or any time before that), `/billing` offers the two
paid plans below via Stripe Checkout; an existing subscription is managed
through Stripe's own Billing Portal - no custom billing UI to build or
maintain beyond the plan picker itself.

Pricing is seat-based, where a "seat" is a distinct admin (whoever's
actually building sets/rosters here, not anyone just viewing or receiving
a published schedule - that's always free, no matter how many people that
is):

| Tier | Price | Included admin seats |
|---|---|---|
| Standard | $14/mo | 2 |
| Multi-Campus | $28/mo | 5 (+$5/mo per extra seat) |

Standard has no paid overage - going over 2 seats just shows a
non-blocking upsell nudge on `/billing`. Multi-Campus bills overage for
real: a daily cron (`jobs.BillingJobs`) pushes each org's actual seat
count into Stripe as that subscription's line-item quantity.

Signing in also requires the PCO account to have some access to the
Services product in Planning Center - a login with zero Services access
is blocked at sign-in with a message asking them to have their church's
Planning Center admin grant it. This check is coarse: PCO's API exposes
no clean permission-level field, so it can only tell "has some access" from
"has none" (a live probe call that succeeds vs. 401/403s) - it can't tell
PCO's own Viewer/Scheduler/Editor/Administrator levels apart, so anyone
with at least Viewer access can still sign in.

## Features

**Songs**
- **Song Relationships** - tag songs as `flows_well`, `same_theme`, or
  `avoid_pairing` with each other, with notes
- **Song Roles** - mark songs with a role (opener, response, communion,
  prayer, reflection, baptism, or any other role you add to the lookup
  table - no code change needed) - shown as badges on the song and
  filterable while building a set
- **Song Tag Suggestions** - a non-AI suggestion tool that groups songs
  sharing a significant word in their titles (e.g. "King of Kings,"
  "Christ the King," and "All Hail King Jesus" all sharing "king"), so you
  can review a group and tag them all at once instead of tagging every
  song by hand
- **Song Info** - each song's Planning Center arrangement (key, tempo,
  meter, length) is cached locally so showing it doesn't mean a live
  Planning Center request per song on every page load - a song gets
  cached automatically the first time the Build page's set builder
  actually needs it, with no button or page to remember to visit. A
  song's detail card (click its row on the Build page) also shows its
  section-by-section
  structure ("Intro, Verse 1, Chorus...") and a "Chord chart" badge when
  one's attached in Planning Center - both pulled from the same cached
  arrangement data, no extra Planning Center request
- **Song attachments (lead sheets/chord charts)** - a song's detail card
  also lists its Planning Center Attachments (lead sheet/chord chart PDFs,
  scans, audio - any file a church has uploaded to it) as a small "Files"
  list, one link per attachment, opening the real file in a new tab.
  Cached alongside the rest of Song Info at sync time; clicking a link
  live-fetches a fresh URL from Planning Center first, rather than linking
  to a possibly-stale one, in case it expires. This is a manual reference
  aid, not automatic vocal-range extraction - Planning Center's attachments
  are arbitrary file blobs (often scanned PDFs), not structured notation,
  so there's nothing to compute a range from automatically
- **Song Vocal Range** - record each song's lowest and highest sung note,
  measured against the key you played it in, on a dedicated setup page
  (mirroring Song Tags/Song Roles - a fact about the song itself, reused
  across every plan it appears in, not a per-occurrence value). The Build
  page's song detail card shows the transposed result live - "Vocal
  range: G3-D5 (in this key)" - re-derived from the stored interval into
  whatever key the arrangement is actually in right now, whether or not
  that matches the key it was originally measured in. Pairs with the
  People section's **Vocal Range** above; the mismatch warning comparing
  a song's range against a roster'd vocalist's is still on the roadmap
  (see `docs/plans/in-progress/vocal-range-plan.md`)
- **Song / Person Preferences** - note who's preferred or discouraged for a
  given song
- **Song notes** - click a song's row on the Build page to open its detail
  card: "This week's note" is a local per-occurrence note ("cut the bridge
  this week") that reaches Planning Center as that item's Description on
  the next Publish/Refresh; once a song is published, its item also gets
  one box per Planning Center note category configured for the service
  type (e.g. "Audio/Visual," "Band," "Vocals"), each saving straight to
  Planning Center immediately - the same notes a team member sees when
  looking at that song in Planning Center itself
- **Per-occurrence key** - also on the song detail card: a key for this
  one week only, independent of the song's arrangement and its own
  default key - reaches Planning Center as a Key relationship on the
  song's plan item on the next Publish/Refresh, resolving to an existing
  Key under that arrangement or creating one if it doesn't exist yet.
  Tempo and time signature (BPM/meter) aren't offered here - Planning
  Center only models those on the arrangement itself, with no
  per-occurrence override at all
- **Song leader (per-occurrence)** - also on the song detail card: who's
  actually leading this song this week, picked from your Planning Center
  people (not typed as free text, so it can't drift from what Planning
  Center itself knows about that person). Once you designate which of a
  service type's Item Note categories is "the vocal one" (a one-time
  per-service-type setting, since categories are named freely per org -
  set on the plan picker page once a service type is chosen, not on any
  one plan's own builder), the leader syncs in as a "Leading: [name]"
  line at the top of that category's note on the next Publish/Refresh -
  added, replaced, or removed as the leader changes, without touching
  anything else a team member typed in that same box by hand
- **Debrief log / talking points (per-song, persistent)** - also on the
  song detail card, but unlike "This week's note" above, these aren't tied
  to one occurrence and never touch Planning Center: a running log of
  short notes about a song, or a specific song-to-song transition, that
  carries forward across plans. Two kinds - a **debrief** ("the bridge ran
  long," written after a Sunday) and a **talking point** ("say this right
  before the key change," written ahead of one) - each entered against
  just a song, or a directional pairing (into/out of another specific
  song, picked from a plain song list). The latest one resurfaces
  automatically, as a click-to-expand badge (not a warning), the next time
  that same song or that exact pairing back-to-back comes up in a new
  draft; the latest talking point per song/pair also prints on the Run
  Sheet. Writable even on a past, read-only plan, since a debrief is
  naturally written about a plan that's already happened
- **Build a Service (Songs tab)** - pick an upcoming plan and build its
  song lineup against the plan's *full* order of service (including
  template headers and non-song items already in Planning Center, not just
  a blank slate). Drag-and-drop reordering, recommendations based on song
  relationships, avoid-pairing warnings, duplicate-song support (e.g. a
  song reprised as the response), a repeated-song warning if a song was
  sung in each of the last two plans, a plan-length warning when the draft
  runs noticeably longer/shorter than that service type's recent average,
  a key-change warning when two back-to-back songs (nothing else between
  them) are a jarring half step apart, and each song's key/tempo/meter/length
  shown right on the row (see Song Info above). The opposite nudge also runs
  automatically: a song genuinely new to the church (Planning Center has no
  prior scheduling date for it the first time it's seen) gets a "Sung
  once"/"Sung twice" badge and a spot in a "Could use one more sing" section
  once it's been published once or twice, so a new song gets the repetition
  it needs to stick before it's forgotten. The mirror-image nudge runs too:
  a song that HAS been sung before but not in the last 6 months gets a
  "Not sung recently" badge, a soft prompt to consider archiving it rather
  than an automatic suggestion - a seasonal song (Christmas, Easter, etc.,
  tagged with the "Seasonal" tag category) is exempted, since its long gap
  is by design. A new plan's first song lands
  wherever the first song usually goes for that service type once you've
  placed it there once (tracked separately per service type, so e.g.
  Sunday and Wednesday can differ). Add a song right after another one and
  you're offered a one-click prompt to record how they relate (flows well,
  same theme, etc.) without leaving the page - ad hoc song tags/
  relationships/roles can also be created from a "Quick add" panel right on
  the build page, not just the dedicated setup pages, or from each song
  row's own "more actions" menu (tag it, relate it - defaulting to whichever
  song comes next in the order - or mark a role, all already scoped to that
  song, no picking it out of a list again). The "Browse all songs" search
  box already matches typed text against tag values as well as song
  titles; before typing anything, that same box shows a plain, scrollable
  list of every song's title to skim - clicking one adds that song
  straight to the set. Below the search box, "Browse by tag" chips (one
  collapsible section per tag category) list every distinct tag value
  actually in use, so you can find a tag to search for without already
  knowing what's been tagged - clicking a chip fills in the search box.
  Publish pushes everything to real PCO plan items in one sync; Discard
  reverts your local draft back to whatever's live in PCO; "Pull in from
  Planning Center" sits between the
  two - it adds anything added directly in Planning Center since the draft
  was last touched, without discarding any local change (unpublished adds,
  removals, reordering, pending note/key edits) the way Discard would.

**People**
- **Person Relationships** - tag people as `works_well`, `mentorship`
  (directional - who mentors whom), or `avoid_pairing`, with notes
- **Position Preferences** - set how often someone wants to serve a
  position, backed by Planning Center's own
  `PersonTeamPositionAssignment.schedule_preference` field ("Every other
  week," "Once a month," etc.) - this writes back to real PCO data, so it's
  respected by Planning Center's own scheduling tools too, not just here
- **Vocal Range** - record each vocalist's lowest and highest comfortable
  note (e.g. "G3" to "D5") on a dedicated setup page, so "can they hit this
  note" is answered from a lookup instead of guessed at in rehearsal. Each
  note has a small tap-to-hear button next to it that plays a synthesized
  reference tone (Web Audio, standard A440 tuning) at that pitch, so a
  director unsure what a stored note actually sounds like doesn't have to
  work it out by ear or on an instrument. Pairs with **Song Vocal Range**
  below (Songs section) - the mismatch warning that compares the two is
  still on the roadmap (see `docs/plans/in-progress/vocal-range-plan.md`)
- **Build a Service (Team tab)** - the same page as the Songs tab above,
  filling an upcoming plan's open positions from each position's real PCO
  eligibility list, with avoid-pairing conflict warnings, works-well/
  mentorship suggestions, serving-frequency warnings (checked against real
  history), a cross-team warning when someone's already filling another
  position/team the same week, and blockout-date warnings (read live from
  Planning Center).
  Add someone right after someone else already on the roster and you're
  offered the same one-click "remember how they relate" prompt as the
  Songs tab; ad hoc person relationships and position eligibility can also
  be set from a "Quick add" panel here. Publish/Discard mirror the Songs
  tab's sync model.

Songs and Team stay on separate tabs (each directly linkable via `?tab=`),
but a sidebar keeps both tabs' Publish/Discard controls available no matter
which one is open, plus an at-a-glance card for whichever tab *isn't* open -
the other tab's actual song order or who's filling each position, not just
a count.

**Past plans & notes** - the Build page's plan picker has an Upcoming/Past
tab: since every set/roster ever drafted stays in this app's own database
indefinitely (nothing deletes it after Publish), a plan that's already
happened is still browsable here, just read-only (no Publish/Discard/
PCO-sync actions) once opened. Every plan in the list, past or upcoming,
can also carry a small local-only note ("the one with the guest speaker")
editable right in the list - purely for finding a specific service at a
glance, matched by the picker's own search box alongside title and date.

**Run sheet** - a one-click printable page (a Print button using the
browser's own print-to-PDF, no separate PDF library) combining the full
order of service, each song's key/tempo/length and item notes, any
talking point on file for that song or the transition into it, and the
roster grouped by team/position, flagging any still-short-handed spots -
the artifact a worship leader would otherwise assemble by hand every
week. Linked from the Build page next to "Open in Planning Center."

**Account** - `/account` consolidates account/organization identity,
billing (the same plan picker/Stripe portal `/billing` itself renders),
and a "Data & Caches" section with two cache-clear actions (Song Info's
key/tempo/meter/length cache, and the person-data cache behind roster
scheduling warnings). Explicitly non-destructive: both only delete this
app's own locally-cached copy of Planning Center data, which gets
transparently re-collected from Planning Center the next time it's
needed - nothing in Planning Center, and no draft set/roster, is touched.
Both caches also top themselves up in the background roughly once an
hour for any organization with someone currently signed in, so a church
that doesn't open this app every day still finds its Song Info and
roster warnings already warm next time someone does, rather than only
ever catching up the moment a page is opened.
It also lists everyone currently signed in, with a per-person Sign out
button, so a departed staff member's billed seat can be freed right away
instead of waiting out the 30-day session expiry (see the Pricing section
above) - any signed-in person can sign any other out, since every seat is
already, by this product's own definition, a full admin. An "Install this
app" section is the persistent, findable home for adding Resonance to a
home screen or dock, whichever install path applies (see below).

Mobile-first, installable as a PWA, with light/dark theming. Beyond the
browser's own passive install affordance, a custom in-app install flow
(`web/static/js/install-prompt.js`) captures Chrome/Edge's
`beforeinstallprompt` event and drives the prompt from this app's own
button instead - shown both on `/account` (always available once the
browser makes installing possible at all) and as a dismissible banner that
starts appearing after a few days of use rather than on a visitor's first
visit. iOS Safari has no such event (Apple has never shipped one), so it
gets user-agent-gated instructional copy instead ("tap the Share icon,
then Add to Home Screen") in the same two spots. A dismissed banner stays
dismissed for two weeks before re-appearing, entirely via `localStorage` -
no server-side state involved.

## Tech stack

- Go + [Fiber](https://gofiber.io/)
- [templ](https://templ.guide/) views, rendered server-side and swapped via
  [htmx](https://htmx.org/) - no client-side framework
- PostgreSQL, migrated with [goose](https://github.com/pressly/goose)
  (embedded migrations, run automatically on startup)
- [`go-pco`](https://github.com/drkgrntt/go-pco) - a Planning Center API SDK,
  its own separate repo/module, pulled in as a normal tagged dependency
- A small in-house CSS design system - `web/static/css/tokens.css` (colors,
  spacing, type scale, radius, shadow, motion), `base.css` (element
  defaults), `components/` (one file per reusable component/utility group -
  buttons, badges, alerts, forms, tables, cards, list rows, disclosure
  menus, tabs, ...), and a handful of page-specific files (`header-nav.css`,
  `build-page.css`, ...) for everything that's only ever used in one place.
  Structured patterns (cards, dismissible banners, sortable-table chrome,
  list rows, disclosure menus, tabs) are backed by real templ components so
  every page gets the correct markup/ARIA/JS wiring automatically, not just
  the right class name. See it rendered live at `/style-guide` (only
  responds when `APP_ENV=local`).

## Setup

1. **Postgres.** Create a database and set the `DB_*` variables below to
   point at it - migrations run automatically on startup.

2. **Planning Center credentials.** You need two separate things from PCO:
   - A [Personal Access Token](https://api.planningcenteronline.com/oauth/applications)
     (`PCO_CLIENT_ID` / `PCO_SECRET`) - used for every server-side API call
   - An [OAuth application](https://api.planningcenteronline.com/oauth/applications)
     (`PCO_OAUTH_CLIENT_ID` / `PCO_OAUTH_CLIENT_SECRET`) with redirect URI
     `http://localhost:8080/auth/pco/callback` - used for "Sign in with
     Planning Center"

3. **Stripe (optional for local dev).** Every organization gets a free
   trial automatically at sign-in - Stripe is only needed for the
   `/billing` page's "choose a plan"/"manage billing" buttons to
   actually work. Create two Products/Prices (Standard $14/mo,
   Multi-Campus $28/mo with graduated per-seat overage - see the
   pricing table above) in a
   [test-mode Stripe account](https://dashboard.stripe.com/test/apikeys),
   and a webhook endpoint pointed at `<APP_BASE_URL>/billing/webhook`
   listening for `checkout.session.completed`,
   `customer.subscription.updated`, and `customer.subscription.deleted`.
   Leave the `STRIPE_*` variables below blank to skip this - the
   trial/paywall gate itself still works, only checkout/portal don't.

4. **Mailgun (optional for local dev).** The `/feedback` page emails
   submissions through [Mailgun](https://www.mailgun.com/) - a domain
   and API key (`MG_DOMAIN`/`MG_KEY`) plus the address you want feedback
   sent to (`FEEDBACK_RECIPIENT_EMAIL`). Leave `MG_DOMAIN`/`MG_KEY` blank
   to skip this - feedback submissions are just logged instead of
   emailed, so local dev never needs real Mailgun credentials.

5. **Backups (production only, optional for local dev).** A daily cron
   (`jobs.BackupJobs`) dumps the database with `pg_dump` and uploads it to
   a [Cloudflare R2](https://developers.cloudflare.com/r2/) bucket
   (`R2_ACCOUNT_ID`/`R2_ACCESS_KEY_ID`/`R2_SECRET_ACCESS_KEY`), pruning
   older backups down to one per week past 30 days and one per month past
   6 months. The job no-ops unless `APP_ENV=production`, so it's inert in
   local dev and staging regardless of whether these are set. Requires
   `pg_dump` on `PATH` (already in the Docker image via
   `postgresql-client`).

6. **PCO request throttle (enabled in production).** go-pco's opt-in,
   priority-aware request throttle (see its README) can be turned on
   purely via env var, no code change - `PCO_THROTTLE_ENABLED=true`, plus
   optional `PCO_THROTTLE_RATE_PER_20S` (defaults to 60, i.e. 60% of PCO's
   real 100-req/20s ceiling) and `PCO_THROTTLE_DEFAULT_PRIORITY` (defaults
   to 10). This app already tags its two background syncs (Song Info,
   person-data cache) at priority 20 so they never delay interactive
   traffic once the throttle is enabled - see `docs/pco-api-risks.md`. Set
   `PCO_THROTTLE_ENABLED=true` to match production (enabled 2026-09-03
   after a real 429 - see `docs/pco-api-risks.md`'s throttle addendum);
   leave unset to skip this in local dev, since a single developer's own
   traffic is in no danger of hitting PCO's ceiling.

7. **PCO 429 retry (enabled in production).** go-pco v0.19.0+'s opt-in
   retry (see its README) is this app's other line of defense against a
   429 - the throttle above bounds *sustained* rate but not every
   possible burst pattern, so a caller can still get a real 429 even with
   it on. `PCO_RETRY_ON_429_ENABLED=true`, plus optional
   `PCO_RETRY_ON_429_MAX_ATTEMPTS` (defaults to 2, i.e. one retry) and
   `PCO_RETRY_ON_429_MAX_WAIT_SECONDS` (defaults to 30) - a 429 waits for
   PCO's own `Retry-After` header (capped at `MAX_WAIT_SECONDS`) and tries
   once more before giving up. This app also registers a
   `pco.SetResponseHook` (`internal/server/pco_observability.go`) that
   logs any PCO failure - a 429 specifically with its attempt count and
   throttle-wait time - through the app's own logger, since go-pco never
   logs anything itself; this is unconditional (not gated by either env
   var above) so a 429 leaves a trace in the logs whether or not the
   throttle/retry are on. See `docs/pco-api-risks.md`'s throttle addendum
   for the incident that prompted both of these.

8. **`.env`**:
   ```
   PORT=8080
   APP_ENV=local

   DB_HOST=localhost
   DB_PORT=5432
   DB_DATABASE=...
   DB_USERNAME=...
   DB_PASSWORD=...
   DB_SCHEMA=public

   PCO_CLIENT_ID=...
   PCO_SECRET=...

   PCO_OAUTH_CLIENT_ID=...
   PCO_OAUTH_CLIENT_SECRET=...
   PCO_OAUTH_REDIRECT_URI=http://localhost:8080/auth/pco/callback

   APP_BASE_URL=http://localhost:8080

   STRIPE_SECRET_KEY=...
   STRIPE_PRICE_STANDARD=...
   STRIPE_PRICE_MULTI_CAMPUS=...
   STRIPE_WEBHOOK_SECRET=...

   MG_DOMAIN=...
   MG_KEY=...
   FEEDBACK_RECIPIENT_EMAIL=...

   R2_ACCOUNT_ID=...
   R2_ACCESS_KEY_ID=...
   R2_SECRET_ACCESS_KEY=...

   PCO_THROTTLE_ENABLED=...
   PCO_THROTTLE_RATE_PER_20S=...
   PCO_THROTTLE_DEFAULT_PRIORITY=...

   PCO_RETRY_ON_429_ENABLED=...
   PCO_RETRY_ON_429_MAX_ATTEMPTS=...
   PCO_RETRY_ON_429_MAX_WAIT_SECONDS=...
   ```

## Makefile

Run build make command with tests
```bash
make all
```

Build the application
```bash
make build
```

Run the application
```bash
make run
```

Live reload the application:
```bash
make watch
```

Run the test suite:
```bash
make test
```

Clean up binary from the last build:
```bash
make clean
```

Wipe the local database and rebuild it from migrations (asks for
confirmation - pass `ARGS=-yes` to skip it):
```bash
make db-reset
```

Populate the local database with example scheduling data - every lookup
table (song tag categories, song/person relationship types, song roles,
song preference types) is read live and seeded with several examples each,
so a type added to one of those tables later needs no dbseed change to get
picked up. Safe to run more than once: tags/roles/preferences upsert in
place every run (harmless, not duplicates), and relationships correctly
report "already seeded" instead of creating a second row:
```bash
make db-seed
```

Both refuse to run unless `APP_ENV=local`, and can also be run directly with
`go run ./cmd/dbreset` / `go run ./cmd/dbseed` (each accepts `-h` for its
flags). Seeded rows reference songs/people by ID, and songs/people
themselves live in Planning Center, not this database - if you've signed
into the app in your browser at least once, `db-seed` actually creates a
fixed set of example songs in Planning Center (skipping any that already
exist by title, so a repeat run doesn't duplicate the library), each with
its arrangement set to a real key/tempo the same way Planning Center's own
"add song" flow leads you through by hand. People are read (not created)
from whichever real people your org already has, padded with placeholder
IDs if there aren't enough for full variety. With no usable session, both
songs and people fall back to placeholder IDs entirely - still enough to
exercise every page, just without a resolvable title/name. Note `db-reset`
wipes the `sessions` table along with everything else, so sign in again at
http://localhost:8080/login before re-running `db-seed` if you want it to
seed against real Planning Center data rather than placeholders.

Permanently delete every Song and Plan in the signed-in Planning Center
organization (asks for confirmation - pass `ARGS=-yes` to skip it). Unlike
`db-reset`/`db-seed`, this reaches out to the live PCO account itself, not
just this app's local database - only ever point it at a disposable
dev/test PCO org, never a real church's account. Local rows that reference
what it deletes are left behind (now pointing at IDs that no longer exist),
so pair it with `db-reset` for a full reseed cycle:
```bash
make pco-reset
```
