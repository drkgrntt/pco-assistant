# Plan: key-change tip surfaced inline

Written 2026-09-05, expanding the "Key-change tip surfaced inline" bullet
in `docs/ideas.md`'s "Songs" section into a real plan. Not built yet.

This depends on where `docs/plans/not-started/in-place-set-warnings-plan.md`
lands, which was being written concurrently with this doc and also isn't
built yet. That plan's own research concluded the key-transition warning is
the one warning shape among this app's four that's inherently about the
*gap between two rows* rather than one row or the whole plan, and - because
its message is a full sentence, too long for an always-visible inline pill -
leans toward rendering it via `components.Disclosure` (click-to-expand,
keyboard/screen-reader accessible) rather than a hover-only tooltip or a
plain `ConflictBadge`. Nothing here re-opens that decision. This plan
assumes whatever surface that plan settles on (a `Disclosure` popover between
the two songs is the leading candidate, but that plan explicitly leaves the
exact structural placement - inter-row `<tr>` vs. captioned badge on the
second song - as a real open question) and treats the key-change tip as
**one more paragraph inside that same popover**, appended after the existing
warning sentence, never a second badge/icon of its own. If that plan hasn't
landed by the time this one is picked up, re-read it first rather than
re-deciding "where does a key-transition warning render" here.

## Current state

`internal/server/controllers/sets_builder.go` already has all the pitch-class
math this plan needs; nothing here requires new music-theory code beyond one
small reverse lookup (see "What's actually new" below):

- `keyPitchClasses` (~L1152): a 7-entry map, natural letter -> pitch class
  0-11 (`C:0, D:2, E:4, F:5, G:7, A:9, B:11`).
- `parseKeyPitchClass` (~L1164): parses a key string ("Bb", "F#m", "G") into
  a 0-11 pitch class, ignoring major/minor quality entirely (a trailing "m"
  is stripped, doesn't change the result - this app treats key-transition
  awkwardness as pure root movement, not mode). Accepts a bare letter A-G
  optionally followed by a single `#`/`♯` or `b`/`♭`; anything else (empty
  string, "capo 2", a double accidental) returns `ok=false`. Verified live
  against the actual `<datalist id="key-options">` this app's own key input
  offers (`musicalKeyOptions`, `song_detail_card.templ` ~L406-431): all 12
  chromatic notes, sharp and flat spellings both for the 5 that have one
  (C#/Db, D#/Eb, F#/Gb, G#/Ab, A#/Bb), major then the same list again with a
  trailing "m" - 34 entries, all of which `parseKeyPitchClass` handles
  correctly. It's a free-text input though (`list=` on a plain `<input>`,
  not a strict `<select>`), so an unlisted spelling (e.g. "Cb", "E#") can
  still reach this code - `parseKeyPitchClass` parses those too (worked
  through below; the suggestion math holds up even for those, not just the
  34 listed ones).
- `semitoneDistance` (~L1190): minimal circular distance (0-6) between two
  pitch classes.
- `keyTransitionWarnings` (~L1209): flags two songs that are truly
  back-to-back in the merged order (`mergeServiceOrder`) whose *effective*
  keys (local override, falling back to the arrangement's cached default -
  `effectiveKey`, same value `SetSongView.Key` shows) are a **half-step
  (distance 1) or a tritone (distance 6)** apart. This is the one correction
  worth flagging up front: `docs/ideas.md`'s own bullet for the shipped
  warning calls it "the jarring interval half... flags a half-step key
  change," but the code (L1229: `if change != 1 && change != 6 { continue }`)
  already flags both intervals and has done since it shipped - there are two
  distinct message strings today, one for `change == 1` ("half-step key
  change") and one for `change == 6` ("tri-tone key change"). Section
  "What actually varies by interval" below is built on this corrected
  understanding, not the doc's slightly-stale summary.
- A song with no key set, or a key string that doesn't parse, is silently
  skipped (`ok=false` from `parseKeyPitchClass` on either side) - same
  best-effort convention as `fetchRecentPlanHistory`.
- Tests: `sets_builder_warnings_test.go` covers `parseKeyPitchClass`
  (bare note, sharp/flat, minor-suffix-ignored, unparseable-rejected),
  `semitoneDistance`'s wraparound, and `keyTransitionWarnings` itself
  (flags a half-step, ignores a whole step, skips when a foreign item sits
  between the two songs, skips an unparseable key). Notably **there is no
  test exercising the `change == 6` tritone branch** even though the code
  has handled it since it shipped - not a known bug, just an untested path
  worth closing while this plan is in the same function anyway.

**How the message reaches the view today**: `keyTransitionWarnings` returns
`[]string` (pre-formatted, e.g. `"Amazing Grace (G) into How Great Thou Art
(Ab) is a half-step key change with no transition - consider a pad/bridge,
or adjusting one song's key."`), appended into `SetBuilderData.PlanWarnings`
(`internal/views/scheduling/set_types.go`) alongside the unrelated
`serviceLengthWarning` string. `set_builder.templ`'s `songWarningSpans`
(~L123-133) renders every `PlanWarnings` entry as a flat
`<div class="warning">{ msg }</div>` inside one shared `#song-warnings`
container above the order-of-service table - nothing here identifies *which*
row pair a given string is about; that's exactly the gap the sibling plan's
proposed `KeyTransitionWarning{AfterRowID int64; Message string}` struct
exists to close.

## What's actually new here (and what isn't)

`docs/ideas.md`'s bullet frames this as "a static lookup off the same
interval the warning already computes... a message per interval size" -
that framing turns out to be slightly off once you work the theory through,
in a way that actually *simplifies* the plan:

**The suggested move doesn't vary by interval at all.** The technique is:
play the dominant seventh (V7) of the *destination* key immediately before
the change - a secondary/applied dominant, standard functional-harmony
practice, valid for pulling into a target key regardless of how far away the
previous key was. A half-step arrival and a tritone arrival both resolve the
same way once you're locked onto "what's the V7 of where we're going" - the
size of the jump from the *previous* key doesn't change what note to play,
only the destination key does. So there's no real "lookup table keyed by
interval size" the way the brainstorm envisioned (two entries, one per
interval, each mapping to a different kind of suggestion) - there's one
formula, keyed by the *destination* key alone, and it applies unchanged to
both intervals `keyTransitionWarnings` currently flags. This is worth
stating plainly rather than inventing a distinction that doesn't hold up
musically, since the whole point of this feature is not to hand a director
a suggestion that's subtly wrong.

**What genuinely is new code** (not just "the same interval the warning
already computes," despite the ideas.md bullet's "not new music-theory
code" framing): going from a key name to a *pitch class* is already solved
(`parseKeyPitchClass`), but nothing today goes the other direction - pitch
class back to a *spelled note name* ("Ab" vs. "G#"). That reverse mapping is
new, and it needs to be done carefully: naive letter-cycling (jump the
tonic's letter forward by a fifth, adjust the accidental) produces
technically-correct-but-absurd spellings for a few keys (worked through
below - "A#" major's strict dominant spells as "E#", which no real chord
chart would ever write). The design below uses a small static table instead,
specifically to avoid that trap.

### The formula and the table

For a destination key with pitch class `destPC` (already computed by
`parseKeyPitchClass` - no new parsing needed):

```
dominantPC = (destPC + 7) % 12
```

(a perfect fifth above the tonic - the root of the destination key's V
chord; V7 stacks a minor seventh on top, which is what makes it pull so
hard toward the tonic - same in major and in harmonic-minor practice, so
the "m" suffix on a minor destination key doesn't change anything here,
consistent with `parseKeyPitchClass` already treating mode as irrelevant).

`dominantPC` then needs a spelled name. Seven of the twelve pitch classes
(the natural letters - 0, 2, 4, 5, 7, 9, 11) have exactly one conventional
spelling and no ambiguity. The other five (1, 3, 6, 8, 10) are the black
keys, each with a sharp and a flat spelling - exactly the same 5 pairs
`musicalKeyOptions`' own datalist already lists (C#/Db, D#/Eb, F#/Gb,
G#/Ab, A#/Bb). For those five, pick whichever spelling matches the
*destination key's own* accidental style: if the destination was written
with a `#`/`♯`, spell the suggested dominant with a sharp too; if it was
written with a `b`/`♭`, spell it with a flat. A destination written as a
bare natural letter (no accidental) has no accidental of its own to match -
working through all seven natural-letter destinations, this ambiguity only
actually arises for one of them: **B** (pitch class 11) is the only natural
letter whose dominant (pitch class 6) is one of the five ambiguous ones;
every other natural letter's dominant is itself a natural note. For that one
case, hard-code the real-world/circle-of-fifths answer: B major's dominant
is conventionally **F#**, never "Gb" - there's no live ambiguity to resolve
generically, just one specific fact to encode.

Complete worked table, one row per canonical key (major; a "...m" minor
destination reuses the same row by root - mode doesn't change the answer):

| Destination key | Pitch class | Dominant PC | Suggested V7 |
|---|---|---|---|
| C | 0 | 7 | G7 |
| C# | 1 | 8 | G#7 |
| Db | 1 | 8 | Ab7 |
| D | 2 | 9 | A7 |
| D# | 3 | 10 | A#7 |
| Eb | 3 | 10 | Bb7 |
| E | 4 | 11 | B7 |
| F | 5 | 0 | C7 |
| F# | 6 | 1 | C#7 |
| Gb | 6 | 1 | Db7 |
| G | 7 | 2 | D7 |
| G# | 8 | 3 | D#7 |
| Ab | 8 | 3 | Eb7 |
| A | 9 | 4 | E7 |
| A# | 10 | 5 | F7 |
| Bb | 10 | 5 | F7 |
| B | 11 | 6 | F#7 *(hard-coded exception - see above)* |

This is genuinely a *table*, not "a table per interval size" - it's keyed
by the destination key alone (17 rows covering all 12 pitch classes with
both spellings where the datalist offers both), and it's the same table
regardless of whether `keyTransitionWarnings` fired on a half-step or a
tritone interval. Concrete tie-back to the one existing passing test,
`TestKeyTransitionWarningsFlagsHalfStep` (E into F): the tip for that exact
case would be "**C7** (the V7 of F)."

**Why the naive letter-cycling approach was rejected**: transposing by a
literal fifth-in-letter-names (tonic letter + 4 positions in the C-D-E-F-
G-A-B cycle, then solving the accidental) is the textbook-strict way to do
this, and it agrees with the table above for every canonical key *except*
A# - strict spelling of A# major's dominant is "E#" (enharmonically F, but
spelled as a sharpened E because A# major's key signature is theoretically
all-sharps). No chord chart written for a worship team would ever write
"E#7" - it would say "F7," which is exactly what the static-table approach
above produces (pitch class 5 has one conventional name, full stop, so
there's no accidental-matching step to go wrong for it). The static table
sidesteps this failure mode entirely rather than special-casing it.
**Confirmed this also handles non-canonical free-typed keys correctly** -
e.g. "Cb" (bare letter C + flat, pitch class 11, same as B): dominant PC 6,
destination was flat-spelled, so "Gb7" - which is in fact the traditionally
correct answer for Cb major (7-flat key signature), not a fallback guess.
"E#" (pitch class 5, same as F): dominant PC 0, natural, unambiguous - C7,
also correct (E# major ≡ F major). The formula is driven by pitch-class
arithmetic with one narrow hard-coded exception, not by the input string's
surface spelling, so it degrades gracefully outside the 34 listed options
too.

### Per-interval tips, V7 as the shared default, room left for more

Derek's own framing on this (2026-09-05, across a few messages): worth
hardcoding a lookup of tip strings keyed by interval, with the
V7-of-destination move above as the *default* - "because that works so
well for basically any key" - and explicitly wanting **room left for a
wholly different, specific tip per interval later**, not just today's two.
He also raised a real, verified-below music-theory point in passing: a
4th/5th (and arguably a 6th) relationship between two keys is itself
already a smooth, natural-sounding modulation, not something that needs
fixing at all.

That last point matters for scoping this correctly, so it's worth stating
precisely rather than folding it in quietly: **`keyTransitionWarnings`
only ever calls this code for a half-step or tritone** (`change != 1 &&
change != 6` at ~L1229 is a hard `continue` for every other distance) - a
4th/5th/6th transition never reaches a tip at all today, because it never
fires a warning at all today. So "leave room for more tips" splits into
two genuinely different things, and this plan is only building the first:

1. **A specific tip for a *new* interval `keyTransitionWarnings` already
   flags or starts flagging** - purely additive, no caller change needed
   beyond adding a map entry (built below).
2. **A specific tip for an interval `keyTransitionWarnings` doesn't flag
   today** (a 4th/5th/6th, per Derek's example) - requires *also* widening
   that function's own `change != 1 && change != 6` gate, and almost
   certainly changing what kind of message it is (an affirming "that's a
   great transition, no bridge needed" note is a different *category* of
   thing than a warning telling a director to go fix something) - a real
   product-scope question, not a data/logic one, left open below rather
   than decided here.

**The music theory, checked precisely** (worth getting right before
leaning on it, and useful groundwork whenever #2 above gets picked up):
`semitoneDistance`'s minimal-circular-distance model folds an interval and
its inversion together (a fourth up reads the same as a fifth down), so
"4th or 5th" isn't two categories to this code, it's one - `distance 5`.
That is in fact the smoothest, most common modulation in tonal music
(dominant/subdominant motion) - Derek's instinct here is correct and
well-supported. A "6th" is murkier and worth being precise about instead
of overclaiming: a *major* sixth up (9 semitones) and a *minor* third
(3 semitones) collapse to the same `distance 3` - which is exactly the
relative-major/relative-minor relationship (e.g. C major and A minor share
a key signature), also a genuinely smooth pairing, but it's the
minor-third/major-sixth interval specifically, not sixths in general - a
*minor* sixth (8 semitones) instead pairs with a major third at
`distance 4`, which is a real modulation technique (chromatic mediant) but
a more colorful/deliberate one, not universally "smooth" the way 4th/5th
or relative-major/minor are - **don't extend an affirming message to
`distance 4` on the strength of this plan's research alone** if #2 above
is ever picked up; it needs its own judgment call, not a pattern-match off
`distance 3`/`5`. (`distance 2`, a whole step, is its own well-known
thing too - the deliberate "modulate up a step" pop/worship key change -
intentional and attention-grabbing rather than either jarring or smooth,
so it doesn't obviously belong in an affirming bucket either.)

**What this plan actually builds**, scoped to #1 above: a map of
interval-specific *tip functions* (not just lead-in text glued to a fixed
chord - a specific interval can override the whole suggestion, including
swapping out the chord idea entirely, not just the sentence around it),
falling back to the shared V7-of-destination default for any interval
without an override - which today means both real entries (1, 6) still
fall through to that same default, since the research above found no
better alternative for either one:

```go
// keyChangeTips holds a specific suggested-move override for a semitone
// distance (semitoneDistance), when the generic V7-of-destination default
// (defaultKeyChangeMove) isn't the best available tip for that interval.
// Empty today - neither interval keyTransitionWarnings currently flags
// (1, 6) has a better-known alternative to V7, per the research above -
// but the shape exists so a future specific tip is a one-line addition,
// not a refactor: add an entry here, suggestedKeyChangeMove already
// checks it first.
//
// IMPORTANT: adding an entry here alone does nothing for a distance
// keyTransitionWarnings doesn't already flag (e.g. distance 5, a 4th/5th -
// see this section's own research on why that one's a strong future
// candidate). That function's own `change != 1 && change != 6` gate has
// to change too before this map's entry for a new distance is ever
// reached - and widening that gate is a real product-scope decision
// (a "smooth transition" note is a different kind of message than a
// warning), not a data change, so don't add one silently.
var keyChangeTips = map[int]func(destKey string) string{
	// e.g., were distance 5 ever added to keyTransitionWarnings' own gate:
	// 5: func(destKey string) string {
	// 	return "That's a fourth/fifth apart - one of the smoothest
	// 	relationships between two keys. No bridge needed."
	// },
}

// defaultKeyChangeMove is the fallback for any interval without its own
// entry in keyChangeTips above - the V7-of-destination move
// (suggestedDominantSeventh / the table above), which the research above
// found works correctly regardless of interval size, so it's a real
// answer here, not a placeholder standing in for one.
func defaultKeyChangeMove(destKey string) string {
	chord := suggestedDominantSeventh(destKey)
	return fmt.Sprintf("Try hitting %s (the V7 of %s) right before the change - it resolves strongly into the new key.", chord, destKey)
}

// suggestedKeyChangeMove is what keyTransitionWarnings actually calls:
// an interval-specific override if one exists, the shared default
// otherwise.
func suggestedKeyChangeMove(distance int, destKey string) string {
	if tip, ok := keyChangeTips[distance]; ok {
		return tip(destKey)
	}
	return defaultKeyChangeMove(destKey)
}
```

### Is this ever actively wrong?

Purely additive, and low-risk in a specific sense worth stating precisely:
**given that `keyTransitionWarnings` already fired** (both keys parsed
successfully, per its own `ok` checks), the suggestion above is
deterministic, correct functional-harmony practice, and cannot itself
introduce a false positive - it only ever appends a sentence onto a warning
that's already decided to exist. The only way this could mislead a director
is if `keyTransitionWarnings` fires when it *shouldn't have* (a
`keyTransitionWarnings`-level false positive, not a suggestion-level one) -
and the existing test suite's own scope (ignores whole-step changes, skips
when a foreign item sits between two songs, skips unparseable keys) is
exactly the set of guards against that. No known false positive exists
today in that function; the only real gap is the untested tritone branch
noted above, worth adding a test for as part of this change rather than
leaving it implicitly-trusted.

### Enharmonic spelling consistency

Checked against how keys already display elsewhere (`SongArrangementInfo.
Key`, `SetSongView.Key`, the `musicalKeyOptions` datalist, and
`keyTransitionWarnings`' own existing message strings) - the app has no
central "canonical spelling" policy today; it just echoes back whatever
string is stored (plain ASCII `#`/`b`, not the unicode `♯`/`♭` that
`parseKeyPitchClass` also happens to accept on input). The design above
matches that: it always emits ASCII `#`/`b`, and it derives its accidental
choice *from* the already-displayed destination key rather than introducing
any new spelling convention of its own.

## Where the new text renders

Per the dependency noted at the top: append the suggestion as one more
sentence/paragraph inside whatever popover the sibling in-place-warnings
plan builds for `keyTransitionWarnings`' message, on the same trigger (the
same "AfterRowID" pair), not as a second badge, icon, or separate
disclosure. Concretely, once that plan's `KeyTransitionWarning` struct
exists, add one more field to it:

```go
type KeyTransitionWarning struct {
    AfterRowID int64
    Message    string   // existing "half-step/tri-tone... no transition" sentence
    Suggestion string   // new: "Try hitting Ab7 (the V7 of Db) right before the change - it resolves strongly into the new key."
}
```

populated by `suggestedKeyChangeMove(distance, destKey)` (see "Per-interval
framing text" above) called from `keyTransitionWarnings` right after it
decides to fire, using `change` (the distance it already computed via
`semitoneDistance`) and `curPitch`/`curKey` (the destination side) it
already has in scope - no new data fetched, no new PCO call, nothing that
changes `docs/pco-api-risks.md`. If the sibling plan's UI instead ends up
being a
captioned badge on the second song rather than a popover (that plan leaves
this genuinely open), the same principle holds: this sentence is a second
line/paragraph within whichever single surface ends up representing "the
transition between these two rows," never its own separate visual element.

## Factor `parseKeyPitchClass` into a shared "key engine" now?

`docs/ideas.md`'s "Song vocal range (relative to key)" bullet names the
exact trigger for doing this: "[the pitch-class parser is] currently
inlined for one warning check - a second consumer is a real reason to
factor it into an actual shared 'key engine'... rather than copy-pasting
the parser again." This plan is arguably that second consumer - but on
inspection, **not yet, and not this plan's job to do**:

- The new code this plan adds (`dominantPC`, the spelling table, the
  hard-coded B exception) lives in the same file
  (`sets_builder.go`), calls the *existing* `parseKeyPitchClass` directly
  (no copy-pasting - it's a genuine reuse, not a duplicate), and is
  consumed by the same function (`keyTransitionWarnings`) that already
  owns the pitch-class math. There's no package boundary being crossed and
  no second copy of the parser being written - the "second consumer"
  concern the vocal-range bullet is guarding against (duplicated parsing
  logic drifting apart across files) doesn't actually arise here.
- The vocal-range idea's "key engine" is shaped differently from what this
  plan needs: it wants a *storage-and-transposition* primitive living in
  `internal/scheduling` (canonical pitch-class representation + a
  transposition helper usable when persisting/reading a range that has to
  survive being re-expressed in whatever key an arrangement is in right
  now). This plan's need is transient and render-time only - compute a
  suggestion string, use it, done - nothing here gets stored.
- **Recommendation**: write the new function(s) as pure, dependency-free
  helpers (destination key string in, suggestion string out) right next to
  `parseKeyPitchClass`/`semitoneDistance`, following those functions' exact
  documentation style - this keeps them trivially portable. When the vocal-
  range work actually starts and needs a real `internal/scheduling` key
  engine, move all of it (the existing parser plus this plan's spelling
  table) over together at that point, rather than pre-building a package
  boundary now for a second consumer that isn't the one the vocal-range
  bullet was actually anticipating.

## Genuine open questions

- **Should `keyTransitionWarnings` widen its own gate to also surface a
  4th/5th (and possibly relative-major/minor) transition as an affirming
  "that's a smooth pairing, no bridge needed" note**, rather than staying
  silent on those the way it does today? See "Per-interval tips" above for
  the worked-through theory (4th/5th and relative-major/minor genuinely are
  smoother than what's flagged today; a major third/minor sixth and a
  whole step are more ambiguous and shouldn't be lumped in on this plan's
  research alone). This is a real product-scope decision, not something
  this plan resolves - it's a different *kind* of message (affirming, not
  warning) potentially appearing on far more transitions than today's rare
  jarring ones (real noise-vs-value tradeoff), and it changes
  `keyTransitionWarnings`' own trigger condition, not just this plan's tip
  text. `keyChangeTips`' map shape (per that section) is ready for it
  whenever this gets decided either way.
- **Exact wording.** The worked example above ("Try hitting Ab7 (the V7 of
  Db) right before the change - it resolves strongly into the new key.")
  is a reasonable draft but not something to treat as final copy without
  actually looking at it next to the existing sentence in the popover - the
  combined message (existing warning + this addition) needs a read-through
  for tone/length once the sibling plan's actual popover markup exists to
  drop it into.
- **Does a non-musician director need "V7"/"dominant seventh" explained?**
  The ideas.md bullet itself uses that terminology, and this plan's
  approach leans on it being understood by whoever reads the tip (the
  audience is someone already comfortable enough with key names to be
  looking at a key-transition warning at all) - but whether the sentence
  needs a parenthetical explaining *why* it works (the "pulls strongly
  toward that key's tonic" framing from the original bullet) or can stay
  terse is a copy decision, not a data/logic one, and is left to whoever
  builds this against the real popover.
- **Should this eventually cover a capo-based alternative for guitarists**,
  not just a keyboard-oriented V7 chord? Out of scope for this plan (the
  ideas.md bullet doesn't ask for it, and it would need a genuinely
  different kind of lookup - capo position + relative shape, not a
  dominant-seventh chord name) - flagged only so it isn't silently assumed
  covered.

## Suggested build order

1. Add the pitch-class-to-suggestion helpers next to `parseKeyPitchClass`
   in `sets_builder.go`: the static 12-entry spelling table, the `+7 mod 12`
   dominant calculation, the one hard-coded B-natural exception
   (`suggestedDominantSeventh`), `defaultKeyChangeMove`, and the (initially
   empty) `keyChangeTips` map feeding `suggestedKeyChangeMove` (see
   "Per-interval tips" above). Table-driven tests cover all 17 canonical
   keys from the worked table above, the two non-canonical free-typed cases
   ("Cb", "E#"), and `suggestedKeyChangeMove` falling through to the
   default for both distances it's actually called with today (1, 6) -
   independent of everything else here, buildable and testable in
   isolation before any UI work.
2. Add the missing `TestKeyTransitionWarningsFlagsTritone`-style test for
   the untested `change == 6` branch while in the same file (closes the
   test gap noted above; not new behavior, just coverage).
3. Once the sibling in-place-warnings plan's `KeyTransitionWarning` struct
   (or equivalent) exists, wire the new helper into
   `keyTransitionWarnings` as a `Suggestion` field on that struct.
4. Render `Suggestion` as the extra paragraph inside whatever popover/badge
   surface that plan built, per "Where the new text renders" above.

## Verification (once built)

- `go build ./...` (empty output = success).
- `make test` - new table-driven tests for the suggestion helper (all 17
  canonical keys, the B-natural exception, the two non-canonical worked
  examples), `suggestedKeyChangeMove` falling through to
  `defaultKeyChangeMove` for both distances it's actually called with
  (1, 6), and the new tritone-branch test for `keyTransitionWarnings`
  itself; existing `TestKeyTransitionWarnings*`/`TestParseKeyPitchClass*`/
  `TestSemitoneDistanceWrapsAround` tests should keep passing unchanged
  (this plan adds a field/function, it doesn't change existing behavior).
- Live check: open a plan with a real half-step transition (e.g. a song in
  G directly followed by one in Ab) and a real tritone transition, confirm
  the popover/badge shows both the existing warning sentence and the new
  suggested-move sentence together, confirm the suggested chord name is
  actually correct for the destination key shown, and confirm editing a
  key override on the detail card (the `KeyFormFragment` OOB-refresh path
  the sibling plan's doc comment calls out) updates the suggestion text
  along with the rest of the warning, not just the warning sentence alone.
- Confirm no PCO call was added and `docs/pco-api-risks.md` needs no
  update - this is pure local computation over data already in scope at
  render time.
