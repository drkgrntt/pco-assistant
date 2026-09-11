# Plan: PCO webhooks live sync (Option A)

Not started. Spun out of `docs/plans/shipped/live-sync-reflection-plan.md`'s
own remaining unbuilt piece once that plan's Option C (the poll-while-open
banner) and `MergeSet`'s already-tracked-item reconciliation shipped
2026-09-05 - see that doc for the full research/build history this builds
on (the current-state audit of PCO sync, `go-pco`'s already-vendored
webhook support, htmx's existing swap-model plumbing, and the
`internal/billing/webhook.go` pattern this would imitate). Nothing below
has been built; the content that follows is pulled forward unchanged from
that plan's own "Proposed design" and "Open questions" sections, scoped to
just the parts specific to Option A (real PCO webhooks) plus the
conflict-handling decisions that remain undecided regardless of mechanism.

## Option A, from the trigger-mechanism comparison

| Option | How | Infra cost | Rate-limit cost | Complexity |
|---|---|---|---|---|
| **A. Real PCO webhooks (push)** | Register a `pco.CreateWebhookSubscription` per relevant event, receive deliveries at a new public endpoint, verify + reconcile only the named plan | Needs a publicly reachable HTTPS endpoint (already true - this is a deployed web app - but now an *unauthenticated-until-signature-checked* one, a new attack surface class this app doesn't have yet outside `/billing/webhook`) + new per-org subscription bookkeeping | **Best case, by far**: a webhook delivery is PCO telling this app exactly what changed, so the reconciliation fetch can be a single targeted `GetItems`/`GetPlan` call for the one affected plan - not a poll of every open plan, and not proportional to how many people have a Build page open. Doesn't touch the 100 req/20s ceiling at all on the "nothing changed" case, since nothing polls when nothing happened. | Highest: subscription lifecycle (create at org connect time? at first Build-page visit? rotate/delete on disconnect?), a new authenticated-by-signature-not-session route class, delivery retry/idempotency handling (`GetWebhookEvents`/`IgnoreWebhookEvent`) |

**A is the architecturally "right" long-term answer** the original
brainstorm bullet actually named, and is the only option whose rate-limit
cost doesn't scale with polling frequency or open-tab count at all - but
it's also the one with the most unresolved plumbing (see Open questions
below) and the most new attack surface.

The middle path this plan settled on: **C now, A later** - ship the
poll-while-open banner first (cheap, self-contained, matches existing
patterns), and treat real webhooks as the natural upgrade path once/if
adoption or a specific pain point justifies the extra plumbing. C has now
shipped; A is this doc.

### Option A's rate-limit cost, reasoned against the actual budget

Restating the numbers from `docs/pco-api-risks.md` rather than inventing
new ones: **100 req/20s per token**, with the throttle's sustained
ceiling set to 60/20s in prod, and a single full page load already able to
cost "100-200 calls" pre-mitigation (now roughly half that, tab-split).
This is not a budget with obvious slack to spend carelessly.

**Option A's steady-state cost is close to zero** on the "nothing
changed" case - a webhook delivery only fires when something *did*
change, and the targeted reconciliation fetch (one `GetItems` for the one
affected plan) is the same order of cost `MergeSet` already pays today per
manual click, just automatically triggered. This is the strongest
argument for A being the right eventual mechanism despite its plumbing
cost.

### Option A's multi-tenant / multi-user implications

Per CLAUDE.md's Organization-scoping section, every piece of this needs
org scoping, and the multi-user case needs explicit thought since it's
genuinely new here (nothing today tracks "who else has this page open"):

**Option A (webhooks)** needs fan-out to *every* currently-open viewer of
a changed plan across potentially multiple people/tabs/organizations (a
webhook subscription is inherently org-scoped in PCO's model, but this
app would still need to know which of its own currently-connected clients
are looking at that specific plan to push the update to them - this needs
a live connection per viewer (SSE/WebSocket) that doesn't exist anywhere
in this codebase today, a materially bigger addition than Option C's
"just poll" approach implies at first glance). This is probably the
single biggest hidden cost of option A beyond the webhook-receiving
plumbing itself - genuinely worth weighing against C's simplicity when
deciding whether to build this at all.

Regardless of mechanism, **the org-scoping discipline is the same as
every other `scheduling.Store` method**: any new table (a webhook
subscription's id/secret, a "last-known plan fingerprint" cache) needs an
`organization_id` column and query filter, matching the existing pattern
rather than inventing a new one - see CLAUDE.md's "Organization scoping"
section and any neighboring `scheduling` store file for the concrete
shape to copy.

## Conflict handling - the hard part, not glossed over

Carried over unresolved, since it isn't specific to which trigger
mechanism ships - it's the actual hard case regardless: **PCO changed
something AND the local draft has an unpublished, local-only change to
the same thing.** This needs a real, field-by-field policy, not one
global "PCO wins" or "local wins" rule - and the existing code already has
a precedent for exactly this kind of per-field split, worth generalizing
rather than inventing fresh:

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
   picking C's poll-tick implementation. (Option C itself has since
   shipped - this question, if it still matters, now bears on whether the
   shipped poll's own check can be made cheaper, not on whether to build
   it.)
5. **The field-level conflict-policy questions** under "Conflict
   handling" above (order-diff summarization, the Description/Key/
   ArrangementID three-way case, and broken-tracked-item handling on a
   PCO-side delete) - each needs an actual product decision, not a
   default picked to unblock implementation.
6. **Option A's per-viewer fan-out gap** (see Multi-tenant section above)
   - if webhooks are ever pursued, does this app take on a live-connection
   mechanism (SSE/WebSocket) it has never needed before, or does a webhook
   delivery just invalidate a server-side cache and let each viewer's
   *next* poll/interaction pick up the change - which would mean option A
   and C aren't actually alternatives but a layered design (webhook
   invalidates, poll/next-load discovers)? This reframing is worth taking
   seriously rather than treating A and C as mutually exclusive, now that
   C is the live mechanism this would be layered on top of.

## Suggested build order, once the open questions above have real answers

1. **Resolve open questions 1, 2, and 4 live against the dev PCO org**
   first - they gate whether option A is even feasible, and cost nothing
   to check before any code is written (a handful of one-off
   `pco.GetAvailableEvents`/`GetPlan` calls against the signed-in dev
   session).
2. **Settle the conflict-policy open questions (5) with a real product
   decision**, informed by how the now-shipped Option C banner's diff
   actually renders in practice, not hypothetical.
3. **Option A (real webhooks), only after the above are resolved and
   open question 1 confirms it's feasible** - the highest-effort,
   highest-payoff piece: it's the only piece that removes the poll's
   ongoing rate-limit cost entirely, but everything about its lifecycle,
   fan-out, and new-attack-surface handling is new plumbing this codebase
   doesn't have a pattern for yet.

## Verification note carried over

The original plan's own verification section flagged, for whichever
mechanism ships here: "If option A shipped: confirm a forged/unsigned
webhook POST to the new endpoint is rejected (mirrors
`billing.Client.HandleWebhook`'s signature check, verified the same way
that one presumably was), and confirm a duplicate delivery (PCO's own
retry behavior) doesn't double-apply a reconciliation - the same
idempotency question `internal/billing`'s webhook handling has already
had to answer once." Also carried over: confirm
`docs/pco-api-risks.md`'s "Current state" table and Changelog are updated
with this feature's new traffic pattern and its real, measured
rate-limit cost (not the estimate in the original plan) before calling
it shipped - per CLAUDE.md's "Keeping the docs current" section.
