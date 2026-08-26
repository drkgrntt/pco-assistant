# Roadmap

This is a living record of product/business thinking for PCO Assistant -
distinct from the README's "what it does and how to run it." Nothing here
is a commitment; it's where the current thinking stands, including options
still being weighed against each other. Update it as decisions actually get
made, and prune sections once they're settled instead of leaving stale
options sitting next to the real answer.

## Vision / positioning

PCO Assistant is a **supplement** to Planning Center Services, not a
replacement - it only exists because of the gaps PCO leaves for worship
scheduling: song relationships, avoid-pairing/mentorship-aware team
building, repeat-song and burnout warnings, before-you-publish conflict
detection. See the README for the current feature set.

The wedge is worship teams specifically, not "volunteer scheduling" in
general - see [Expansion strategy](#expansion-strategy) for why, and why
that's a sequencing choice, not a ceiling.

## Reference: what Planning Center Services costs

Pricing this has to sit comfortably underneath the PCO bill it supplements.
PCO charges by "Team Members" (people actively scheduled/accessing the
system), not per admin seat - People (their core directory) is free, so
churches are already conditioned to pay for specific functionality rather
than for access itself.

| Team members | PCO Services price |
|---|---|
| 5 | Free |
| 20 | $15/mo |
| 50 | $32/mo |
| 150 | $69/mo |
| 400 | $115/mo |
| 1,000 | $179/mo |
| Unlimited | $239/mo |

## Pricing model (undecided - options under consideration)

Golden rule going in: **never charge per volunteer**. Churches run on
volunteers; if a tech director has to pay extra to give a teenage camera
operator an account, they'll share a login or drop the tool instead.
Whatever model gets picked, volunteers viewing/receiving published
schedules never count against it - only admin-side users (worship
directors, campus pastors, tech directors actually building sets/rosters
here) would.

### Option 1: Flat "no-brainer" fee
One tier, unlimited users, songs, and plans. $12-15/mo ($120-150/yr).
Simple, and small enough to be a ministry leader's discretionary spend
rather than something that needs a finance-committee sign-off. Doesn't
scale revenue with the churches that can afford more, and doesn't reflect
that a multi-campus church puts meaningfully more load on the app (more
people, more relationship tags, more concurrent admins).

### Option 2: Honor-system attendance tiers
Self-selected by weekend attendance, not enforced by hard limits:
- Church plant/small (<100): $9/mo
- Mid-size (100-500): $19/mo
- Large/multi-site (500+): $29/mo

Scales revenue with church size without gatekeeping features. Attendance
is a proxy for ability to pay, but not a great proxy for actual usage of
*this* app - two 150-person churches can have very different numbers of
people actually scheduling.

### Option 3: Hybrid - included admin seats + expansion (current leaning)
Ties price to the thing that actually scales with complexity here: how
many distinct people are configuring sets/rosters, not attendance or total
church size. In a single-campus church this is usually one person, so it
collapses to a flat rate naturally. Multi-service/multi-campus churches
have several (worship pastor + campus pastors + tech directors working
simultaneously), which also happens to track real infra load (DB storage,
PCO API sync volume).

| Tier | Price | Included admin seats | Target |
|---|---|---|---|
| Standard | $15/mo | 2 | Single campus, small-to-mid church |
| Multi-Campus | $29/mo | 5 (+$5/mo per extra seat) | Multi-site or large team |

Confirmed cheap to meter: PCO already has an OAuth-based identity for
every user, and the `sessions` table already records `person_id` +
`organization_id` per sign-in - "admin seat count" for an org is already
just `COUNT(DISTINCT person_id)` away, no new tracking needed. The
`organizations` table (see README) is where the actual plan/seat
assignment itself would live once billing exists.

### Mission commitment (applies regardless of which pricing option wins)
A quiet "pay what you can" path for church plants (<2 years old) or
financially struggling churches - a low-key link on the pricing page, not
an application process: email and get a free account, no interrogation.

## Reducing friction vs. Planning Center

The core adoption risk: this is a second tab, a second login, a second
place to check - no matter how good the features are, that friction
competes with just staying in PCO. Ideas below, roughly ordered by
leverage vs. build effort, not commitment order:

1. **Deep linking, both directions.** Lowest effort. From this app: a
   prominent "Open in Planning Center ↗" on a published plan. From PCO: use
   the API to write a link back into the plan's note/description pointing
   at this app's staging view for that plan, so anyone looking at the real
   plan can jump into the draft in one click.
2. **Live sync reflection.** Right now sync is one-directional at publish
   time (this app → PCO). If someone edits a plan directly in PCO while a
   draft is open here, nothing reflects that until the next load. PCO
   webhooks + htmx's existing swap model could close that loop so the app
   never shows stale-vs-PCO state.
3. **Pre-publish nudges.** Automated summary 24-48h before a service -
   open positions remaining, unresolved song-pairing conflicts - via email
   or a Slack/Discord webhook, so checking this app isn't something someone
   has to remember to do.
4. **PWA / compact side-by-side layout.** Already installable as a PWA
   (see README); worth deliberately supporting a narrow (~350px) layout so
   it's usable as a floating sidebar next to a PCO tab, not just full-width.
5. **Browser extension HUD.** Highest effort, highest ceiling: inject a
   panel directly into `services.planningcenteronline.com/plans/*` showing
   this app's warnings (conflicts, repeats, blockout collisions) as an
   overlay on PCO's own UI, so the native PCO drag-and-drop workflow never
   has to leave PCO at all. A real product bet, not a quick add - decide
   this only after the lighter-weight items above are in and adoption data
   says it's worth it.

**Framing that's now live in onboarding copy, not just an idea:** PCO is
the live broadcast; this app is the private rehearsal studio - a safe place
to test lineups and resolve conflicts before pushing the final version
live. That framing turns "why is this a separate tool" into a feature
instead of an apology. It's the centerpiece of the public landing page at
`/` (`internal/views/landing.templ`) - the rehearsal-studio comparison is
one of its sections verbatim, not just referenced.

## Expansion strategy

**Prove the worship-team wedge first.** Worship leaders are the
tightest-knit subculture in a church - they compare notes with peers at
other churches and feel song-repetition/team-chemistry pain acutely, which
makes for a clean, undeniable pitch. Trying to sell a generic
"volunteer-ops platform" to every ministry head at once on day one muddies
the message and multiplies who has to say yes.

**But the underlying data model is already ministry-agnostic** - see
`internal/scheduling/person_relationships.go`, teams, and positions in the
scheduling package: `works_well`/`mentorship`/`avoid_pairing` tags, position
preferences, and serving-frequency warnings don't know anything about
"worship" specifically.
Kids' ministry, student ministry, and production leads fight the exact same
scheduling-fatigue and mentorship-pairing problems. Once worship
directors are bought in, expanding to another ministry should mean
unlocking a view for that department's teams/positions - not rewriting the
engine underneath it.

Sequencing: **Phase 1** - worship only, prove people actually adopt and pay
for this. **Phase 2** - once that's proven, open department-specific views
for other volunteer-heavy ministries on the same backend.

## Open questions

- Which pricing option (flat / attendance-tier / hybrid-seats) actually
  ships, and at what price points - all three above are still live options.
- Picking an actual billing provider (Stripe or otherwise) and writing
  its subscription state into `organizations.status` - the table and the
  request-time gate that checks it (`RequireActiveOrganization`) already
  exist, just always passing until something writes a non-`active` value.
- Whether the browser-extension HUD is worth building at all before there's
  real usage data suggesting the lighter integrations aren't enough.
- When Phase 2 (non-worship ministries) actually starts - deliberately not
  scheduled yet; the plan is "after worship PMF," not a fixed date.
