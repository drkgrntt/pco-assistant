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
piece remains genuinely not started, unchanged from what's written here.
The content that follows is otherwise pulled forward unchanged from that
plan's own "Phase 3: mismatch warning UI, and the eventual matrix view"
and "Crowdsourced default: needs a real decision, not a default" sections.

## Crowdsourced default: needs a real decision, not a default

Per this task's own instruction (and `docs/ideas.md`'s own explicit flag
- "the priciest single item in this file"), this section deliberately
does **not** pick an answer. Following the phrasing convention
`docs/plans/shipped/live-sync-reflection-plan.md` uses for its own
unresolved mechanism questions:

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

**Conclusion**: not ready to build without a product decision on at
least (1)'s real-world coverage and (2)'s consent model - (3) and (4) are
buildable once those two are settled. Phase 2 (manual per-org song range
entry) shipped and is fully useful on its own regardless of whether this
ever gets built; crowdsourcing is an optional accelerant for filling in
otherwise-blank songs, never a blocker for the rest of that plan.

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

5. **Crowdsourced default**: not scheduled - blocked on the product
   decisions in "Crowdsourced default" above.
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
