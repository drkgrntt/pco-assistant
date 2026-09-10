# Plan: switching between linked Planning Center accounts

**Status: in progress.** Option A's build-order steps 1-3 are done -
`prompt=select_account`, the orphaned-previous-session fix in `Callback`,
and a "Switch account" link (`internal/views/layout.templ`'s
`accountMenuContent`, next to Log out). Step 4 (live round-trip against the
real dev PCO org - does PCO's picker actually appear, does switching land
on the right org) hasn't happened yet; step 5 (`/oauth/revoke` on Logout)
is still an open follow-on, not part of this pass. Move to `shipped/` once
step 4 is confirmed.

Written 2026-09-09, prompted by direct feedback from a meeting with the
worship pastor (`docs/issues.md` at commit `6eaa828`, working tree since
cleared): "When logging out (or killing a session from Account,
presumably), it's not a true logout. You can easily log back in via the
login button without having to log in via planning center again. The
issue is that I couldn't change accounts because when I click sign in, I
was already signed in. How can we fix this?" Reframed by Derek as: is a
real account-switcher - swap between multiple linked PCO accounts without
a full re-auth dance each time, Google/Slack-style - architecturally
possible here.

## Current state

One browser session is exactly one `(person, organization, PCO OAuth
token)` triple, held in a single fixed-name cookie
(`middleware.SessionCookieName = "church_session"`,
`internal/server/middleware/auth.go`). `internal/auth/sessions.go`'s
`sessions` table has no uniqueness constraint on `person_id` - the schema
already tolerates several live rows for the same person (different
orgs, different devices) - but the cookie jar can only ever present *one*
token per request, so a browser is only ever authenticated as one
identity at a time regardless of how many session rows exist underneath.
There is no per-org "which of my linked accounts is active" concept
anywhere in this app's data model; `organizations` is just a flat table
of every org that's ever signed in, with no person-to-org membership
table at all.

`internal/server/controllers/auth.go`'s `AuthController` is the whole
OAuth surface: `Start` (`/auth/pco/start`, builds the PCO authorize
redirect), `Callback` (`/auth/pco/callback`, exchanges the code, resolves
identity/org, gates on Services access, calls `CreateSession`, sets the
cookie), and `Logout` (`/auth/logout`, deletes the session row and
clears the cookie). **Logout is already a real, complete local logout** -
`ac.store.DeleteSession` removes the row, `c.ClearCookie` removes the
cookie - there is no partial/soft logout bug on this app's own side.

A prior pass already landed a partial fix for the reported symptom:
commit `9e1bfba` ("Auth: force PCO to re-prompt on sign-in instead of
silently reusing its own session", already on `main`, predates this plan)
added `prompt=login` to every `/oauth/authorize` request
(`internal/auth/pco_oauth.go`'s `AuthorizeURL`). That commit's own message
correctly diagnosed half the picture - PCO's *own* browser session lives
in a separate cookie jar this app's logout can never touch - but shipped
before the exact PCO-supported `prompt` values had been checked against
PCO's docs (see Live-verified below), and it doesn't address the
orphaned-session gap this plan found (see "The real design fork").

`internal/server/controllers/account.go`'s `SignOutSession` /
`ListActiveSessions` ("who's signed in", on `/account`) is a materially
different feature from what Derek is asking for here: it lets any signed-in
admin end *another person's* session within the *same* org (see
`SignOutSession`'s own doc comment - "every signed-in person for an org is
already... a full admin"). It has no notion of one person's own multiple
identities across different orgs, and isn't reusable as-is for that - though
its per-row `ActiveSession`/`DeleteSessionByID` shape (list of live
sessions, act on one by id, not by person) is exactly the pattern a real
multi-session switcher (Option B below) would need to copy for a
per-person "your other signed-in accounts" list, were that ever built.

## Live-verified

**Root cause of the reported bug, precise:** `AuthController.Start`
(`internal/server/controllers/auth.go:43`, pre-`9e1bfba`) built the PCO
authorize redirect with no `prompt` parameter at all and, separately, never
checked for or cleared an existing session before redirecting. So clicking
"Sign in" while already holding a valid `church_session` cookie hit PCO's
`/oauth/authorize` with nothing telling PCO's OAuth server anything needed
re-approving; PCO's own session cookie (a different cookie jar, on
`api.planningcenteronline.com`, never touched by this app's `Logout`)
silently re-approved the same already-logged-in PCO identity and
redirected straight back with a fresh code for that same account - no PCO
login screen shown at all. This matches the report exactly ("I click sign
in, I was already signed in") and is a genuine code-path finding, not a
guess: `Start` builds the redirect, `Callback` never questions who comes
back, and prior to `9e1bfba` nothing in `AuthorizeURL` asked PCO to do
anything but silently reuse. **This was never actually "logout isn't
real"** - `Logout` deletes the row and clears the cookie correctly; the
gap was entirely in what happens on the *next sign-in* while PCO's own
session was still live.

**PCO's OAuth server does support forcing a fresh account choice -
confirmed against PCO's own docs** (`https://api.planningcenteronline.com
/docs/overview/authentication`, fetched directly; corroborated by a second
independent fetch/search pass for consistency; the `developer.planning
.center` SPA URL given in the task doesn't serve real content to a
non-browser fetch, this API-doc mirror does and is far more specific than
a hallucination would typically be). `/oauth/authorize` accepts a `prompt`
parameter with two documented values:

- **`select_account`** - "allow users to verify which account they will be
  signing in as and allow them to switch accounts."
- **`login`** - "will log users out of their current Planning Center
  session and force them to login again."

`select_account` is PCO's own documented recommendation for exactly this
use case - it's a lighter touch than `login`, which forces a *full* PCO
logout (killing any other PCO product tab/session the person has open
elsewhere, not just this app's connection) just to offer a picker.
Search results independently corroborate that PCO already has its own
native multi-account UX at the login layer: a "Select Your Account" page
and a "link these accounts together" feature that lets one PCO login
switch between several linked PCO accounts/orgs "with just one click" -
i.e., **PCO already owns and runs a real account switcher of its own**;
`prompt=select_account` is this app's doorway into it, not something this
app needs to rebuild. This app's current code uses `prompt=login`
(`9e1bfba`), which works for the narrow "force a login screen" bug but is
the more disruptive of the two documented values for what Derek's actually
asking for (routine switching, not "log me out of everything").

PCO also documents a real revocation endpoint, `/oauth/revoke` (`token`,
`token_type_hint`, `client_id`, `client_secret`), which this app's
`Logout` doesn't call today - it only ever deletes the local session row,
never tells PCO "this access/refresh token is dead." Not required to fix
the reported bug (PCO's own token expiry/refresh already governs token
lifetime, and `prompt=select_account`/`login` don't depend on prior
revocation), but worth flagging as unused hardening: right now a stolen
`refresh_token` row stays valid against PCO until it naturally expires,
even after this app's own `Logout` runs.

**Not yet live-tested** (this task was research/design only, no live
mutation): whether `prompt=select_account` actually renders PCO's account
chooser for a PCO login that has only *one* linked account (vs. silently
proceeding the way no-`prompt` did), and the exact click-through UX PCO
shows. Confirm with a real sign-out/switch round trip before treating this
as fully closed - same caveat `9e1bfba`'s own commit message already
raised for `prompt=login`.

## This app's own data model: single-identity-per-browser is a cookie-jar
## limit, not a schema limit

Per the task's step 5: nothing in the schema stops one person having
several live `sessions` rows at once (no unique constraint on `person_id`,
confirmed by reading `sessions.go` and its migration) - a person signed in
on two devices already produces two independent rows today, and
`SeatCounts`/`ListActiveSessions` already handle that correctly. The only
thing making this a strict one-at-a-time system is that the *cookie name is
fixed* (`church_session`) and *nothing keeps more than one token in it at
once*. Multi-account support therefore isn't a database redesign - it's a
cookie-and-selection-UI problem layered on infrastructure that mostly
already exists.

## The real design fork

**A. Force a fresh PCO login/chooser on demand, and stop orphaning the
old session.** Smallest change, closest to "just fix the bug as reported":

- Swap `prompt=login` for `prompt=select_account` in `AuthorizeURL` - the
  value PCO's own docs recommend for account switching, and less
  disruptive (doesn't force-logout every other PCO tab just to switch
  which org this app is connected to).
- Close a real gap this plan found that `9e1bfba` didn't: neither `Start`
  nor `Callback` ever end the *previous* session when someone signs in
  again while already holding a valid one. `CreateSession` inserts a new
  row and the cookie gets overwritten, but the old row is never deleted -
  it sits live in `sessions` until its 30-day TTL sweep
  (`DeleteExpiredSessions`), during which it keeps counting as a billed
  seat for its organization (`SeatCounts` dedupes by `person_id` per
  `organization_id` among *all* unexpired sessions, no matter which one
  the browser is actually using now) and keeps showing up in that org's
  Account page "who's signed in" list as if still active. Fix: in
  `Callback`, right before `CreateSession`, delete whatever session the
  incoming request's cookie currently points at (mirrors `Logout`'s own
  `DeleteSession` call) - do it post-token-exchange, not in `Start`, so an
  abandoned/cancelled PCO consent screen doesn't strand the person fully
  logged out of a session that was otherwise still good.
- Add a visible "Switch account" entry point (Account page, maybe the
  header) that just links to `/auth/pco/start` - today the only way to
  trigger this at all is knowing to click "Sign in" while already signed
  in, which is exactly the confusing UX Derek described.

This doesn't give true concurrent multi-account holding - switching is
still a real (if now much lighter, PCO-native) round trip - but it matches
what PCO's OAuth server actually supports and fixes the two real gaps this
research found (wrong `prompt` value, orphaned old session).

**B. True multi-account session support**: let the browser hold several
concurrent authenticated sessions (one per linked PCO account/org) and add
a switcher UI to flip which one is "active" with no OAuth round-trip for
an account already linked - the literal Google/Slack model, and the
closest match to Derek's own framing. Would need: a session-list cookie
scheme (either several distinctly-named cookies, one per linked account,
or one cookie holding a small ordered list of session ids with an "active"
pointer) instead of today's single fixed-name cookie; `RequireAuth`
resolving "which of possibly several sessions is active" rather than "is
there a session"; a switcher UI (`ListActiveSessions`-shaped, scoped to
*this browser's own* sessions rather than the whole org) to flip the
active one; and a decision on what happens when the active session's PCO
token needs a refresh while a person is mid-browsing a *different* one (no
correctness risk found, but real design surface to work through, not a
reuse of anything that exists today).

**Recommendation: A, not B.** PCO already runs and owns a real account
switcher at its own login layer - linked accounts, a "Select Your Account"
picker, one-click switching between them - and `prompt=select_account` is
this app's documented doorway straight into it. Building Option B
duplicates infrastructure PCO already maintains, for a workflow (which
church org someone is scheduling for right now) that's plausibly an
occasional action, not the several-times-a-session multi-tasking Slack's
workspace switcher exists for - nothing in this research turned up
evidence Derek's team is bouncing between orgs that often. Option A is a
small, targeted fix (one `prompt` value swap, one added `DeleteSession`
call, one new link) that closes both real gaps this research found and
rides PCO's own, already-battle-tested account-picker UI instead of this
app re-implementing a lighter version of it. Revisit Option B only if real
usage shows people switching org-to-org often enough that even PCO's
one-click picker becomes real friction - not a default to build now.

**C. Other options considered and ruled out:** nothing found suggests PCO
account switching can happen entirely outside this app's control in a way
that would make either A or B unnecessary - a person still has to come
back through this app's own `/auth/pco/callback` to get a `church_session`
cookie no matter which PCO account they picked, so this app always has a
role to play here; there's no "skip us entirely" path.

## Which product promise this serves

Per CLAUDE.md's protect-the-team / protect-the-service test: **neither
applies cleanly, and that's fine to say plainly rather than force.** This
isn't burnout/preference/relational-friction territory, and it isn't a
set-flow/repetition-fatigue/conflict-catching concern either - it's pure
account-management friction (a confusing, undiscoverable way to switch
which PCO org this app is connected to). It belongs on the roadmap under
CLAUDE.md's separate "friction reduction" discipline, not because it
protects the team or the service, but because right now it's a confusing
dead end for anyone managing more than one org through this app.

## Suggested build order (Option A)

1. `internal/auth/pco_oauth.go`: change `AuthorizeURL`'s `prompt` value
   from `"login"` to `"select_account"`; update the doc comment to cite
   PCO's actual documented values (both, and why `select_account` is the
   one this app sends) rather than the current comment's "PCO's own docs
   don't enumerate... supported prompt values" - they do, see
   Live-verified above.
2. `internal/server/controllers/auth.go`'s `Callback`: right before
   `ac.store.CreateSession`, if the incoming request already carries a
   valid `church_session` cookie, delete that session
   (`ac.store.DeleteSession`) so switching accounts without an explicit
   `Logout` first doesn't leave a stale, still-billed session behind for
   whichever org/person was previously active.
3. Add a "Switch account" link/button (Account page is the natural home,
   next to "Sign out") pointing at `/auth/pco/start`, so the flow is
   discoverable rather than "happen to click Sign in while still signed
   in."
4. Live-verify against the real, signed-in dev PCO org per CLAUDE.md's own
   instruction for confirming PCO behavior rather than assuming docs are
   complete: sign in, don't log out, click "Switch account," confirm PCO
   actually shows its picker/login screen (not a silent bounce back), and
   that the resulting session correctly reflects whichever account got
   chosen.
5. Not required for this fix, but worth a follow-on ticket: call
   `/oauth/revoke` from `Logout` (best-effort, same tolerance
   `UpsertOrganization` already gets) so a local logout also kills the PCO
   token server-side, not just the local session row.

## Verification

- `go build ./...` / `make test` clean once the above lands.
- Live round trip against the dev PCO org: sign in, click "Switch
  account" without logging out first, confirm PCO's own picker/login
  screen actually appears (closes the open question from Live-verified);
  choose a different linked account (or re-auth the same one) and confirm
  the app ends up scoped to the right org afterward.
- Confirm the previous session is actually gone: after switching, the
  *previous* org's `/account` "who's signed in" list no longer lists the
  now-replaced session, and `SeatCounts` for that org drops accordingly -
  this is the concrete check for the orphaned-session gap this plan
  found, not just "does sign-in still work."
