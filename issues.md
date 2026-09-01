- ~~Can we make sure the first letter in the key field is always uppercase? no need to validate, just make it happen.~~
  - **Done**, branch `issue-song-list-cleanup`. Server-side `uppercaseFirst` helper applied before saving in `UpdateSetSongKey`.

- ~~on the plan page, the "replace with" select takes up a lot of space. Can we make it take up less?~~
  - **Done**, branch `issue-song-list-cleanup`. Narrowed and made it the shrinking flex element with ellipsis overflow, in `builder-rows.css`.

- ~~on the plan page, can we persist via local or session storage the accordion state between reloads so someone who closes what they dont' need doesn't continue to have to close them?~~
  - **Done**, branch `issue-client-js-polish`. `scheduling.js` now writes open/closed state per section id to localStorage on toggle and reapplies it once on load; confirmed no conflict with `filterCandidates()`/`toggleRoleFilter()`'s force-open behavior.

- ~~in the plan page, whenever the viewport gets narrower, the items in teh songs start to wrap ,starting with the dot menu. I'd like to prioritize that dot menu staying unwrapped and on the right side.~~
  - **Done**, branch `issue-song-list-cleanup`. Dot menu pinned with `flex-shrink:0; order:1`; replace-form shrinks/ellipsizes first instead.

- ~~in the plan page, in our song list, the right side of each item feels like a cluster of a bunch of stuff. I'd like it to feel a little more organized. Could we start by putting the metadata underneath the title like in the planned song items?~~
  - **Done**, branch `issue-song-list-cleanup`. CSS-only fix in `list-item.css` (title span forced to its own line via `flex: 1 1 100%`), so every existing `ListItem` call site kept working unmodified.

- ~~in the plan page, I don't think we need the tags for the items tha tcome from planning center, such as "item" or "song".~~
  - **Done**, branch `issue-song-list-cleanup`. `row.ItemType` span removed from the foreign-row Tags cell.

- ~~in the plan page, can we add the metadata and tags to the suggested song items as well, such as the "could use one more sing" list.~~
  - **Done**, branch `issue-song-list-cleanup`. `SuggestionView` now embeds `CandidateSongView`; "Recommended next" and "Could use one more sing" share the same metadata/badge rendering as the candidate list via a new shared `songCandidateMeta` component. Minor flagged nit: in "Could use one more sing" the `NewSongLabel` badge and the suggestion's own relationship text are now both shown and slightly redundant - cosmetic only, left as-is pending review.

- ~~in the run sheet, can we separate teams similar to how we do in the plan page?~~
  - **Done**, branch `issue-run-sheet-teams`. Run sheet now calls the same `groupPositionsByTeam` helper the roster builder uses and nests positions under a per-team heading.

- ~~in the run sheet, can we exclude anything unassigned? I don't think unassigned needs to be shown in that context~~
  - **Done**, branch `issue-run-sheet-teams` (same change as above). Positions with nobody assigned are no longer rendered, with fallback messaging when a whole team or the whole roster has no assignments yet.

- ~~in the plan page team tab, it looks like we're reloading the entire page when we add a person or change something. Can we limit that so that it's not so heavy to load it? I've run into the rate limit a couple of times in planning and I think it's mostly due to this as the rate limit itself isn't unreasonable at all.~~
  - **Done**, branch `issue-roster-partial-refresh`. Assignment/preference mutations now render just the touched position-group card plus small OOB fragments (status/publish/glance), each recomputing candidates/warnings for one group instead of the whole roster (fewer PCO calls per action); relationship-only changes touch even less (`#roster-top` only). Publish/Discard still do a full reload by design - both can change every position at once, so there's no single group to scope to.
  - Two tradeoffs flagged by the build worth a look before merge: the "Recommended next" suggestions panel doesn't refresh on a scoped mutation (can show a stale entry mid-session, but duplicate-assignment is already rejected server-side so a stale click just fails safely); a `BodyParser` failure on the two busiest handlers would render an empty stub card into a real card's slot until the next mutation (cosmetic, self-correcting, not data-corrupting).

- ~~What even is "add eligible person"?~~
  - **Done**, branch `issue-roster-partial-refresh` (same branch as above). Added inline muted-text copy in the panel clarifying "eligible" means PCO already clears this person for the position (not suggested/preferred) and that it only adds them to this one position.

- When clicking a link (particularly clicking into a plan) can we show the loader until the page comes up (which probably requires nothing as it's not loading by default) to make the mobile experience indicate that it's loading a little better?
  - Eval: Small-medium. There's already a global `#htmx-progress-bar` (`layout.templ:35,85`) but plan links are plain `<a href>` full-page navigations (`build.templ:115`), not htmx-driven, so that indicator never fires for them - only the browser chrome's own (often invisible on mobile) loading affordance does. Two options: add `hx-boost` to the plan-list links so they go through htmx and get the existing progress bar for free (bigger behavior change, check other boosted-link side effects app-wide), or a lightweight `pagehide`/click-listener JS that shows the progress bar (or a simple spinner) on any same-origin navigating click, no htmx involved. Lean toward the second - smaller blast radius.

- What even is "add eligible person"?
  - Eval: Not a code task as written - it's a UX-clarity question about `roster_builder.templ:280`'s "+ Add eligible person" quick-add panel (per-position, lets you add someone who's eligible for that position - per whatever eligibility means there, check `rosters_builder.go` candidate logic around line 554-574 - but not currently suggested/assigned). Worth asking the user directly what's confusing (the word "eligible", or that it's unclear this is per-position vs. a general add) before guessing at a copy/tooltip fix.

- ~~I think we shoudl extend the free trial from 14 days to 30 days.~~ That'll need to be updated in the landing page and anywhere else, but given the weekly cadence of church services and the nature of this app where "the more you use it, the more useful it becomes", I think it makes sense to allow them a free month.
  - **Done**, branch `issue-trial-30-days`. New migration `00031` changes the `trial_ends_at` default to 30 days (existing mid-trial orgs not backfilled, per the decision above); landing page and README copy updated everywhere "14-day"/"14 days" appeared.

- ~~Is it worth adding verbiage in the marketing copy about how you can try it risk free because you are still using PCO, so if you don't like it, worst case, you didn't have to move any data, you still have it in your original source.~~
  - **Done**, branch `issue-trial-30-days` (same branch as above). Added a line next to the existing `.landing-reassurance` copy: "Your schedule stays live in Planning Center the whole time, so there's nothing to migrate back if you stop."

- ~~Do we have any sense of an archive or history? I could see something like that being useful, maybe with less features. Like, it's probably not worth backfilling old PCO services with our data, but being able to see them on our end, and especially being able to see our backlog of data.~~
  - **Done (past-plans list scope; analytics dashboard deferred)**, branch `issue-plan-archive-and-notes`. User chose the browsable-past-plans scope over the analytics dashboard - that dashboard idea has been written up in `ideas.md` instead of built. New Upcoming/Past tab in the plan picker; `Filter: "past"` confirmed live against the dev PCO org; past plans render existing local sets/rosters fully read-only (server-computed, not client-spoofable).

- ~~Worth adding a global service note? I'm not sure if it makes sense to integrate it with anything PCO has or not, but something that can show up at a glance in the services list on our end so that it's easier to pinpoint specific services based on something.~~
  - **Done**, branch `issue-plan-archive-and-notes` (same branch as above). New dedicated `plan_notes` table (migration `00032`, org-scoped, independent of whether a draft exists) instead of hanging it off `sets`; inline note field in the plan list on both tabs; search now matches note text too.
