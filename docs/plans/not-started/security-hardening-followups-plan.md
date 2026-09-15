# Plan: security/prod-readiness follow-ups from the pre-launch audit

Not built yet - written up as a punch list to pick from later, not as
urgent enough to have blocked opening up to real users. Prompted by a
full local audit on 2026-09-14 ahead of the first public/small-launch
(Reddit) push: three parallel read-only passes (auth/session/cookies/
CSRF, injection/org-scoping/webhook-security, headers/error-handling/
exposure-surface) plus a direct `govulncheck` sweep.

## What the audit found clean (not re-litigated here)

Two of the three passes came back with **no findings at all** - worth
recording so this doesn't get re-audited from scratch next time: SQL
injection (every hand-written query parameterized, no user input ever
concatenated into query text), multi-tenant organization scoping (every
`scheduling.Store` method that touches org-owned data filters by
`organization_id`, spot-checked across `song_relationships.go`,
`person_relationships.go`, `sets.go`, `rosters.go`), IDOR on controller
`:id` routes (all pass `middleware.CurrentOrganizationID(c)`, which is
server-derived from the session, never client-supplied), Stripe webhook
signature verification (`internal/billing/webhook.go`'s `ConstructEvent`,
no bypass), XSS (templ's auto-escaping + `internal/notify`'s
`html.EscapeString` on feedback email fields), and mass-assignment (every
`Create*Params` type deliberately has no `OrganizationID` field for a
request to fill in - see e.g. `song_relationships.go`'s own doc comment).
CORS, `/style-guide`/`/pco/*` dev-route gating, Dockerfile non-root/
multi-stage hardening, and Fiber's default 4MB body limit also checked
out clean.

## What shipped same-day, not held for this list

Small enough to fix immediately rather than queue: 8 known
vulnerabilities across the Go stdlib and two dependencies (`go-chi/chi`,
`aws-sdk-go-v2`) via a toolchain pin (`go.mod`'s `toolchain go1.26.6`) and
dependency bumps - `govulncheck ./...` now reports zero; missing `Secure`
cookie flag on the session/oauth-state cookies (`internal/server/
controllers/auth.go`); a raw DB/driver error echoed straight to the
browser on a failed session-creation (`auth.go`, now logged server-side +
generic message); no baseline security headers at all (new
`internal/server/middleware/security_headers.go` -
`X-Content-Type-Options`/`X-Frame-Options`/`Referrer-Policy`, deliberately
no CSP - see that file's own doc comment for why); and no rate limiting on
the OAuth entry points or feedback submission (new `internal/server/
middleware/rate_limit.go`, applied to `/auth/pco/start`,
`/auth/pco/callback`, `POST /feedback`).

## Open items, by priority

### High

- **Done 2026-09-15** - see `internal/auth/token_encryption.go`.
  ~~PCO access/refresh tokens stored unencrypted at rest~~
  (`sessions.access_token`/`refresh_token`, migration `00014_sessions_
  pco_tokens.sql`, read/written by `internal/auth/sessions.go`). Encrypted
  at the app layer (AES-256-GCM) before every write, decrypted on every
  read; key comes from an optional `SESSION_TOKEN_ENCRYPTION_KEY` env var
  (base64-encoded 32 bytes, `openssl rand -base64 32`) - unset means
  encryption is skipped (plain text, as before) with a one-time startup
  WARN, matching this repo's Stripe/Mailgun/R2 "optional for local dev"
  convention. No migration/backfill script: a format-tagged ciphertext
  (`enc:v1:` prefix) lets legacy plaintext rows keep decrypting unchanged,
  so every session self-upgrades to encrypted storage on its own next PCO
  token refresh (automatic every ~2 hours).

### Medium

- **No error-monitoring/alerting service** (Sentry or similar). Today the
  only way to learn about a production error is `ssh debian && docker
  compose logs`, or someone hitting `/feedback` to report it manually.
  Fine for zero real users; worth reconsidering once this is publicly
  launched and Derek isn't the only person hitting it. `internal/logger`
  already structures every error with a `request_id` - wiring a hook
  into an external service would be additive, not a rework.
- **No real Content-Security-Policy.** Deliberately skipped in the
  same-day pass (see `security_headers.go`'s doc comment) because
  `internal/views/layout.templ`'s inline `onclick="..."` handlers calling
  into global-scope JS (documented, deliberate architecture - see
  CLAUDE.md's Architecture section on `web/static/js/`) are incompatible
  with a strict CSP, and a `script-src 'unsafe-inline'` policy buys little
  real protection for the effort of auditing every page against it. Worth
  a real pass if this app's JS architecture ever changes; not worth a
  half-measure now.

### Low / optional hardening

- **~150+ controller call sites return raw `err.Error()`** in the HTTP
  response body (this app's own established convention, per `recover.go`'s
  doc comment). Audience for nearly all of them is an already-signed-in
  org user, not the open internet, so this reads as a debugging aid more
  than a disclosure risk - not worth a mechanical sweep before a small
  launch. If ever revisited, a shared `renderError(err)` helper that
  strips raw SQL/driver error text specifically (`pq: ...`) would cover
  the worst case without touching every call site's PCO-facing messages.
- **`SignOutSession` (`internal/server/controllers/account.go`) has no
  explicit organization-ownership check** on the `:sessionID` path param -
  relies entirely on the id being an unguessable 256-bit hash
  (`hashToken`, `internal/auth/sessions.go`). Correct as-is, but adding
  `WHERE organization_id = $2` to `DeleteSessionByID` would be
  defense-in-depth worth the one-line cost next time that file's touched.
- **`GET /health`'s DB-down error path** (`internal/database/database.go`)
  may echo back connection-pool/driver detail (host/port, not
  credentials) when Postgres is unreachable. Only reachable when the DB is
  already down, and doesn't leak secrets even then - redact to a generic
  "down" string next time that file's touched, not worth a dedicated
  change.
- **No CSRF token** - relies solely on `SameSite=Lax` cookies. Confirmed
  no state-changing route is reachable via GET anywhere in the app, which
  is the one classic gap `SameSite=Lax` alone doesn't cover - so this is
  low-risk as-is. Only worth revisiting if this app ever needs to be
  embedded/called cross-site in a way that changes the `SameSite`
  calculus.

## Verification (once any of these are picked up)

`go build ./...`, `go vet ./...`, `make test`, `govulncheck ./...` - same
baseline this audit itself ran clean against.
