# Plan: automated welcome email on first sign-in

Not started. Written 2026-09-15 after a discussion that started as "an
automated welcome for new users/orgs" and, once the existing in-app
onboarding was checked, narrowed to something more specific - see "What
this isn't" below.

## What this isn't

`docs/plans/shipped/onboarding-and-ambient-framing-plan.md` and
`docs/plans/shipped/help-system-and-nav-consolidation-plan.md` already
shipped a persistent setup checklist, a guided first-run tour (home
page), a second guided tour (Build page), and a click-to-open help-dialog
system per setup page. The discoverability gap `docs/ideas.md`'s
"Reducing friction vs. Planning Center" section originally flagged
("a lighter-weight onboarding nudge... pointing at the highest-leverage
features") is fully addressed in-app already. This plan is not about
that.

## What this is

An actual **email**, sent once, the first time a person ever signs in -
something that reaches someone even if they bounce off the app before
ever seeing the in-app tour. This would be the first genuinely
outbound-to-a-real-user email this app sends: `internal/notify` today
only sends *inbound* notifications to the app operator (feedback
submissions -> Derek's configured `recipientEmail`), never anything to an
org's own people.

## Decisions made (2026-09-15)

1. **One email for every brand-new person**, not two tiers split by
   "first person at a new org" vs. "teammate joining an existing org."
   Simpler to build and reason about; accepts that a teammate joining an
   already-onboarded org gets the same copy as someone starting fresh.
2. **Short, with a link back into the app** - a brief note (who it's
   from, why they're getting it, one line on the protect-team/
   protect-service framing) rather than a richer standalone pitch. The
   existing tour/checklist already does the real explaining once someone
   clicks through; a second full onboarding narrative living in the email
   itself would just be a second thing to keep in sync with the in-app
   copy over time.

## Trigger mechanics

Fires from `AuthController.Callback` (`internal/server/controllers/
auth.go`), right after the existing `ac.store.UpsertPerson` call, using
the exact same "first sign-in" signal `people.sign_in_count` already
exists to provide (see its own doc comment in `internal/auth/people.go`:
"Starts at 1 on first insert, incremented... on every later sign-in").

- `UpsertPerson` changes from `(ctx, ...) error` to
  `(ctx, ...) (signInCount int, err error)`, via
  `INSERT ... ON CONFLICT ... RETURNING sign_in_count` - the increment
  and the read happen in the same atomic statement, so there's no
  check-then-act race between two concurrent callbacks for the same
  person (e.g. a double-click on "Sign in").
- `Callback` checks `signInCount == 1` after that call; if so, and
  `personEmail != ""` (PCO's userinfo response can theoretically omit
  it - see `FetchIdentity`'s doc comment), fires the welcome send.
- Dispatched from a goroutine, not inline - unlike `SubmitFeedback`
  (moved *to* inline specifically so a real send failure is visible to
  the submitter waiting on the response, see that handler's own doc
  comment), sign-in is a genuine hot path every person hits every
  session, and nothing about a welcome-email failure should ever delay
  or block a sign-in. Matches CLAUDE.md's own documented pattern for
  this exact situation ("a background PCO sync, an outbound email... a
  goroutine a handler kicks off and returns without waiting on"): wrap
  with
  `logger.WithRequestID(context.Background(), middleware.CurrentRequestID(c))`
  before dispatch, same as `song_arrangement_cache.go`/
  `person_data_cache.go`/the feedback flow's own background-send
  precedent, so a failure still logs with a traceable request id even
  though the request itself has already returned.
- No new DB column needed for idempotency - `sign_in_count == 1` is
  itself the one-shot signal, already durable and already atomic.

## `internal/notify` changes

- New `SendWelcome(ctx context.Context, recipientEmail, personName,
  organizationName string) error`, same shape as `SendFeedback`: logs
  and returns nil instead of sending when `!hasCredentials()` (local dev
  needs no real Mailgun setup, same as today), bounded by the existing
  `sendTimeout`, sender something like
  `fmt.Sprintf("Resonance <welcome@%s>", c.mgDomain)` (a second sender
  identity, `welcome@` vs. `feedback@`, both at the same configured
  domain - no new env var needed).
- New `welcomeHTML(personName, organizationName string) string` next to
  the existing `feedbackHTML`, same "small string builder, not a templ
  component" reasoning `feedbackHTML`'s own doc comment gives (still only
  two kinds of outbound email total - a dedicated templating layer is
  still overkill).
- Package doc comment ("just one kind so far... feedback submissions")
  needs updating once this actually ships, per CLAUDE.md's own
  "Architecture" callout for `internal/notify`.

## Operational open question - resolved 2026-09-15

Mailgun sandbox/test domains follow a fixed `sandboxXXXX.mailgun.org`
pattern and restrict sending to a short allowlist of pre-authorized
recipients. The configured domain is `mg.resonanceonline.org` - a real
custom domain, not that pattern, which means it's already been added and
verified in the Mailgun account (a sandbox domain couldn't be configured
as a custom `mgDomain` value at all). Sending to arbitrary real PCO
account emails is not blocked by domain verification. Still worth the
build order's own last step - a real end-to-end send to an actual inbox
once built, to catch anything spam-filtering related, which domain
verification alone doesn't guarantee against.

## Suggested build order

1. `UpsertPerson`'s signature change (`RETURNING sign_in_count`) +
   updating its one call site (`Callback`) to just log the new count for
   now, no email yet - small, isolated, easy to verify against existing
   `internal/auth` tests.
2. `notify.SendWelcome`/`welcomeHTML` - independently testable the same
   way `client_test.go` already tests `SendFeedback`
   (`TestSendFeedbackWithoutCredentialsLogsInsteadOfSending` has a direct
   analog).
3. Wire the two together in `Callback` (goroutine dispatch, `signInCount
   == 1` + non-empty-email gating).

Small enough that this is plausibly one PR, not three separate slices -
listed only for review-checkpoint purposes.

## Verification note

No PCO API call anywhere in this design - `docs/pco-api-risks.md` needs
no update. This does touch `internal/notify` for the first time since it
was introduced, and is the app's first ever send-to-a-real-user email;
worth a quick manual end-to-end check against a real inbox once built
(sign in as a brand-new person in a dev/staging PCO org, confirm the
email actually lands and isn't caught by spam filtering), not just unit
tests against `hasCredentials()`.
