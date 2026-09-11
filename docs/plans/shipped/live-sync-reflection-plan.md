# Plan: live sync reflection

**Status: Option C (poll-while-open banner, no auto-merge) and
`MergeSet`'s already-tracked-item reconciliation shipped 2026-09-05 - this
doc's "Suggested build order" steps 2 and 3: see `SyncCheck` and the
extended `MergeSet` in `internal/server/controllers/sets_publish.go`,
`pcoItemsFingerprint` in `internal/server/controllers/sets_builder.go`, and
the poll wiring in `internal/views/scheduling/set_builder.templ`. Option A
(real PCO webhooks) and the harder order-diff/multi-viewer questions in
"Open questions" below remain not started and have been spun out to their
own doc, `docs/plans/not-started/pco-webhooks-live-sync-plan.md` - see
that doc for their design content, and this doc's own "Proposed design"
and "Open questions" sections below (kept here unchanged) for the full
research/build history they were spun out of.**

Written 2026-09-05, expanding `docs/ideas.md`'s "Reducing friction vs.
Planning Center" item 1 ("Live sync reflection") into an actual design.
Not built yet - written up for later, same spirit as
`docs/plans/shipped/song-tags-setup-redesign-plan.md` was before it shipped
and `docs/plans/shipped/song-lead-sheet-attachments-plan.md`. This is the
first of that section's five ideas and, on paper, the most architecturally
invasive: unlike items 2-4 (a cron job, a CSS width, a browser extension),
this one needs either a new push-based ingress into the app (a public
webhook endpoint) or a new recurring pull loop tied to a page being open -
neither of which this codebase has today. The research below is what
turned "PCO webhooks + htmx's existing swap model could close that loop"
from a one-line brainstorm bullet into something buildable, and it
surfaces enough genuinely unresolved questions that this doc deliberately
does **not** end with a single recommended mechanism - see "Open
questions" below.

## Current state

**Sync is one-directional and stateless per request.** There is no
server-side "this draft is open" session at all: `BuildPlanPage`
(`internal/server/controllers/build.go:451`) is a plain stateless GET that
rebuilds `SetBuilderData`/`RosterBuilderData` from this app's own Postgres
tables plus a handful of live PCO reads (`pco.GetItems`, arrangement/key
lookups) on every hit - see `buildSetBuilderData`
(`internal/server/controllers/sets_builder.go`, referenced throughout
`docs/pco-api-risks.md`). Nothing about a page being open right now is
tracked anywhere; "stale" in the brainstorm bullet's sense literally means
"whatever this GET last returned, now possibly wrong, with nothing telling
the browser so." Two browser tabs on the same plan, or two different
people, are just two independent request cycles - there's no shared
in-process or DB row that says "N viewers currently have plan X open."

**The three existing sync directions**
(`internal/server/controllers/sets_publish.go`), per CLAUDE.md's Sync
model section:

- `PublishSet` (line 77) - pushes the local draft to real PCO plan items:
  deletions, creates, a single `pco.ReorderItems` call, then a targeted
  `UpdateItem` for anything with `InfoPending` (a note/key edit not yet
  synced). Deliberately **never touches Title** after creation - "a title
  someone edited directly in Planning Center isn't clobbered by a
  reorder" (line 69's doc comment) - which is the one place today's code
  already picks a per-field winner between "what PCO has now" and "what
  this app knows," rather than a blanket rule.
- `RevertSet` (line 604) - discards every local change and rebuilds the
  local set from whatever PCO's plan currently has. A full, destructive
  "PCO wins" operation.
- `MergeSet` (line 542) - the actual closest existing analog to live-sync
  reflection: a **non-destructive** pull that walks the plan's current
  items (`deriveMergeImportRows`) and inserts a fresh local row for any
  song-linked item whose PCO item id isn't already tracked (active or
  tombstoned), leaving every already-tracked row completely untouched -
  "no local edit... is discarded or altered" (doc comment). Critically,
  **`MergeSet` only handles net-new PCO items**. It has no concept of "an
  already-tracked item's title/key/notes changed in PCO since we last
  looked" - that case isn't reconciled by any existing code path today,
  destructive or not. A live-sync feature inherits this gap unless it
  closes it.

All three are manual, button-triggered actions on the currently-rendered
page - none run on a timer or in response to anything happening outside
this app's own request/response cycle.

**PCO API cost, from `docs/pco-api-risks.md`** (the living reference this
plan must reason against, not re-derive):

- **100 requests / 20 seconds, per authenticated user token** - PCO's
  hard limit, scoped per-token (i.e. per signed-in person), not
  per-process.
- A single full Build-page load (both Songs and Team tabs) already runs
  "100-200 sequential round trips" before mitigation, per that doc's
  changelog intro - now split so only the active tab builds per request
  (~half that cost), plus a background-throttle (`PCO_THROTTLE_ENABLED`,
  60 req/20s sustained ceiling in prod) and a 429 retry as backstops.
  There is real, documented, *already-tight* headroom on this budget -
  this is not a green field to add new polling/webhook-triggered traffic
  into without accounting for it against that ceiling, not an invented
  one.
- The existing hourly background-sync cron (`jobs.PCOJobs`, added per
  `docs/plans/shipped/pco-background-sync-plan.md`) already runs
  low-priority, paced PCO traffic (Song Info + person-data refresh) once
  per org per hour, borrowing that org's own idle session token. Its
  discovery sub-pass was throttled from hourly to once/24h specifically
  because it was making near-identical calls with nothing new to find
  (`docs/pco-api-risks.md`'s last changelog entry) - the same shape of
  mistake (re-checking something that mostly hasn't changed, on a fixed
  schedule regardless) is exactly the risk a naive polling design for this
  feature would repeat, just per-plan instead of per-org.

**What "the draft is open here" looks like on screen today, concretely**:
nothing marks staleness at all. If someone edits a plan's items directly
in PCO while a teammate has that plan's Build page open in this app, nothing
on screen changes until that person manually reloads or clicks Merge/
Discard/Publish - and if they click Publish first, `PublishSet`'s own
create/reorder logic runs against whatever PCO state happens to be true at
that moment, silently absorbing (or colliding with) whatever changed
without ever telling the user something had.

**No webhook receiver exists in this app for any PCO event today.**
`grep -rn webhook internal/ -i` turns up exactly one implemented pattern:
Stripe's, `internal/billing/webhook.go`. Its shape (verify a signature
against a shared secret, decode into a typed event, switch on event type,
ignore anything unrecognized rather than erroring) is the right shape to
imitate for a PCO webhook handler, once/if one is built - see "What
already helps" below for why PCO's own webhook design maps onto it almost
exactly.

**This app's PCO OAuth model is one shared app-wide client
(`PCO_OAUTH_CLIENT_ID`, `internal/auth/pco_oauth.go`), each organization's
`sessions` row holding that org's own user-authorized access/refresh
tokens** (`internal/auth/organizations.go` has no PCO-application-level
fields at all, just Stripe/trial/status columns) - there is no existing
concept of "one webhook subscription per organization" to hang this off
of; that would be new state, not a slot already there waiting for it (see
"Open questions").

## What already (partially) helps

**`go-pco` (the pinned dependency, `v0.21.0` per `go.mod`) already has
full PCO webhook support built in** - this was the single most load-bearing
finding of this research pass, and changes the framing from "does PCO even
support this" to "which of several already-available mechanisms to use."
Present at the installed version (`webhookReceiver.go`,
`webhookSubscriptions.go`, `webhookEvents.go`, `availableEvents.go` in the
module):

- `pco.CreateWebhookSubscription(ctx, &pco.CreateWebhookSubscriptionParams{Name, URL, Active})`
  registers a subscription for one named event
  (`pco.GetAvailableEvents` lists every event PCO can fire, e.g. the
  doc comment's own example `"people.v2.events.person.created"`) and
  returns an `AuthenticitySecret` - **returned in full only at creation
  time** (and after `RotateWebhookSubscriptionSecret`), so it must be
  captured and stored immediately, not re-fetchable later.
- `pco.VerifyWebhookSignature`/`pco.ReadWebhookRequest` verify the
  `X-PCO-Webhooks-Authenticity` header as an HMAC-SHA256 digest of the raw
  body keyed by that stored secret - structurally identical to
  `billing.Client.HandleWebhook`'s `ConstructEvent` signature check, just
  a different algorithm/header.
- `pco.ParseWebhookDelivery`/`pco.ParseEventPayload[T]` decode the POST
  body into a `WebhookDelivery` (one delivery can carry multiple events),
  each event naming which subscribed event fired
  (`EventDeliveryAttributes.Name`) plus a JSON-encoded `Payload` string of
  the affected resource, decodable into whatever PCO response type
  matches.
- `pco.GetWebhookEvents`/`IgnoreWebhookEvent`/`RedeliverWebhookEvent`
  cover PCO's own delivery-retry bookkeeping (PCO retries a failed
  delivery on its own schedule and expects `IgnoreWebhookEvent` to call
  off further retries once handled) - relevant to idempotency design
  below.

This is real infrastructure already vendored in, not something to build
from scratch - but it answers "how would a webhook be received and
verified," not "should PCO webhooks be the mechanism at all" (see Open
questions - specifically whether an OAuth-authorized token can even create
a subscription the way a PAT traditionally has to, which this research
pass could not confirm from the library alone).

**`internal/billing/webhook.go`** is the one existing "receive and verify
an external webhook" pattern in this codebase end to end - verify
signature, decode typed event, switch on type, ignore unknown, look up the
affected local row by an id carried in the payload (there,
`ClientReferenceID`/`Metadata["organization_id"]`). A PCO webhook handler
would need the equivalent lookup: the delivery's `Payload` names a plan/
item id, which must map back to this app's own `sets`/`set_songs` rows the
same way `MergeSet`/`PublishSet` already look them up by
`(organization_id, service_type_id, plan_id)`.

**htmx's existing swap model already does two of the three things a
live-reflection UI needs**, just not for this yet:

- `hx-trigger="load once"` + a client-side visibility resync
  (`build_tabs.templ`'s `lazyTabTrigger`, `syncBuildTabVisibility` in
  `scheduling-build.js`) - fires a fetch as soon as a fragment becomes relevant
  and self-heals regardless of which of several trigger paths land last.
  This is the "fetch fresh state without a full page reload" plumbing a
  poll-driven design would reuse almost as-is, just retriggered on an
  interval instead of once.
- `hx-swap-oob` (used extensively across `person_detail_card.templ`,
  `song_detail_card.templ`, `build_detail_card.templ`) already lets one
  response patch multiple, non-adjacent parts of the page at once - the
  exact mechanic a "PCO changed, here's what's different" banner-plus-
  patched-rows response would need.
- What's **not** already there: any `hx-trigger="every Ns"` (polling) or
  Server-Sent-Events pattern anywhere in this codebase - `grep -n
  "hx-trigger\|sse\|EventSource\|every "` across every `.templ` file under
  `internal/views/scheduling/` turns up zero hits for either. "htmx's
  existing swap model" in the brainstorm bullet undersells this a little:
  the *swap* mechanics exist and are mature; the *trigger* mechanics this
  feature actually needs (recurring, or push-driven) do not exist yet at
  all and would be new.

## Proposed design

Two independent decisions drive everything else: **what triggers a
reconciliation check**, and **what happens once one detects a real
difference**. Both have a genuine tradeoff, not an obvious answer - laid
out below rather than picked for you.

### Trigger mechanism - three real options, not a foregone conclusion

| Option | How | Infra cost | Rate-limit cost | Complexity |
|---|---|---|---|---|
| **A. Real PCO webhooks (push)** | Register a `pco.CreateWebhookSubscription` per relevant event, receive deliveries at a new public endpoint, verify + reconcile only the named plan | Needs a publicly reachable HTTPS endpoint (already true - this is a deployed web app - but now an *unauthenticated-until-signature-checked* one, a new attack surface class this app doesn't have yet outside `/billing/webhook`) + new per-org subscription bookkeeping | **Best case, by far**: a webhook delivery is PCO telling this app exactly what changed, so the reconciliation fetch can be a single targeted `GetItems`/`GetPlan` call for the one affected plan - not a poll of every open plan, and not proportional to how many people have a Build page open. Doesn't touch the 100 req/20s ceiling at all on the "nothing changed" case, since nothing polls when nothing happened. | Highest: subscription lifecycle (create at org connect time? at first Build-page visit? rotate/delete on disconnect?), a new authenticated-by-signature-not-session route class, delivery retry/idempotency handling (`GetWebhookEvents`/`IgnoreWebhookEvent`) |
| **B. Poll on an interval, unconditionally** | A background job (cron, like `jobs.PCOJobs`) periodically re-checks every plan with a local draft | No new public endpoint | **Worst**: cost scales with (number of orgs) x (number of draft plans per org), runs whether or not anyone has the page open, and repeats the exact "checking something that mostly hasn't changed, on a fixed schedule" mistake `docs/pco-api-risks.md`'s own changelog just called out and fixed for the discovery sub-pass (hourly -> 24h, because most ticks found nothing new) | Low-medium: reuses the existing cron/job pattern almost exactly |
| **C. Poll only while a draft page is actively open** | `hx-trigger="every Ns"` on the Build page itself, hitting a cheap "has this plan changed since I last checked" endpoint | No new public endpoint, no new cron | **Bounded by concurrent viewers**, not total org/plan count - only costs anything when someone's actually looking, which directly matches the brainstorm bullet's own framing ("while a draft is open here"). Still additive on top of an already-tight budget if the check itself isn't cheap (see below) or if `N` is small enough to matter within a 20s window with multiple tabs open. | Low: no new receive-side plumbing, just a new lightweight GET endpoint + an `hx-trigger="every"` on the existing page |

**C is the pragmatic starting point** if this ships incrementally: it
directly matches the brainstorm bullet's own scoping ("while a draft is
open here"), needs no new public/unauthenticated endpoint, and reuses
htmx patterns this codebase already has everywhere else (`hx-swap-oob`,
`load once`-style triggers) - it only adds a *trigger* mechanic
(`hx-trigger="every Ns"`) that's genuinely new. **A is the architecturally
"right" long-term answer** the brainstorm bullet actually named, and is
the only option whose rate-limit cost doesn't scale with polling
frequency or open-tab count at all - but it's also the one with the most
unresolved plumbing (see Open questions) and the most new attack surface.
**B should probably be ruled out outright** - it has the worst cost
profile and no compensating simplicity advantage over C.

A real middle path worth naming: **C now, A later** - ship the
poll-while-open banner first (cheap, self-contained, matches existing
patterns), and treat real webhooks as the natural upgrade path once/if
adoption or a specific pain point justifies the extra plumbing. This
mirrors how `MergeSet` itself shipped as a manual, on-demand action before
this idea proposes making any part of it automatic.

**Making C's poll cheap is itself an open design point, not free**: the
naive version (re-run the equivalent of `buildSetBuilderData` every N
seconds per open tab) would recreate exactly the "100-200 calls per
render" cost `docs/pco-api-risks.md` fought hard to fix, just on a timer
instead of per-click. The check itself needs to be cheap enough to poll -
e.g. a single `GetItems` (or a lighter PCO endpoint if one exists -
unconfirmed, see Open questions) compared against a locally-stored
"last-known" fingerprint (item count + max updated-at, or similar), only
triggering the fuller rebuild when that comparison actually differs.

### Where it surfaces in the UI

Whichever trigger mechanism, the safest first version is **a banner, not
silent auto-merge**: "Planning Center has changes since you opened this
page" with an explicit action, not an automatic page rewrite mid-edit -
auto-swapping content under someone's cursor while they're mid-drag on the
song order (this page's core interaction) is a worse experience than a
stale-but-stable page, not a better one. Concretely, this is **`MergeSet`,
automatically triggered and continuously kept fresh, but not automatically
applied** - the detection loop (trigger mechanism above) decides *when* to
check and *whether* to show the banner; clicking the banner still runs the
existing `MergeSet`-shaped reconciliation, not a new auto-apply path. That
scoping deliberately punts full auto-merge to a later iteration once the
banner-plus-manual-pull version has proven the detection side actually
works and doesn't false-positive.

This also means **the "something changed" banner must cover more ground
than `MergeSet` (line 542) currently does** - `MergeSet` only ever adds
net-new song-linked items; it has no notion of "an already-tracked item
changed" (see Current state above). A live-sync feature needs a real diff,
not just an existence check: for each already-tracked `set_songs` row,
whether the PCO item it's linked to still has the title/key/description
this app last saw. Building that diff is new work, not something
`MergeSet` already does under a different name.

### Conflict handling - the hard part, not glossed over

The actual hard case: **PCO changed something AND the local draft has an
unpublished, local-only change to the same thing.** This needs a real,
field-by-field policy, not one global "PCO wins" or "local wins" rule -
and the existing code already has a precedent for exactly this kind of
per-field split, worth generalizing rather than inventing fresh:

- **Title** - `PublishSet` already deliberately never touches it after
  creation ("a title someone edited directly in Planning Center isn't
  clobbered by a reorder"). The existing precedent is PCO wins, always,
  once an item exists - a live-sync reflection should keep this: a title
  edited in PCO should show up in this app's rendering (it's read fresh
  on every render already, via `GetItems`), never something this app
  offers to "resolve" a conflict on.
- **Order/sequence** - genuinely two-sided: a person could reorder locally
  (unpublished, `SetSong.Moved`) while someone else reorders the live PCO
  plan directly. `mergeServiceOrder`'s existing anchor-based merge
  (referenced throughout `sets_publish.go`) already has real logic for
  "how do tracked songs interleave with untracked/foreign items" at
  *publish* time - whether that same logic can answer "what does the
  *reflection* banner even show for a reordered set" (a full reorder diff
  is harder to summarize than "3 songs added") is unresolved; may need to
  simplify to "order changed in Planning Center - review before
  publishing" rather than a literal before/after diff.
- **Description/Length/ArrangementID/KeyID** (`InfoPending`) - today,
  these are local-owned until the next `PublishSet`/`RefreshSongInfo`
  push; PCO is never treated as a source of truth for them once a local
  edit is pending. A live-sync check that saw these change in PCO (e.g.
  someone corrected a key mistake directly in PCO) has no existing
  precedent to lean on at all - does a pending local `KeyOverride` win
  outright (matches today's implicit behavior, simplest to keep), or does
  the banner need to surface "this was also changed in PCO" as a
  three-way conflict for a human to pick? **Needs a real decision, not a
  default**: this is the one field-conflict case with no existing analog
  to copy.
- **Deletion races** - PCO deletes an item this app has a
  pending-deletion tombstone for already (harmless, converges to the same
  end state either way), versus PCO deletes an item this app still has as
  an *active*, non-tombstoned `set_songs` row (the row now points at a
  PCO item id that no longer exists) - the latter needs explicit handling
  (drop the local row? flag it broken? offer to re-create it on next
  publish?) that no existing code path covers, since nothing today expects
  a tracked `pco_item_id` to ever go missing out from under it.

None of these should be answered by this plan with a guess - they're
exactly the kind of decision that needs a real product call once someone's
actually looking at a live diff, not something to default silently during
implementation.

### Multi-tenant / multi-user implications

Per CLAUDE.md's Organization-scoping section, every piece of this needs
org scoping, and the multi-user case needs explicit thought since it's
genuinely new here (nothing today tracks "who else has this page open"):

- **Option C (poll-while-open)** is inherently per-viewer, per-tab - each
  open Build page independently polls "did *this* plan change," which
  naturally handles N people having the same plan open (N independent
  polls, no shared state needed) at the cost of N times the polling
  traffic for a heavily co-edited plan. Scoping is already implicit:
  the poll endpoint takes the same `(organization_id, service_type_id,
  plan_id)` triple every other Build endpoint does, authenticated via that
  viewer's own session/PCO token per the existing `RequireAuth` ->
  `RequireActiveOrganization` chain - no new scoping model needed, just a
  new endpoint following the existing pattern.
- **Option A (webhooks)** is the opposite: one delivery per PCO-side
  change, needing fan-out to *every* currently-open viewer of that plan
  across potentially multiple people/tabs/organizations (a webhook
  subscription is inherently org-scoped in PCO's model, but this app would
  still need to know which of its own currently-connected clients are
  looking at that specific plan to push the update to them - this needs a
  live connection per viewer (SSE/WebSocket) that doesn't exist anywhere
  in this codebase today, a materially bigger addition than option C's
  "just poll" approach implies at first glance). This is probably the
  single biggest hidden cost of option A beyond the webhook-receiving
  plumbing itself - genuinely worth weighing against C's simplicity when
  deciding.
- Regardless of mechanism, **the org-scoping discipline is the same as
  every other `scheduling.Store` method**: any new table (a webhook
  subscription's id/secret, a "last-known plan fingerprint" cache) needs
  an `organization_id` column and query filter, matching the existing
  pattern rather than inventing a new one - see CLAUDE.md's "Organization
  scoping" section and any neighboring `scheduling` store file for the
  concrete shape to copy.

### Rate-limit cost, reasoned against the actual budget

Restating the numbers from `docs/pco-api-risks.md` rather than inventing
new ones: **100 req/20s per token**, with the throttle's sustained
ceiling set to 60/20s in prod, and a single full page load already able to
cost "100-200 calls" pre-mitigation (now roughly half that, tab-split).
This is not a budget with obvious slack to spend carelessly.

- **Option A's steady-state cost is close to zero** on the "nothing
  changed" case - a webhook delivery only fires when something *did*
  change, and the targeted reconciliation fetch (one `GetItems` for the
  one affected plan) is the same order of cost `MergeSet` already pays
  today per manual click, just automatically triggered. This is the
  strongest argument for A being the right eventual mechanism despite its
  plumbing cost.
- **Option C's cost is `(polling interval) x (concurrent open Build
  pages)`** - additive, real, and worth bounding explicitly rather than
  picking an interval by feel: at even a conservative 30s poll interval,
  a single person leaving one Build page open for an hour adds 120 extra
  requests to their own token's budget across that hour - not itself
  alarming against the 100/20s *rolling* ceiling (spread thin, one call
  every 30s is nowhere near bursty), but it stacks with whatever else that
  same token is doing (any other page load, a Publish, a background sync
  sharing the same session per `jobs.PCOJobs`' "borrows and refreshes that
  org's own idle session token" design) - the actual risk is a poll tick
  landing inside an unrelated burst (a Prev/Next paging session, per the
  429 incident `docs/pco-api-risks.md`'s changelog already documents),
  not the poll's own steady rate. **Mitigation, if C ships**: make the
  poll's own check as cheap as possible (ideally zero PCO calls on most
  ticks - see the fingerprint-comparison idea above), and route it through
  the existing priority-aware throttle (`pco.WithPriority`, same
  low-priority tagging background syncs already use) so a poll tick never
  contends with genuinely interactive traffic on the same token.
- **Whichever mechanism ships, this is new PCO traffic that
  `docs/pco-api-risks.md`'s "Current state" table needs a new row for**,
  and its Changelog needs an entry the same shape as every other fix
  documented there - per CLAUDE.md's "Keeping the docs current" section,
  this is not optional follow-up, it's part of finishing the feature.

## Open questions (real decisions needed, not answered here)

1. **Can this app's OAuth-authorized access token create a PCO webhook
   subscription at all**, the way `go-pco`'s `CreateWebhookSubscription`
   implies, or does PCO's webhooks API require a Personal Access Token
   (a different auth model than the per-user OAuth tokens this app uses
   everywhere else, per `internal/auth/pco_oauth.go`)? This research pass
   could not confirm either way from the library or its tests alone.
   Per CLAUDE.md's own guidance on this exact situation ("verify live
   against the signed-in dev PCO account instead of guessing"), this needs
   a real `pco.CreateWebhookSubscription` call against the dev org before
   committing to option A at all - if it requires a PAT, that's a
   materially different (and probably disqualifying, for a multi-tenant
   SaaS with no per-org PAT today) design than "this app's own OAuth token
   registers a subscription automatically."
2. **Which event names actually exist for Services v2 plan/item
   changes** (`pco.GetAvailableEvents`'s real response) - the library's
   own doc comments only give a People v2 example
   (`"people.v2.events.person.created"`). Needs a live
   `GetAvailableEvents` call against the dev org to enumerate the real
   Services-side names (plan updated? item created/updated/destroyed?
   something else?) before any subscription can be created. Explicitly
   not guessed at here.
3. **Subscription lifecycle** if option A ships: created once per
   organization at first connect (needs a place to persist the returned
   `AuthenticitySecret` - a new table, org-scoped per the Multi-tenant
   section above), or lazily on first Build-page visit? Rotated/deleted
   on disconnect? None of this has an existing analog to copy - it's new
   surface area.
4. **Is there a cheaper "has this plan changed at all" PCO check than a
   full `GetItems`** for option C's poll body - e.g. does `GetPlan` alone
   carry a cheap `updated_at` that reflects item-level changes, or does
   detecting an item add/remove/reorder require the full `GetItems` list
   either way? Not confirmed in this pass; worth a live check before
   picking C's poll-tick implementation.
5. **The three field-level conflict-policy questions** under "Conflict
   handling" above (order-diff summarization, the Description/Key/
   ArrangementID three-way case, and broken-tracked-item handling on a
   PCO-side delete) - each needs an actual product decision, not a
   default picked to unblock implementation.
6. **Option A's per-viewer fan-out gap** (see Multi-tenant section) - if
   webhooks are ever pursued, does this app take on a live-connection
   mechanism (SSE/WebSocket) it has never needed before, or does a webhook
   delivery just invalidate a server-side cache and let each viewer's
   *next* poll/interaction pick up the change - which would mean option A
   and C aren't actually alternatives but a layered design (webhook
   invalidates, poll/next-load discovers)? This reframing is worth taking
   seriously rather than treating A and C as mutually exclusive.

## Suggested build order, once the open questions above have real answers

1. **Resolve open questions 1, 2, and 4 live against the dev PCO org**
   first - they gate whether option A is even feasible, and cost nothing
   to check before any code is written (a handful of one-off
   `pco.GetAvailableEvents`/`GetPlan` calls against the signed-in dev
   session).
2. **Ship option C's detection loop first, banner-only, no auto-merge**:
   a new lightweight "has this plan changed" endpoint (cheap fingerprint
   comparison per open question 4's answer), `hx-trigger="every Ns"` on
   the Build page, a banner (not silent) when it detects a difference.
   Self-contained, reuses existing patterns (`hx-swap-oob`, the existing
   auth/org-scoping chain), and directly matches the brainstorm bullet's
   own "while a draft is open here" scoping - the natural first
   deliverable regardless of what happens with option A later.
3. **Extend the existing `MergeSet`-shaped reconciliation to cover
   already-tracked-item changes**, not just net-new items (Current
   state's gap) - needed for the banner's "pull in changes" action to
   actually be complete, and independently useful even if step 2's
   auto-detection never ships (today's manual `MergeSet` button has the
   same gap).
4. **Settle the conflict-policy open questions (5) with a real product
   decision**, informed by however step 3's diff actually renders once
   it's real data on screen, not hypothetical.
5. **Option A (real webhooks), only after 1-4 are live and open question
   1 confirms it's feasible** - the highest-effort, highest-payoff piece,
   deliberately last: it's the only piece that removes the poll's
   ongoing rate-limit cost entirely, but everything about its lifecycle,
   fan-out, and new-attack-surface handling is new plumbing this codebase
   doesn't have a pattern for yet, unlike steps 2-4 which all extend
   something that already exists.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test`.
- Live: with a plan's Build page open in one browser, edit that same
  plan's items directly in Planning Center (add a song, change a title,
  reorder) and confirm the banner (or webhook-driven update, once/if that
  ships) appears without a manual reload, using a real dev-org PCO session
  the way CLAUDE.md's "verify live" guidance directs for anything
  PCO-behavior-dependent.
- Confirm a local, unpublished pending edit (a `KeyOverride`, an
  unpublished new song, a tombstoned deletion) survives the reflection
  flow untouched, matching whatever the conflict-policy decisions (open
  question 5) actually resolved to - not silently overwritten.
- Confirm `docs/pco-api-risks.md`'s "Current state" table and Changelog
  are updated with this feature's new traffic pattern and its real,
  measured rate-limit cost (not the estimate in this plan) before calling
  it shipped - per CLAUDE.md's "Keeping the docs current" section.
- If option A shipped: confirm a forged/unsigned webhook POST to the new
  endpoint is rejected (mirrors `billing.Client.HandleWebhook`'s signature
  check, verified the same way that one presumably was), and confirm a
  duplicate delivery (PCO's own retry behavior) doesn't double-apply a
  reconciliation - the same idempotency question `internal/billing`'s
  webhook handling has already had to answer once.
