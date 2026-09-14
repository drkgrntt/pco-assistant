# Plan: mobile strategy (stay on PWA, don't build a JSON API yet)

Written 2026-09-08, following a research pass into the current API
surface and auth model. Not built, not started - this doc is
deliberately a **non-build decision with reasoning attached**, not a
spec to hand to a builder. Companion doc (raised in the same
conversation, separate decision): `docs/plans/not-started/
admin-tooling-plan.md`.

## The question

Derek's framing: at some point this app will likely need a mobile app,
htmx/templ don't seem like the right foundation for that, and PCO
Assistant's API endpoints would presumably need to grow JSON branches
(plus new view-only endpoints) to support it. This doc checks that
assumption against the actual codebase before treating it as a plan.

## What's actually there today

- **90 of ~97 routes render server-side templ/htmx HTML.** The one real
  JSON surface, `internal/server/controllers/scheduling_api.go`
  (`/api/scheduling`), covers exactly two narrow resources
  (song-relationships, song-preferences) - it was never built as a first
  slice of a general API, but it *is* proof the pattern works: its
  handlers call the same `internal/scheduling.Store` methods the htmx
  controller calls, just JSON-encoding the result instead of rendering a
  templ component.
- **Auth is cookie-only** (`church_session`, `HTTPOnly`, `SameSite:
  Lax`) - no bearer-token issuance, no refresh flow exposed to a client,
  PCO's own OAuth tokens never leave the server. `RequireAuth`
  (`internal/server/middleware/auth.go`) only ever reads a cookie.
- **A real, installable PWA already exists** - `web/static/
  manifest.json`, a registered service worker, a full custom install-
  prompt UX (`docs/plans/shipped/pwa-install-improvements-plan.md`,
  2026-09-07). The service worker's `fetch` handler is an intentional
  no-op - no offline support, on purpose, because the app's whole value
  is live PCO + local-DB data with nothing meaningful to serve offline.
- **No prior brainstorming exists on a native app anywhere in `docs/`.**
  The only adjacent thinking (`docs/ideas.md`'s "narrow/compact
  side-by-side layout" note) is about the PWA running *beside* PCO on a
  desktop/tablet, not a phone-first standalone app - "should this go
  native" was never actually decided, just assumed by momentum toward
  PWA.
- **Domain logic is unevenly decoupled from rendering.** Simple
  resources (song relationships/preferences, tags) already return plain
  domain structs from `internal/scheduling` with rendering added on top
  in the controller - clean, and already proven JSON-branchable. The
  complex pages (Build/Sets/Rosters) have their real logic - warning
  computation, page assembly - living in the `controllers` package
  against UI-shaped view-model structs (`internal/views/scheduling`),
  and every mutation handler rebuilds and re-renders the *entire* page
  rather than returning what changed. Not tangled with templ syntax
  (the view-model structs are plain serializable data, no `templ.
  Component` fields), just not shaped as an API contract, and
  bandwidth-expensive for anything but a full-fragment htmx swap.

## Decision

**Stay on PWA. Don't build a general JSON API preemptively.** Reasoning:

1. **Auth decides this more than UI framework does.** A webview-wrapped
   app (or plain "Add to Home Screen") inherits the existing cookie-based
   OAuth flow for free. A true native HTTP client does not - it needs a
   new token-issuance/refresh mechanism that doesn't exist today, which
   is real backend work independent of and larger than "add JSON
   responses." Nothing forces that work today.
2. **Nothing currently needs native-only capability.** No push
   notifications, no offline requirement, no app-store distribution
   goal has been named. If PWA already covers the actual use case
   (glanceable scheduling data, usable from a phone), native adds cost
   (new auth mechanism, app-store process, two codebases) without a
   matching benefit yet.
3. **Adding JSON branches to all 90 HTML routes now would be premature.**
   There's no consumer for most of them. The right incremental pattern is
   the one that already exists: add a JSON response only when a real
   consumer needs that specific resource, following `scheduling_api.go`'s
   precedent - not a wholesale content-negotiation pass across every
   controller.

## What would flip this decision

Revisit native (or a real general-purpose JSON API) if any of these
become true:

- **Push notifications** become a real want (e.g. "notify me when I'm
  scheduled" / "notify when a set changes") - PWA push exists on some
  platforms but is meaningfully weaker/less reliable on iOS Safari than a
  native push channel.
- **Offline capability** becomes a real want - conflicts with the
  service worker's current deliberate no-op design and would be a
  substantial rework regardless of native vs. PWA.
- **App-store presence** becomes a distribution/credibility goal in its
  own right (worship-team volunteers searching the App Store rather than
  being sent a link).
- A **third-party integration** or the admin tool
  (`docs/plans/in-progress/admin-tooling-plan.md`) needs to call this
  app's data from outside a browser session - that would be the first
  real forcing function for a token-based auth mechanism, and could double
  as the seed of a broader JSON API.

## Cheap insurance worth doing regardless

Independent of whether/when this decision flips: as Build/Sets/Rosters
code gets touched anyway, keep moving warning-computation and page-
assembly logic out of the `controllers` package and into
`internal/scheduling` as plain-DTO domain functions (the pattern
song-relationships already follows) rather than leaving it coupled to
`internal/views/scheduling`'s UI-shaped structs. This doesn't cost extra
now, and it's exactly what would make a future JSON branch (if one of the
triggers above fires) cheap instead of a rewrite.

## Non-goal

This doc does not commit to a timeline. "At some point" per Derek's
original framing stays exactly that - nothing here is scheduled work.
