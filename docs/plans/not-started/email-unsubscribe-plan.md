# Plan: honored unsubscribe link for outbound-to-a-real-user email

Not started. Written 2026-09-16, prompted directly by
`docs/plans/not-started/welcome-email-plan.md` (also not started): that
plan is this app's first email ever sent to a real org person rather than
just to Derek (`SendFeedback` only ever goes to the app operator's own
configured `recipientEmail` - there's no one for *it* to unsubscribe).
The moment that changes, every such send needs a working, honored opt-out
- not just as email-marketing best practice but because CAN-SPAM requires
it for any message with commercial content, and `welcomeHTML` is written
to include exactly that (a one-line protect-team/protect-service pitch,
per that plan's "Decisions made" #2). This plan is a prerequisite for
welcome-email-plan.md, not an alternative to it - see that plan's own
`internal/notify` changes section, which needs the signature updates
below before `SendWelcome` can ship.

## What "honored" means here

Not just a link in the email footer that happens to work if clicked -
structurally impossible for a future `notify` call site to forget the
check. That means suppression lives inside `notify.Client`'s send path
itself, not as a step every caller has to remember to run first (compare
`sendTimeout`/`hasCredentials()` - already enforced centrally in
`Client`, not left to each caller).

## Storage

New migration `internal/database/migrations/00049_people_unsubscribed_at.sql`:
a nullable `unsubscribed_at timestamptz` column on `people`. Scoped to the
existing `(organization_id, person_id)` compound key - not a global
by-email-address block - matching CLAUDE.md's Organization scoping
section and this table's own existing shape: someone opting out is
opting out of *this org's* Resonance emails, not declaring anything about
a different org they might also belong to under the same PCO account.
`NULL` = still subscribed, a timestamp = when they opted out. One-way for
now (no resubscribe flow) - nothing in this app's UI lets someone manage
their own email preferences yet, and CAN-SPAM doesn't require offering
resubscribe, so this isn't a gap worth closing preemptively.

`internal/auth` (`people.go`) gains:

```go
// IsUnsubscribed reports whether organizationID/personID has opted out of
// this org's emails. Any error (including no row - shouldn't happen,
// since every email-sending call site only ever fires after UpsertPerson
// has already run, but this isn't a case worth trusting blindly) is the
// caller's signal to skip the send, not to assume subscribed - see
// notify.Client's SuppressionChecker usage.
func (s *Store) IsUnsubscribed(ctx context.Context, organizationID, personID string) (bool, error)

// SetUnsubscribed marks organizationID/personID opted out, idempotently
// (a second call is a no-op, not an error) - called from the unsubscribe
// controller's POST handler.
func (s *Store) SetUnsubscribed(ctx context.Context, organizationID, personID string) error
```

## Link/token scheme

The recipient clicking a link in an email has no session cookie for this
app at all (may never have signed in from that browser/device) - the link
itself has to carry enough to identify who's unsubscribing, signed so it
can't be forged or tampered with (a guessable org/person id pair
must not be enough on its own to unsubscribe someone else - PCO ids
aren't secret).

New file `internal/notify/unsubscribe_token.go`:

- HMAC-SHA256 over `organizationID + "|" + personID`, keyed by a process-
  wide secret, base64url-encoded for the URL. Signing (not the
  AES-256-GCM *encryption* `token_encryption.go` uses for PCO tokens) is
  the right primitive here - org/person ids aren't confidential, they
  just need tamper-evidence, so there's nothing to decrypt back out.
- `(c *Client) UnsubscribeURL(organizationID, personID string) string` -
  builds `{appBaseURL}/unsubscribe?org=...&person=...&sig=...`.
- `(c *Client) VerifyUnsubscribeToken(organizationID, personID, sig
  string) bool` - recomputes the HMAC and compares via `hmac.Equal`
  (constant-time - this is a public-facing verification endpoint).
- No expiry on the token. Real ESPs' List-Unsubscribe links don't expire
  either, and the only action a valid token ever authorizes is "opt this
  person out of email," which is safe to honor indefinitely - unlike a
  session or password-reset token, there's no elevated-access risk to
  bound with a TTL.

### Secret key: `EMAIL_LINK_SECRET`

New env var, base64-standard 32-byte key, loaded once via
`notify.LoadLinkSecretFromEnv()` (mirrors
`auth.LoadTokenEncryptionKeyFromEnv`'s shape - fails hard, log +
`os.Exit(1)`, if set but malformed).

Deliberately **not** the same "unset -> safe no-op" fallback
`token_encryption.go` uses. That no-op works there because a missing key
just means encryption-at-rest is off, degrading to today's existing
plaintext-storage baseline - not introducing a new hole. Here, an
unkeyed/predictable HMAC would mean anyone who can see an org/person id
(both appear in ordinary app URLs already) could forge a valid
unsubscribe link for someone else. So unset instead falls back to a
random key generated once via `crypto/rand` at process startup (still
unforgeable), with a one-time WARN log same as the token-encryption
loader's unset case - but call out explicitly in that log message (and
here) that this fallback is **local-dev-only safe**: every already-sent
unsubscribe link stops verifying the moment the process restarts, and
this app redeploys routinely in production (`docker compose build && up
-d` per CLAUDE.md's Deployment section), so a real deployment leaving
this unset would silently break every previously-sent link on the next
deploy. Treat this the same as `SESSION_TOKEN_ENCRYPTION_KEY` being
*recommended* in dev vs. required in prod, but enforce the prod half of
that in code rather than only in a doc: `notify.Client` refuses to
actually send anything to a real recipient (as opposed to the
operator-facing `SendFeedback`) when running with the random ephemeral
fallback - see "Gating real sends" below.

## `notify.Client` changes

- `New(...)` gains two new params: `appBaseURL string` (already exists
  elsewhere as `APP_BASE_URL`, see `internal/views/version.go`'s
  `MarketingBaseURL` and `billing.New`'s identical param - `server.go`
  passes `os.Getenv("APP_BASE_URL")` again here, not a new env var) and
  `linkSecret *[32]byte` (or equivalent, from
  `LoadLinkSecretFromEnv()`), plus a `SuppressionChecker`:

  ```go
  // SuppressionChecker reports whether organizationID/personID has opted
  // out of email. auth.Store satisfies this directly (IsUnsubscribed
  // matches this signature already) - defined here, not imported from
  // internal/auth, so notify doesn't need to depend on auth's package
  // just for one method signature (same reasoning as Go's io.Reader).
  type SuppressionChecker interface {
      IsUnsubscribed(ctx context.Context, organizationID, personID string) (bool, error)
  }
  ```

  `server.go` wires this as `notify.New(..., s.authStore)` - `authStore`
  is already constructed before `notifyClient` in `New()` today, so this
  is just a reordering, not a new dependency edge.

- **Gating real sends**: `hasCredentials()` stays as-is (gates
  `SendFeedback`, which has no recipient/unsubscribe concept - it's
  operator-facing). New `canSendToUsers() bool` = `hasCredentials() &&
  c.linkSecret is a real configured key, not the ephemeral fallback`.
  Any future `Send*` method aimed at a real org person (starting with
  `SendWelcome`) checks this instead of `hasCredentials()` alone, and
  logs+skips (same "log instead of send" shape `SendFeedback` already
  uses for missing Mailgun creds) rather than sending a link that a
  redeploy will silently invalidate.

- **`SendWelcome`'s signature** (per welcome-email-plan.md's own
  `internal/notify` changes section, updated here): gains
  `organizationID, personID string` params -
  `SendWelcome(ctx, organizationID, personID, recipientEmail, personName,
  organizationName string) error` - both already in scope at
  `AuthController.Callback`'s call site. Before doing anything else,
  calls `c.suppression.IsUnsubscribed(ctx, organizationID, personID)`;
  **any** error (lookup failure *or* already-unsubscribed) skips the
  send and logs, rather than risk mailing someone who opted out - fail
  closed, not open, same spirit as the rest of this design.

- `welcomeHTML` gains an unsubscribe footer line linking to
  `c.UnsubscribeURL(organizationID, personID)`.

- Also set Mailgun's `List-Unsubscribe` /
  `List-Unsubscribe-Post: List-Unsubscribe=One-Click` headers on the
  message (`(*mailgun.Message).AddHeader` - confirmed present on the
  `mailgun-go/v4 v4.23.0` version already in `go.mod`) pointing at the
  same `UnsubscribeURL`. This is the mechanism Gmail/Yahoo's bulk-sender
  rules actually check for and what drives the native one-click
  "Unsubscribe" button next to the sender name (separate from, and in
  addition to, the in-body link) - worth doing alongside the in-body link
  rather than after, given welcome-email-plan.md's own verification note
  already flags deliverability/spam-filtering as the thing to check by a
  real end-to-end send.

## Route / controller

New `internal/server/controllers/unsubscribe.go`, `UnsubscribeController`
(holds `authStore`, `notifyClient` - the latter only for
`VerifyUnsubscribeToken`/building the confirm-page link, not for sending
anything). Two routes, both added to `publicPrefixes` in
`internal/server/middleware/auth.go` (`/unsubscribe` - the visitor has no
session, same reasoning as `/privacy`/`/terms` needing to be reachable
pre-login):

- `GET /unsubscribe?org=...&person=...&sig=...` - verifies the token; on
  success, renders a plain confirmation page ("Unsubscribe from
  Resonance emails?" + a POST-backed button), reusing the same
  query params as hidden form fields. **No mutation on GET** - a
  corporate email-security scanner or link-prefetcher hitting the URL
  before a human ever sees it is a real, known failure mode for
  GET-triggers-the-action unsubscribe links, not a hypothetical one.
  Invalid/tampered signature renders a generic "this link isn't valid"
  page - don't distinguish "bad signature" from "unknown org/person" in
  the response, so the endpoint can't be used to enumerate valid
  org/person id pairs.
- `POST /unsubscribe` (same three fields, resubmitted from the
  confirmation form) - re-verifies the token, then
  `authStore.SetUnsubscribed`, renders a final "you're unsubscribed"
  page.

Both views are plain, layout-free pages (like the marketing
Privacy/Terms pages) - not wrapped in the authenticated app `Layout`,
since the visitor may have no session/org context at all.

## Suggested build order

1. Migration + `auth.Store.IsUnsubscribed`/`SetUnsubscribed` - small,
   testable in isolation against `internal/auth`'s existing test
   patterns.
2. `notify.LoadLinkSecretFromEnv` + `unsubscribe_token.go`
   (`UnsubscribeURL`/`VerifyUnsubscribeToken`) - pure functions, easy to
   unit test (sign then verify, tamper then expect failure) with no
   Mailgun/DB dependency.
3. `UnsubscribeController` + routes + `publicPrefixes` entry, built and
   verified against step 2 alone (no real `SendWelcome` needed yet to
   test the confirm/submit flow - a hand-built URL using a locally
   printed token is enough).
4. `notify.Client`'s `SuppressionChecker`/`canSendToUsers` wiring +
   `SendWelcome` signature update - this is where this plan and
   welcome-email-plan.md's own build order meet; do this step as part of
   that plan's step 2/3, not before `SendWelcome` exists to wire it into.

## Verification note

Same "real end-to-end send to a real inbox" step welcome-email-plan.md
already calls for should include actually clicking the unsubscribe link
from that real inbox, confirming the confirmation page renders, POSTing
it, and then confirming a second `SendWelcome` call for that same
person (hand-triggered, not waiting on a second real sign-in) is skipped
and logged rather than sent - the one behavior that's genuinely worth
checking against the real flow rather than just a unit test of
`IsUnsubscribed`.
