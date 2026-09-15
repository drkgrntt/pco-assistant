# Plan: merge Song Tag Suggestions into the Song Tags page

**Status: shipped 2026-09-11.** The standalone `/scheduling/song-tag-suggestions`
page and its controller/templ files are gone; both suggestion sources now
render as a dismissible "Suggestions" panel (`SongTagSuggestionsPanel`,
`internal/views/scheduling/song_tags.templ`) on `/scheduling/song-tags`,
driven purely client-side (`applySongTagSuggestion`,
`web/static/js/scheduling-song-tags-contexts.js`) into that page's existing
bulk-add form. Only Ignore still round-trips, now at
`/scheduling/song-tags/suggestions/ignore`. See this doc's own "Resolved
architecture" section below for the file-by-file detail, all implemented as
written.

Written 2026-09-11, prompted by Derek's observation reviewing the just-
shipped Song Tag Suggestions two-list UI cleanup: the standalone
`/scheduling/song-tag-suggestions` page and `/scheduling/song-tags` (the
setup page tags actually live on) both ultimately do the same operation -
"pick a category/value, pick some songs, apply the tag to all of them" -
just with the category/value/song-selection typed by hand on one page and
precomputed on the other. Collapsing them removes a whole page and a
second, parallel "select many songs, apply one tag" implementation, not
just some markup.

## Current state

### Song Tags (`internal/views/scheduling/song_tags.templ`,
`internal/server/controllers/song_tags.go`) - see
`docs/plans/shipped/song-tags-setup-redesign-plan.md` for how this page's
own shape was decided

One `components.Card` per active song, rendered unconditionally (no
search-gate), each showing that song's existing tags as chips (with
inline edit/remove) plus its own small "add a tag" form. Above the list:
a client-side title/tag filter (`#song-tag-filter` + tag-value chips,
`filterSongTagCards`), and `songTagBulkAddForm` - a collapsed-by-default
`<details>` with one category-select + value-input + "Add tag to selected
songs" button, fed by a `song-tag-bulk-checkbox` on every card
(`form="song-tag-bulk-form"` ties each card's checkbox to the one shared
form without either living inside the other). `songTagBulkAddForm` lives
*outside* the `#song-tags-list` htmx swap target specifically so an
unrelated single-tag add/edit/delete elsewhere on the page doesn't wipe a
category/value the user was mid-typing there - see that templ's own doc
comment; this plan's new suggestions panel needs the same property, for
the same reason.

### Song Tag Suggestions (`internal/views/scheduling/song_tag_suggestions.templ`,
`internal/server/controllers/song_tag_suggestions.go`) - see
`docs/plans/shipped/song-theme-and-lyric-tag-suggestions-plan.md` for
how the theme-based second source was added

A separate page: `scheduling.SongTagSuggestions` (shared title words,
2+ songs) and `scheduling.SongThemeSuggestions` (shared PCO `Themes`
values, 2+ songs) each return `[]SongTagSuggestionGroup` (word/theme +
its songs, `AlreadyTagged` per song, already-ignored words/tags excluded).
Each group renders as its own card: a checklist of not-yet-tagged songs
(checkboxes, pre-checked), a category/value form defaulted to the group's
own word, an "Apply to selected" button (`ApplySongTagSuggestion` -> one
`CreateSongTag` per checked song), and a separate "Ignore" button
(`IgnoreSongTagSuggestion` -> writes `ignored_song_tag_suggestions`).
Crucially (per the theme-suggestions plan's own finding): **a suggestion
is never itself stored** - it's recomputed at read time from already-
cached data; accepting one is a plain `CreateSongTag` call
indistinguishable from a manually-typed tag. This precedent is what makes
the merge below safe - there's no suggestion-specific state to migrate,
only UI to consolidate.

Nav (`navMenuContent`, `layout.templ`) shows a conditional "Song Tag
Suggestions" link only when `middleware.PopulateNavState` finds something
pending (`navSuggestionsPending`). The home page
(`internal/views/home.templ` + `HomeController`) shows a "N song tag
suggestion(s) waiting for review" nudge plus a Setup-checklist row, both
linking to the standalone page - `data.SuggestionCount` is computed by
`home.go` calling `buildSongTagSuggestionsData` and counting
`len(suggestions.Suggestions)` only. **This already undercounts** - it
never adds `ThemeSuggestions` - worth fixing as part of this change, not
a new bug to file separately, since `buildSongTagSuggestionsData` itself
is going away here anyway.

## Decision: fold suggestions into Song Tags as a dismissible panel that
pre-fills the existing bulk-add form

Once a suggestion is selected, it shouldn't get its own Apply endpoint -
it should just **pre-fill and drive the bulk-add form that already
exists** (open it, set its category/value, check the matching songs'
`.song-tag-bulk-checkbox`es, clear any other selection), then let the
user review and submit that same existing form
(`BulkCreateSongTag`/`/scheduling/song-tags/bulk`). This is pure UI
behavior (a small JS function) - no new "apply" HTTP endpoint at all.
Nothing gets tagged until that existing bulk form is actually submitted,
same "review before it commits" property the standalone page's own doc
comments already called out as a deliberate house style - this merge
doesn't loosen it.

Ignore still needs a real round trip (writes `ignored_song_tag_suggestions`)
but only ever needs to re-render the suggestions panel itself - applying
or ignoring a suggestion never changes which *tags* exist, so there's no
need to touch `#song-tags-list`/the filter chips for that action.

### Why a compact row list, not one `components.Card` per group like today

The songs a suggestion is about to select might be scattered anywhere
across a long, unconditionally-rendered all-songs list - unlike the
standalone page (where a group's own checklist *was* the only place its
songs appeared), here the matching cards may be off-screen. So each
suggestion row should recap the song titles it's about to select
(actually useful context, not the redundant "same list twice" the
standalone page's own two-lists cleanup just removed - see that change's
own reasoning for why this isn't a contradiction of it) - it's simply the
*only* place that list appears now, not a second copy of something
already visible in the same viewport. Reuse the exact existing
`.suggestion-list`/`components.ListItem` row family (`components/
list_item.templ`, `list-item.css`) - already used for `set_builder.templ`'s
"Recommended next songs"/"Songs to reinforce" panels, which is the closest
existing shape to this: a title span, a muted meta span, and a trailing
per-row action - rather than inventing a new row component. Wrap the whole
panel in the existing `collapsibleSection` component (`collapsible.templ`,
already used elsewhere in this app) rather than the standalone page's old
`<details>`-per-`songTagBulkAddForm` pattern, for the same "consistent
existing primitive" reason.

## Resolved architecture

### 1. `internal/scheduling/song_tag_suggestions.go` - unchanged

`SongTagSuggestions`/`SongThemeSuggestions`/`SongTagSuggestionGroup` stay
exactly as they are; nothing about the underlying computation changes,
only where/how it's rendered and applied.

### 2. `internal/views/scheduling/song_tag_suggestion_types.go`

Keep `SongTagSuggestionGroup`/`SuggestedSongView` (still needed). Remove
`SongTagSuggestionsData` (was specific to the standalone page's full-page
render; no longer used once that page is gone). Add a small
`SongTagSuggestionsPanelData` struct (`TitleGroups`, `ThemeGroups
[]SongTagSuggestionGroup`) - one param instead of two wherever the panel
gets rendered/passed around, and the exact shape `SongTagSuggestionsPanel`
(below) needs.

### 3. New templ: `SongTagSuggestionsPanel` (put it in `song_tags.templ`
alongside `songTagBulkAddForm`, not a new file - they're two flavors of
the same "shortcut into the bulk-add form" concept and belong next to
each other)

```
templ SongTagSuggestionsPanel(data SongTagSuggestionsPanelData) {
	@collapsibleSection("song-tag-suggestions-panel", "Suggestions", <open iff anything pending>) {
		if len(data.TitleGroups) == 0 && len(data.ThemeGroups) == 0 {
			<p class="muted">No suggestions right now.</p>
		}
		if len(data.TitleGroups) > 0 {
			<h4>From song titles</h4>
			<ul class="suggestion-list">
				for _, group := range data.TitleGroups {
					@songTagSuggestionRow(group, fmt.Sprintf("%q appears in %d song titles", group.Word, len(group.Songs)))
				}
			</ul>
		}
		if len(data.ThemeGroups) > 0 {
			... same, "%q appears on %d songs" ...
		}
	}
}
```

**Important, per `songTagFilterChips`' own established precedent
(`song_tags.templ`)**: `collapsibleSection`'s outer div must render
unconditionally with its `id="song-tag-suggestions-panel"` even when both
groups are empty (use the panel's own empty-state message above, not an
`if` wrapping the whole component) - an `hx-swap-oob="true"` response
needs a matching id already present in the DOM to swap into, the exact
gotcha `songTagFilterChips`'s doc comment already documents and solves the
same way for this same page.

`songTagSuggestionRow(group SongTagSuggestionGroup, why string) templ.Component`:
one `components.ListItem("", nil, group.Word)` (or a nicer title - decide
at build time) containing:
- a `<span class="muted">{ why }</span>` (the "why" text passed in, same
  pattern the standalone page used)
- a `<span class="muted">` recap of the not-yet-tagged songs' titles,
  comma-joined (only songs where `!song.AlreadyTagged` - already-tagged
  songs don't need re-selecting since their card already shows the tag)
- `<button type="button" onclick="applySongTagSuggestion(this)"
  data-category="theme" data-value={ capitalize(group.Word) }
  data-song-ids={ comma-joined not-yet-tagged song IDs }>Select these
  songs</button>` - `capitalize` moves here from the old
  `song_tag_suggestions.templ` (same helper, same reasoning: a starting
  point for the bulk form's fully-editable value input, not a locked
  value)
- `<form hx-post="/scheduling/song-tags/suggestions/ignore"
  hx-target="#song-tag-suggestions-panel" hx-swap="outerHTML"
  hx-confirm="...">` with the hidden `word` input + "Ignore" button,
  same as today

If every song in a group is already tagged (so there's nothing left to
select), that group shouldn't render as a row at all - matches the
existing `AlreadyTagged`-skip behavior the standalone page already
implicitly had via `SongTagSuggestions`/`SongThemeSuggestions` only
including groups with untagged members; confirm this still holds after
the merge rather than assuming it.

### 4. `internal/server/controllers/song_tags.go`

- `SongTagsPage`: also fetch `ignored` (`ListIgnoredSongTagSuggestionWords`,
  wasn't fetched here before), compute title/theme suggestion groups
  (reuse the existing `songTagSuggestions`/`songThemeSuggestions` helper
  functions - see item 6 below for where they end up), pass a
  `SongTagSuggestionsPanelData` through to `views.SongTags`.
- `CreateSongTag`, `BulkCreateSongTag`, `DeleteSongTag`: after their
  existing `ListSongTags` refetch, also recompute the two suggestion
  groups (tags just changed, so a group might now be smaller or gone) and
  include the panel in their response as an OOB swap - extend
  `SongTagsMutationResponse` to also render `SongTagSuggestionsPanel`
  with `hx-swap-oob="true"` alongside its existing `songTagFilterChipsOOB`
  call, same pattern.
- New handler, replacing `IgnoreSongTagSuggestion`: writes to
  `ignored_song_tag_suggestions` exactly as today, then re-renders *only*
  `SongTagSuggestionsPanel` (not the full mutation response - ignoring a
  suggestion doesn't touch `song_tags` or change any card).
- `views.SongTags`'s own signature grows by one param
  (`SongTagSuggestionsPanelData`) - update every call site in this file
  (the early-return error branches too, which currently pass `nil, nil,
  nil` - decide what an empty/zero-value `SongTagSuggestionsPanelData`
  looks like there, matching how those branches already handle a partial
  failure).

### 5. Delete `internal/server/controllers/song_tag_suggestions.go` and
`internal/views/scheduling/song_tag_suggestions.templ`

Move `songTagSuggestions`/`songThemeSuggestions`/
`songTagSuggestionGroupViews` (still needed - the `scheduling.*` ->
`views.*` translation) into `song_tags.go`. Everything else in both files
(`SongTagSuggestionsPage`, `ApplySongTagSuggestion`,
`buildSongTagSuggestionsData`, the whole standalone templ) has no
replacement - it's superseded, not moved.

### 6. Routes (`internal/server/controllers/scheduling.go`, ~line 510)

Remove:
```
group.Get("/song-tag-suggestions", sc.SongTagSuggestionsPage)
group.Post("/song-tag-suggestions/apply", sc.ApplySongTagSuggestion)
group.Post("/song-tag-suggestions/ignore", sc.IgnoreSongTagSuggestion)
```
Add, grouped with the existing `/song-tags` routes:
```
group.Post("/song-tags/suggestions/ignore", sc.IgnoreSongTagSuggestion)
```

### 7. JS (`web/static/js/scheduling-song-tags-contexts.js`, alongside
the existing `updateSongTagBulkCount`/`filterSongTagCards`)

New `applySongTagSuggestion(button)`:
1. Read `data-category`/`data-value`/`data-song-ids` (comma-split) off
   `button`.
2. Open the bulk-add `<details class="song-tag-bulk-details">` (find it
   via `document.querySelector`, set `.open = true`).
3. Set `#song-tag-bulk-form`'s category `<select>` and value `<input>`.
4. Uncheck every `.song-tag-bulk-checkbox`, then check exactly the ones
   whose `value` is in the id list.
5. Call `updateSongTagBulkCount()` (existing function) so the "N songs
   selected" count stays correct.
6. Scroll `#song-tag-bulk-form` into view
   (`scrollIntoView({behavior:"smooth", block:"center"})`), so the user
   immediately sees the form they're about to review/submit.

### 8. CSS

No new file expected - `.suggestion-list`/`ListItem` (`list-item.css`),
`collapsibleSection` (`components/collapsible.css`), `.muted`, and the
existing button/form styles all already cover this shape. **Delete
`web/static/css/song-tag-suggestions.css`** (shipped moments ago for the
standalone page's own two-list cleanup - `.song-tag-suggestion-songs`/
`.song-tag-suggestion-tagged` have no surviving caller once that page's
templ is deleted) and its entry in `designSystemCSSFiles`
(`internal/views/layout.templ`) - don't leave orphaned, unregistered-but-
still-present CSS behind.

### 9. Nav (`internal/views/layout.templ`, `navMenuContent`)

Remove the conditional `<a href="/scheduling/song-tag-suggestions">` -
"Song Tags" becomes nav's one always-shown link for this feature.
Preserve the "something's pending" ambient nudge the removed link used to
carry (its own doc comment: "an actionable inbox item... reappears on its
own the moment a new song creates a suggestion") by rendering a small
count/badge next to the "Song Tags" link itself when
`navSuggestionsPending(ctx)` - reuse whatever this app's nav already does
for a similar per-link badge if a precedent exists (check first, e.g. any
existing `components.Badge` usage inside `navMenuContent`); a plain " (N)"
suffix is an acceptable fallback if there's no existing pattern to match.
`middleware.PopulateNavState`'s own computation doesn't need to change -
it already ORs both sources together.

### 10. Home page (`internal/views/home.templ`, `HomeController`)

- `home.go`: replace the `buildSongTagSuggestionsData`/
  `len(suggestions.Suggestions)` call (that function no longer exists)
  with a call to whatever `song_tags.go` now exposes for computing both
  suggestion groups, summed - `len(titleGroups) + len(themeGroups)` -
  which also fixes the existing undercount (theme suggestions were never
  counted before).
- `home.templ`: both the "N song tag suggestion(s) waiting for review"
  nudge (~line 56-61) and the Setup-checklist row (~line 118-123) change
  their link from `/scheduling/song-tag-suggestions` to
  `/scheduling/song-tags`; update their copy to reflect that suggestions
  are reviewed on the Song Tags page now, not a dedicated one.

### 11. Docs

- `docs/plans/shipped/song-theme-and-lyric-tag-suggestions-plan.md`:
  Derek/Claude to update directly (not part of this build) once this
  merge ships - its Phase 1 "UX" bullet and Phase 2 section 5 ("UX
  integration") both currently describe extending the now-deleted
  standalone page; they need a pointer to this doc + the new
  `SongTagSuggestionsPanel` location instead. Flagged here so it isn't
  missed, not assigned to the build itself. **Done 2026-09-15**, as part
  of splitting that doc's unbuilt Phase 2 into its own plan - see
  `docs/plans/not-started/song-lyric-tag-suggestions-plan.md`'s own "UX
  integration" section, which now describes this panel correctly instead
  of the deleted page; the original doc's stale Phase 1/Phase 2 text was
  left as-is for history, with a note pointing here.
- `README.md`: update the "Song Tag Suggestions" feature bullet (~line
  59) if it names the standalone page/URL.
- `docs/pco-api-risks.md`: no functional change to any PCO call pattern
  here (same `FetchActiveSongs`-fed computation, just relocated) - confirm
  its existing 2026-09-11 Song Tag Suggestions changelog entry doesn't
  need a correction, but no new entry expected.
- This doc: move to `docs/plans/shipped/` with a `**Status: shipped.**`
  line once built, per this repo's own convention.

## Verification (once built)

- `go build ./...` (empty output = success), `make test`.
- `templ generate` before both (`.templ` files change here).
- Visit `/scheduling/song-tags` locally: confirm the Suggestions panel
  shows real groups, "Select these songs" opens+fills the bulk-add form
  and checks the right cards' checkboxes (scattered anywhere in the page,
  not just visible ones), submitting actually tags them, and the panel
  updates afterward (group shrinks or disappears) via the OOB swap - no
  full page reload needed.
- Confirm Ignore removes a group from the panel without touching any
  song's existing tags or the card list.
- Confirm `/scheduling/song-tag-suggestions` is fully gone (404, not a
  broken link) and nothing in the app still links to it (`grep -r
  "song-tag-suggestions"` across `.templ`/`.go`/`.md` should only match
  the ignore route's new path and this plan/the lyrics plan's own
  historical references).
- Confirm the nav "Song Tags" link's pending indicator and the home
  page's nudge both still work and both point at `/scheduling/song-tags`.
