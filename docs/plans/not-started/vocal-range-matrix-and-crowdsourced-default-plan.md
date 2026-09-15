# Plan: vocal range matrix view + crowdsourced default

Spun out of `docs/plans/shipped/vocal-range-plan.md`'s own two remaining
unbuilt pieces once that plan's Phases 1-3 (person vocal range, song vocal
range + the key engine, and the roster-cross-referenced mismatch warning)
shipped 2026-09-07 through 2026-09-08 - see that doc for the full build
history and context these two pieces build on (the key engine's exact
math, the mismatch-warning surface they'd extend/visualize, and the
"Which product promise this serves" reasoning). Of the two pieces below,
**the matrix view has since shipped (2026-09-12 - see its own "Status"
note under "The matrix view" heading below)**; the crowdsourced-default
piece's open questions were resolved 2026-09-15 (see "Crowdsourced
default: resolved decisions" below) but building it hasn't started yet.
The content that follows is otherwise pulled forward unchanged from that
plan's own "Phase 3: mismatch warning UI, and the eventual matrix view"
and "Crowdsourced default: needs a real decision, not a default" sections.

## Crowdsourced default: resolved decisions (2026-09-15)

The four open questions below were each genuinely undecided until this
pass - none were derivable from existing code or a "does PCO already do
this" test, since this is a wholly new cross-org data pattern. Resolved
as follows, kept next to the original open-questions writeup rather than
replacing it so the reasoning against each is still visible:

1. **Join key coverage** (open question 1) - build against CCLI number
   alone, no title/CCLI hybrid fallback. Real-world coverage is still
   unmeasured (still chicken-and-egg - nothing short of shipping the
   feature answers it), but a hybrid reintroduces the collision risk
   `docs/ideas.md` already flags, so the decision is to accept partial
   coverage for now (songs with no CCLI number simply never get a
   suggestion) and revisit only if real usage shows coverage too sparse
   to be useful.
2. **Consent/opt-in model** (open question 2) - opt-in, off by default;
   one combined toggle for "contribute our org's ranges" and "receive
   suggestions from other orgs" (not two separate settings - simpler
   surface, at the cost of forcing both together). Any signed-in person
   in the org can flip it, matching every other setting in this app today
   - this app has no admin/member role distinction anywhere yet (every
   signed-in person is a full admin, see `SessionForBackgroundSync`'s doc
   comment in `internal/auth/sessions.go`), and this decision deliberately
   doesn't introduce one just for this toggle.
3. **Data-quality/moderation** (open question 3) - minimum sample size
   before a suggestion is shown is a **per-org configurable setting**,
   defaulting to 3 agreeing orgs, surfaced next to the consent toggle.
   Aggregation is by **mode** (the most common exact value), falling back
   to **median** when there's no true mode (e.g. every org's value
   differs) rather than showing nothing or picking an arbitrary tiebreak.
   A "report this suggestion" action lets an org flag a bad aggregate,
   landing in a new review queue in `cmd/admin` (the existing
   operator-only app, gated by `ADMIN_TOKEN` - see this file's own
   Architecture section) rather than a database-only flag with no UI.
4. **New tenancy shape** (open question 4) - aggregates are
   **precomputed**, not computed live: a new gocron job (matching the
   existing pattern in `internal/server/crons.go`) periodically
   recalculates each CCLI number's aggregate into its own new table. Raw
   per-org contributed values live in a **separate new table**, distinct
   from the existing per-org `song_vocal_range` table, so that table's
   other, non-crowdsourcing query paths never need to reason about
   cross-org access. The read path never accepts a client-supplied CCLI
   number (no `?ccli=...`-shaped endpoint) - a suggestion is always looked
   up server-side from the org's own already-authorized song record, so
   nobody can probe the aggregate for a song their org has never added.
   This resolves the plan's own security-review flag structurally (the
   read path literally cannot reach raw per-org rows, only the
   precomputed aggregate table) rather than by add-on query filtering -
   still worth a real security review once built, but the shape itself is
   the mitigation.

None of this is built yet - these are the resolved design decisions the
build should follow, not a status update on code.

## Crowdsourced default: build spec (2026-09-15)

Concrete architecture for the four resolved decisions above, produced by
a Plan-agent pass over the existing codebase patterns. Not built yet -
this is the spec a build should follow. Two additional design calls came
out of that pass and a follow-up discussion (storage shape and display
anchor, both below) - resolved, not open questions.

**Storage shape (resolved 2026-09-15, after the Plan pass):** raw
contributions and the aggregate must NOT store absolute pitch-class +
octave the way `song_vocal_ranges` does post-migration-00043 - two orgs
playing the same CCLI-numbered song in different keys produce absolute
notes that aren't comparable at all, so mode/median aggregation over
absolute values would be meaningless. Considered three shapes:
- Both `low`/`high` as absolute offsets from an arbitrarily-anchored
  tonic octave (the Plan pass's first proposal, reusing migration
  00043's pre-fix shape) - works, but needs a magic fixed-but-arbitrary
  anchor-octave constant with no real meaning.
- Both `low`/`high` as pitch-class-only offsets from tonic (0-11, no
  octave) - avoids the anchor constant, but loses whether `high` sits
  e.g. a third above `low` within the same octave or a tenth above it;
  vocal ranges routinely exceed an octave, so this ambiguity is real, not
  hypothetical.
- **`low_offset_from_tonic` (0-11, pitch-class-only, no octave) +
  `range_span_semitones` (plain, unbounded semitone count from `low` to
  `high`) - the shape actually used below.** No arbitrary anchor constant
  anywhere (pitch-class offset needs no octave; span is just a real
  distance between two notes), and no information loss (span is absolute,
  not mod 12, so the octave separation between `low` and `high` is never
  ambiguous). Strictly dominates the other two: same anchor-freedom as
  the second option, same full information as the first.

**Display anchor (resolved 2026-09-15):** a suggestion has no reference
key of its own (it's a normalized aggregate). Anchor display to **this
org's own arrangement key** for the song, when the org has one on file,
rather than an arbitrary fixed key - more immediately useful (no mental
transposition needed to compare against their own plan) than always
showing e.g. "as if in C." Reuses the existing transpose-and-preview UI
either way for re-previewing in a different key on demand.

### Architecture summary

Four new tables, one new cron job, one new settings section, one new
suggestion surface, one new admin queue. No new PCO calls anywhere -
everywhere a CCLI number or song title is needed, it's read from the
already-cached, already-org-authorized `scheduling.FetchActiveSongs`/
`fetchActiveSongs` result (60s TTL, `internal/scheduling/pco_songs.go`),
never a fresh PCO fetch.

### 1. New migration (`internal/database/migrations/`)

Next available number is **00049** (latest on disk:
`00048_people_sign_in_count.sql`). One migration, four tables - no FK to
`organizations(id)` (no table in this codebase's migrations adds that FK;
`organization_id` is always a bare `TEXT`, matching `song_vocal_ranges`,
`feedback`, etc.).

**`00049_vocal_range_crowdsourcing.sql`:**

```sql
-- +goose Up

-- Per-org opt-in: one combined toggle for "contribute our ranges" +
-- "receive suggestions from other orgs" (resolved decision 2), plus a
-- per-org configurable minimum sample size before a suggestion is shown
-- (resolved decision 3). No row = defaults (enabled=false,
-- minimum_sample_size=3) at the Go layer - "missing row is a normal
-- state, not an error."
CREATE TABLE vocal_range_crowdsourcing_settings (
    organization_id TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    minimum_sample_size SMALLINT NOT NULL DEFAULT 3,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Raw per-org contributed values, joined cross-org by CCLI number alone
-- (resolved decision 1) - separate from song_vocal_ranges (resolved
-- decision 4) so that table's other query paths never need to reason
-- about cross-org access. Never read by any per-request path - only the
-- aggregation cron reads this table; the live read path only ever
-- touches song_vocal_range_aggregates below.
--
-- low_offset_from_tonic: pitch-class-only offset from the reference
-- key's tonic (0-11, no octave - see "Storage shape" above).
-- range_span_semitones: plain semitone distance from low to high
-- (unbounded, since a range can exceed an octave).
CREATE TABLE song_vocal_range_contributions (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    ccli_number INTEGER NOT NULL,
    low_offset_from_tonic SMALLINT NOT NULL,
    range_span_semitones SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, ccli_number)
);
CREATE INDEX idx_song_vocal_range_contributions_ccli ON song_vocal_range_contributions (ccli_number);

-- Precomputed aggregate, one row per CCLI number, no organization_id
-- column at all (resolved decision 4's structural mitigation - the live
-- read path literally cannot reach a raw per-org row from here). Written
-- only by the aggregation cron, read only by server-side CCLI lookups
-- that started from an org's own authorized song record - never a
-- client-supplied CCLI.
CREATE TABLE song_vocal_range_aggregates (
    ccli_number INTEGER PRIMARY KEY,
    sample_count INTEGER NOT NULL,
    low_offset_from_tonic SMALLINT NOT NULL,
    range_span_semitones SMALLINT NOT NULL,
    -- 'mode' or 'median' per bound, kept for transparency (mode falls
    -- back to median only when a bound has no true mode, per resolved
    -- decision 3).
    low_method TEXT NOT NULL,
    span_method TEXT NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Report this suggestion" queue (resolved decision 3), reviewed only in
-- cmd/admin. song_id is recorded purely for the reporting org's own
-- context - ccli_number is the actual join key back to the aggregate
-- being flagged.
CREATE TABLE song_vocal_range_suggestion_reports (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    song_id TEXT NOT NULL,
    ccli_number INTEGER NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'reviewed')),
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX idx_song_vocal_range_suggestion_reports_status ON song_vocal_range_suggestion_reports (status);

-- +goose Down
DROP TABLE song_vocal_range_suggestion_reports;
DROP TABLE song_vocal_range_aggregates;
DROP TABLE song_vocal_range_contributions;
DROP TABLE vocal_range_crowdsourcing_settings;
```

### 2. New/changed Go files

**`internal/scheduling/vocal_range_crowdsourcing.go`** (new, mirrors
`song_vocal_range.go`'s shape; package doc comment carries the storage-
shape reasoning above):

```go
type VocalRangeCrowdsourcingSettings struct {
    OrganizationID    string
    Enabled           bool
    MinimumSampleSize int
    CreatedAt, UpdatedAt time.Time
}

// GetVocalRangeCrowdsourcingSettings returns organizationID's settings, or
// the defaults (Enabled: false, MinimumSampleSize: 3) with a nil error if
// no row exists yet.
func (s *Store) GetVocalRangeCrowdsourcingSettings(ctx context.Context, organizationID string) (VocalRangeCrowdsourcingSettings, error)

// SetVocalRangeCrowdsourcingSettings upserts organizationID's settings. If
// this transitions Enabled from true to false, also purges every raw
// contribution row this org has ever written
// (DeleteAllVocalRangeContributionsForOrganization) in the same
// transaction - opting out means no longer being counted, not just no
// longer writing new rows.
func (s *Store) SetVocalRangeCrowdsourcingSettings(ctx context.Context, organizationID string, enabled bool, minimumSampleSize int) (VocalRangeCrowdsourcingSettings, error)

// UpsertVocalRangeContribution records/replaces organizationID's current
// contributed range for ccliNumber - called from the song-vocal-range
// controller immediately after a successful SetSongVocalRange, only when
// the org's own settings have Enabled=true and the song has a nonzero
// CCLI number. Best-effort from the caller's perspective (log and
// continue, never fail the underlying song-vocal-range save over this).
func (s *Store) UpsertVocalRangeContribution(ctx context.Context, organizationID string, ccliNumber int, lowOffsetFromTonic, rangeSpanSemitones int) error

// DeleteVocalRangeContribution removes organizationID's contribution row
// for ccliNumber, if any.
func (s *Store) DeleteVocalRangeContribution(ctx context.Context, organizationID string, ccliNumber int) error

// DeleteAllVocalRangeContributionsForOrganization is
// SetVocalRangeCrowdsourcingSettings's opt-out cleanup helper - exported
// separately so it's independently testable.
func (s *Store) DeleteAllVocalRangeContributionsForOrganization(ctx context.Context, organizationID string) error

type VocalRangeAggregate struct {
    CCLINumber           int
    SampleCount          int
    LowOffsetFromTonic   int
    RangeSpanSemitones   int
    LowMethod, SpanMethod string // "mode" | "median"
    ComputedAt           time.Time
}

// VocalRangeAggregateForCCLI looks up the precomputed aggregate for
// ccliNumber - sql.ErrNoRows if none exists yet. The ONLY read this
// package exposes against song_vocal_range_aggregates; no method anywhere
// in this file reads song_vocal_range_contributions for anything other
// than an org's own rows.
func (s *Store) VocalRangeAggregateForCCLI(ctx context.Context, ccliNumber int) (VocalRangeAggregate, error)

// SubmitVocalRangeSuggestionReport records organizationID's "report this
// suggestion" flag for ccliNumber (songID kept for the reporting org's
// own context, not a join key).
func (s *Store) SubmitVocalRangeSuggestionReport(ctx context.Context, organizationID, songID string, ccliNumber int, reason string) error
```

**`internal/scheduling/vocal_range_crowdsourcing_admin.go`** (new,
cross-org, admin-only - mirrors `admin_setup_completion.go`'s precedent
for a deliberately non-org-scoped query living directly in `scheduling`):

```go
type VocalRangeSuggestionReport struct {
    ID             int64
    OrganizationID string
    SongID         string
    CCLINumber     int
    Reason         string
    Status         string
    ReportedAt     time.Time
    ReviewedAt     sql.NullTime
}

// ListOpenVocalRangeSuggestionReports returns every status='open' report,
// oldest first - cmd/admin's review queue listing.
func (s *Store) ListOpenVocalRangeSuggestionReports(ctx context.Context) ([]VocalRangeSuggestionReport, error)

// SetVocalRangeSuggestionReportStatus moves a report to "dismissed" or
// "reviewed" and stamps ReviewedAt.
func (s *Store) SetVocalRangeSuggestionReportStatus(ctx context.Context, id int64, status string) error
```

**`internal/server/jobs/vocal_range_crowdsourcing.go`** (new cron job):

```go
var vocalRangeAggregationInterval = 6 * time.Hour // tunable starting point

type VocalRangeCrowdsourcingJobs struct {
    schedulingStore *scheduling.Store
}
func NewVocalRangeCrowdsourcingJobs(schedulingStore *scheduling.Store) *VocalRangeCrowdsourcingJobs
func (j *VocalRangeCrowdsourcingJobs) Register(scheduler gocron.Scheduler) {
    scheduler.NewJob(gocron.DurationJob(vocalRangeAggregationInterval), gocron.NewTask(j.runAggregation))
}
```

`runAggregation`'s shape (set-based, not per-CCLI):
1. One bulk `SELECT ccli_number, low_offset_from_tonic, range_span_semitones FROM song_vocal_range_contributions ORDER BY ccli_number` - all rows in one round trip (matches `backupsToPrune`'s "pull everything into Go, compute in a pure function" precedent in the same `jobs` package).
2. Group by `ccli_number` in Go; for each group, compute `low`/`span` independently via a pure helper `aggregateValue(values []int) (value int, method string)`: build a frequency map, find the max frequency; if exactly one value has it, that's the mode; if more than one value ties for max frequency, fall back to the median (sorted middle value, or the lower-middle of an even count) - this tie-detection is why doing it in Go is cleaner than SQL's `mode() WITHIN GROUP`, which silently picks an arbitrary value on a tie rather than reporting one occurred.
3. `aggregateValue` and its tie-detection are pure functions - unit test directly, no DB, same testability precedent as `backupsToPrune`.
4. One transaction: bulk upsert the freshly computed rows via `unnest(...)` + `INSERT ... ON CONFLICT (ccli_number) DO UPDATE`, then `DELETE FROM song_vocal_range_aggregates WHERE ccli_number NOT IN (<the current set>)` to drop aggregates for CCLI numbers that lost their last contributor.

Registered in `internal/server/crons.go` alongside the existing
`jobs.New*Jobs(...).Register(scheduler)` lines:
```go
jobs.NewVocalRangeCrowdsourcingJobs(s.schedulingStore).Register(scheduler)
```

**`internal/server/controllers/song_vocal_range.go`** (changed):
- `setSongVocalRangeFromForm`: after a successful `sc.store.SetSongVocalRange`, best-effort mirror into the contribution table via a new unexported `mirrorVocalRangeContribution` helper - reads `GetVocalRangeCrowdsourcingSettings`; no-op if `!Enabled`; finds the song's CCLI via the already-fetched `fetchActiveSongs(ctx, orgID)` cached list (no new PCO call); no-op if `CCLINumber == 0` (join key coverage); else computes `low_offset_from_tonic`/`range_span_semitones` from the saved exact-note range and calls `UpsertVocalRangeContribution`. Logs and swallows errors - the org's own save is the thing the user asked for; crowdsourcing is a side effect.
- `DeleteSongVocalRange`: before deleting, read the row for its CCLI via the same cached lookup, and call `DeleteVocalRangeContribution` best-effort after a successful delete (same enabled-check gating).
- New handlers:
  - `SongVocalRangeSuggestion` - `GET /scheduling/song-vocal-range/:songID/suggestion`. Resolves `songID` via `fetchActiveSongs` (org-authorized), checks `GetVocalRangeCrowdsourcingSettings.Enabled`, checks the org doesn't already have a local `SongVocalRange` for this song (suggestion fills gaps, doesn't override an existing entry), looks up `VocalRangeAggregateForCCLI`, only renders if `aggregate.SampleCount >= settings.MinimumSampleSize`. Renders `views.SongVocalRangeSuggestion(...)` - transposed into the org's own arrangement key for this song when one exists (per "Display anchor" above), with a "Use this" button filling the create form's note fields and a "Report this suggestion" link.
  - `ReportVocalRangeSuggestion` - `POST /scheduling/song-vocal-range/:songID/report-suggestion`. Same server-side CCLI resolution, calls `SubmitVocalRangeSuggestionReport`, re-renders the fragment with a "Reported - thanks" state.
- Both new routes registered alongside the existing `/scheduling/song-vocal-range` routes.

**`internal/views/scheduling/song_vocal_range.templ`** (changed): the
song `<select>` gets an `onchange` hook (new function in
`scheduling-vocal-range.js`) that fetches `/scheduling/song-vocal-range/:songID/suggestion`,
swapping into a new `<div id="song-vocal-range-suggestion">` under the
form. New templ component `SongVocalRangeSuggestion(...)` reuses
`vocalRangePreviewControls`'s existing transpose-and-play markup where
possible.

**`internal/views/account_types.go`/`account.templ`** (changed):
`AccountData` gains `VocalRangeCrowdsourcing scheduling.VocalRangeCrowdsourcingSettings`
+ message/error fields. `Account` gains a new section rendering
`AccountVocalRangeCrowdsourcingBody(data)` - the combined checkbox
("Share our vocal ranges with other Resonance orgs, and see their
suggestions for songs we haven't measured yet" - exact copy still open,
see below) plus a minimum-sample-size number input. New route
`POST /account/vocal-range-crowdsourcing` -> `SetVocalRangeCrowdsourcingSettings`,
same `hx-target`/message pattern as `ClearSongInfoCache`.

**`cmd/admin/main.go` + `cmd/admin/views/vocal_range_reports.templ`**
(new): `GET /vocal-range-reports` -> `ListOpenVocalRangeSuggestionReports`,
renders a table (reported-at, org name via the same `authStore`-join
pattern `orgsPage` already uses for `OrgActivityByOrganization`, CCLI
number, song id, reason, "Dismiss"/"Mark reviewed" actions).
`POST /vocal-range-reports/:id/dismiss` and `/reviewed` ->
`SetVocalRangeSuggestionReportStatus`, redirect back. `nav()` in
`cmd/admin/views/layout.templ` gets a new link.

### 3. PCO API discipline check

No new PCO call anywhere in this design - every CCLI-number lookup goes
through the existing 60s-cached `fetchActiveSongs`, already fetched once
per song-vocal-range page render. `docs/pco-api-risks.md` genuinely needs
no update, confirming this plan's own "Verification note carried over"
section below. This *is* a new cross-org query pattern as that note
flags, so it still deserves a real security review once built - the
two-table split + no-client-CCLI read path is the structural mitigation
(resolved decision 4), but a review should confirm
`VocalRangeAggregateForCCLI`'s SQL only ever touches
`song_vocal_range_aggregates`, never `song_vocal_range_contributions`,
and that no error message anywhere echoes another org's raw values.

### 4. Suggested build order (small, independently useful slices)

1. **Migration 00049 + the settings half of `vocal_range_crowdsourcing.go`** (`Get`/`SetVocalRangeCrowdsourcingSettings`) + the `/account` toggle UI. Ships a real, useful (if inert) setting.
2. **Contribution mirroring** (`Upsert`/`DeleteVocalRangeContribution`, wired into `setSongVocalRangeFromForm`/`DeleteSongVocalRange`). Real data accumulates for any opted-in org - verifiable by querying the table directly.
3. **The aggregation cron** (`jobs/vocal_range_crowdsourcing.go`, `aggregateValue` unit-tested standalone). Verifiable via `song_vocal_range_aggregates` contents after a manual run.
4. **The suggestion read path + UI** (`VocalRangeAggregateForCCLI`, `SongVocalRangeSuggestion` handler/view, JS hookup). First user-visible payoff.
5. **The report/flag path + admin review queue** (`SubmitVocalRangeSuggestionReport`, the report button, `cmd/admin`'s new page). Any time after step 4.

Slices 1-3 don't touch any file another slice touches - candidates for
parallel `general-purpose (isolation: worktree)` builds per CLAUDE.md's
agent-delegation guidance. Slice 4 touches the same controller/view file
slice 2 touches (`song_vocal_range.go`/`.templ`), so those two shouldn't
run as parallel worktree builds against each other.

### 5. Remaining open questions (small, non-architectural)

Everything architectural is resolved above. What's left is copy/polish,
not design:
1. Exact copy for the combined consent toggle label (placeholder drafted
   above).
2. Minimum-sample-size input: disabled/grayed out when the toggle is off,
   or always editable regardless of toggle state?
3. Report reason field: free text (drafted above,
   `reason TEXT NOT NULL DEFAULT ''`) or a short fixed reason list? A
   fixed list would be a small additive change later - no schema change
   needed, it'd just constrain what the form submits.
4. **Contribution lifecycle edge case, flagged for awareness rather than
   a decision needed now:** if an org edits a *different* song that
   happens to share a CCLI number with a previous entry (unlikely but
   possible - duplicate PCO song records), the upsert silently overwrites
   their prior contribution for that CCLI, since the table's
   `UNIQUE (organization_id, ccli_number)` constraint is "one org's
   current opinion per CCLI," not per PCO song id. Deliberate, defensible
   consequence of the schema rather than a bug - not something to solve
   now.

## Crowdsourced default: needs a real decision, not a default

Per this task's own instruction (and `docs/ideas.md`'s own explicit flag
- "the priciest single item in this file"), this section deliberately
does **not** pick an answer. Following the phrasing convention
`docs/plans/shipped/live-sync-reflection-plan.md` uses for its own
unresolved mechanism questions. **Resolved 2026-09-15 - see "Crowdsourced
default: resolved decisions" above; kept below unchanged as the reasoning
record those decisions were made against.**

1. **Join key coverage, not just existence.** `SongData.Attributes.CCLINumber`
   is a real, already-wrapped field (confirmed in `go-pco/songs.go`
   during this research pass), so a reliable cross-org join key
   *exists* - this isn't the open question `docs/ideas.md` implies it
   might be. What's genuinely unknown: what fraction of songs across
   real orgs actually have a CCLI number filled in (originals,
   public-domain hymns, and orgs that simply never populated the field
   would have none), and whether a title/CCLI hybrid fallback is worth
   the collision risk `docs/ideas.md` already warns against. This is an
   empirical question this app cannot answer without either the feature
   already existing (chicken-and-egg) or a manual sample of real
   customer data - not something to guess a percentage for here.
2. **Consent/opt-in model.** Per-org toggle (default off, or default on
   with an easy opt-out)? Consent to contribute *and* consent to
   receive suggestions could plausibly be two separate settings (an org
   might want the benefit without contributing, which raises its own
   fairness question). Whatever the shape, `docs/ideas.md`'s own framing
   constraint applies: never attributable ("Grace Church set this"),
   only an aggregate ("this looks like the range") - that constrains the
   schema (no org-identifying column ever exposed in a read path) but
   doesn't by itself answer *when* a value is aggregated in vs. held
   back pending consent.
3. **Data-quality/moderation.** A single bad entry (a typo, a range
   entered against the wrong reference key, someone testing the feature
   with junk data) poisoning the cross-org aggregate for every other org
   is a real risk with no existing analog in this codebase - nothing
   today aggregates one org's input for another org's benefit. Needs an
   actual decision on at least: a minimum sample size before a
   suggestion is shown at all, some outlier-rejection rule (median
   rather than mean, trimming extreme entries), and whether an org can
   ever flag/report a bad aggregate they're seeing.
4. **New tenancy shape.** Per CLAUDE.md's Organization scoping section,
   "everything today is org-scoped... aside from a couple of
   deliberately shared vocabulary tables" (fixed lookup tables like
   `song_relationship_types`, not live data written by one org and read
   by another). A crowdsourced aggregate is neither - it's org-*written*
   data read cross-org, a genuinely new shape needing its own security
   review (can one org's query ever be shaped to leak another org's raw
   per-org value rather than the aggregate alone?), not just a new
   table.

**Conclusion**: **resolved 2026-09-15 - see "Crowdsourced default:
resolved decisions" above.** (Original conclusion, kept for record: not
ready to build without a product decision on at least (1)'s real-world
coverage and (2)'s consent model - (3) and (4) are buildable once those
two are settled.) Phase 2 (manual per-org song range entry) shipped and is
fully useful on its own regardless of whether this ever gets built;
crowdsourcing is an optional accelerant for filling in otherwise-blank
songs, never a blocker for the rest of that plan.

## The matrix view (from "Phase 3: mismatch warning UI, and the eventual matrix view")

**Status: shipped 2026-09-12, see `internal/server/controllers/
vocal_range_matrix.go`/`internal/views/scheduling/vocal_range_matrix.templ`
(+ `vocal_range_matrix_types.go`) - one plan at a time (that plan's own
roster as rows, that plan's own set songs as columns), reusing
`classifyVocalRangeCell` (extracted from `vocalRangeWarningForSong`,
`sets_builder.go`) so every cell agrees with the Songs tab's own per-song
mismatch badge, linked from the Build page next to "Run sheet" rather than
a new Songs/Team-style tab (see that templ file's own doc comment for why
a tab was rejected). The crowdsourced-default section immediately below
remains genuinely not started - unrelated to this build.**

**The matrix view**: a presentation-only page once both ranges exist,
cross-tabulating a team's people (rows) against a plan's songs (columns)
to show "who can sing what, in the key it's actually in" at a glance.
Genuinely new UI, not an extension of the Build page, and purely a read
over data Phases 1-2 already produce - no new storage, no new PCO calls.
Per CLAUDE.md's agent-delegation guidance, this is a strong future
candidate for a `general-purpose (isolation: worktree)` build once
Phases 1-2 are live ("a new read-only page" is one of the four builds
CLAUDE.md already cites as having run well that way) - but it's
deliberately not fully speced in this document, the same way that plan
deferred the crowdsourced-default piece rather than guessing at its
design; it deserves its own short plan doc once there's real range data
to build the view against.

(Both Phases 1-2 it refers to above - person vocal range and song vocal
range + the key engine - are now live; see
`docs/plans/shipped/vocal-range-plan.md`.)

## Suggested build order (unchanged from the original plan)

5. **Crowdsourced default**: design decisions resolved 2026-09-15 (see
   above); build itself not started/scheduled yet.
6. **Matrix view**: shipped 2026-09-12 - see the "Status" note under "The
   matrix view" heading above.

## Verification note carried over

The original plan's own verification section flagged this explicitly:
"Confirm no new PCO call was added anywhere in Phases 1-3 (this is
entirely local-DB storage plus local math over data already fetched
for other reasons) - `docs/pco-api-risks.md` needs no update for
those phases. The crowdsourced-default piece, if it's ever built, is
the one part of this plan that *would* need a `docs/pco-api-risks.md`
entry (new cross-org query pattern) and a real security review, per
its own open questions above." That caveat still applies to whichever
of these two pieces gets built first.
