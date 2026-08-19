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

## Features

**Songs**
- **Song Relationships** - tag songs as `flows_well`, `same_theme`, or
  `avoid_pairing` with each other, with notes
- **Song Roles** - mark songs as good openers or response songs
- **Song / Person Preferences** - note who's preferred or discouraged for a
  given song
- **Build a Set** - pick an upcoming plan and build its song lineup against
  the plan's *full* order of service (including template headers and
  non-song items already in Planning Center, not just a blank slate).
  Drag-and-drop reordering, recommendations based on song relationships,
  avoid-pairing warnings, duplicate-song support (e.g. a song reprised as
  the response), and a repeated-song warning if a song was sung in each of
  the last two plans. Publish pushes everything to real PCO plan items in
  one sync; Discard reverts your local draft back to whatever's live in PCO.

**People**
- **Person Relationships** - tag people as `works_well`, `mentorship`
  (directional - who mentors whom), or `avoid_pairing`, with notes
- **Position Preferences** - set how often someone wants to serve a
  position, backed by Planning Center's own
  `PersonTeamPositionAssignment.schedule_preference` field ("Every other
  week," "Once a month," etc.) - this writes back to real PCO data, so it's
  respected by Planning Center's own scheduling tools too, not just here
- **Build a Team** - fill an upcoming plan's open positions from each
  position's real PCO eligibility list, with avoid-pairing conflict
  warnings, works-well/mentorship suggestions, serving-frequency warnings
  (checked against real history), and blockout-date warnings (read live
  from Planning Center). Publish/Discard mirror Build a Set's sync model.

Mobile-first, installable as a PWA, with light/dark theming.

## Tech stack

- Go + [Fiber](https://gofiber.io/)
- [templ](https://templ.guide/) views, rendered server-side and swapped via
  [htmx](https://htmx.org/) - no client-side framework
- PostgreSQL, migrated with [goose](https://github.com/pressly/goose)
  (embedded migrations, run automatically on startup)
- [`go-pco`](https://github.com/drkgrntt/go-pco) - a local Planning Center
  API SDK, pulled in via a `replace` directive against a sibling checkout
  (see Setup below)

## Setup

1. **Clone `go-pco` as a sibling directory.** `go.mod` points at it via
   `replace github.com/drkgrntt/go-pco => ../go-pco`, so it needs to live
   next to this repo, e.g.:
   ```
   projects/
     pco-assistant/   (this repo)
     go-pco/
   ```
   If you use `air` for live reload, note that it only watches this repo -
   editing files in `../go-pco` needs a touch inside this repo to trigger a
   rebuild.

2. **Postgres.** Create a database and set the `DB_*` variables below to
   point at it - migrations run automatically on startup.

3. **Planning Center credentials.** You need two separate things from PCO:
   - A [Personal Access Token](https://api.planningcenteronline.com/oauth/applications)
     (`PCO_CLIENT_ID` / `PCO_SECRET`) - used for every server-side API call
   - An [OAuth application](https://api.planningcenteronline.com/oauth/applications)
     (`PCO_OAUTH_CLIENT_ID` / `PCO_OAUTH_CLIENT_SECRET`) with redirect URI
     `http://localhost:8080/auth/pco/callback` - used for "Sign in with
     Planning Center"

4. **`.env`**:
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
