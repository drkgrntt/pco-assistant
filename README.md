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
Every PCO organization that signs in gets its own row in `organizations`
(name plus a `status`, `'active'` by default) - the one local place
account-level state lives, so a future subscription/billing flow has
somewhere to write to without a schema change.

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
  cached automatically the first time it's added to a set, or sync your
  whole library at once from the Song Info page
- **Song / Person Preferences** - note who's preferred or discouraged for a
  given song
- **Build a Service (Songs tab)** - pick an upcoming plan and build its
  song lineup against the plan's *full* order of service (including
  template headers and non-song items already in Planning Center, not just
  a blank slate). Drag-and-drop reordering, recommendations based on song
  relationships, avoid-pairing warnings, duplicate-song support (e.g. a
  song reprised as the response), a repeated-song warning if a song was
  sung in each of the last two plans, and each song's key/tempo/meter/length
  shown right on the row (see Song Info above). The opposite nudge also runs
  automatically: a song genuinely new to the church (Planning Center has no
  prior scheduling date for it the first time it's seen) gets a "Sung
  once"/"Sung twice" badge and a spot in a "Could use one more sing" section
  once it's been published once or twice, so a new song gets the repetition
  it needs to stick before it's forgotten. A new plan's first song lands
  wherever the first song usually goes for that service type once you've
  placed it there once (tracked separately per service type, so e.g.
  Sunday and Wednesday can differ). Add a song right after another one and
  you're offered a one-click prompt to record how they relate (flows well,
  same theme, etc.) without leaving the page - ad hoc song tags/
  relationships/roles can also be created from a "Quick add" panel right on
  the build page, not just the dedicated setup pages. Publish pushes
  everything to real PCO plan items in one sync; Discard reverts your local
  draft back to whatever's live in PCO.

**People**
- **Person Relationships** - tag people as `works_well`, `mentorship`
  (directional - who mentors whom), or `avoid_pairing`, with notes
- **Position Preferences** - set how often someone wants to serve a
  position, backed by Planning Center's own
  `PersonTeamPositionAssignment.schedule_preference` field ("Every other
  week," "Once a month," etc.) - this writes back to real PCO data, so it's
  respected by Planning Center's own scheduling tools too, not just here
- **Build a Service (Team tab)** - the same page as the Songs tab above,
  filling an upcoming plan's open positions from each position's real PCO
  eligibility list, with avoid-pairing conflict warnings, works-well/
  mentorship suggestions, serving-frequency warnings (checked against real
  history), and blockout-date warnings (read live from Planning Center).
  Add someone right after someone else already on the roster and you're
  offered the same one-click "remember how they relate" prompt as the
  Songs tab; ad hoc person relationships and position eligibility can also
  be set from a "Quick add" panel here. Publish/Discard mirror the Songs
  tab's sync model.

Mobile-first, installable as a PWA, with light/dark theming.

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
  defaults), `components.css` (buttons, badges, alerts, forms, tables,
  cards, list rows), and `app.css` for everything page-specific. See it
  rendered live at `/style-guide` (only responds when `APP_ENV=local`).

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

3. **`.env`**:
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
