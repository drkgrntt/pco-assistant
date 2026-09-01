- ~~on the plan page, the "replace with" select takes up a lot of space. Can we make it take up less?~~
  - **Done**, branch `issue-song-list-cleanup`. Narrowed and made it the shrinking flex element with ellipsis overflow, in `builder-rows.css`.

- ~~in the plan page, whenever the viewport gets narrower, the items in teh songs start to wrap ,starting with the dot menu. I'd like to prioritize that dot menu staying unwrapped and on the right side.~~
  - **Done**, branch `issue-song-list-cleanup`. Dot menu pinned with `flex-shrink:0; order:1`; replace-form shrinks/ellipsizes first instead.

- ~~in the plan page, in our song list, the right side of each item feels like a cluster of a bunch of stuff. I'd like it to feel a little more organized. Could we start by putting the metadata underneath the title like in the planned song items?~~
  - **Done**, branch `issue-song-list-cleanup`. CSS-only fix in `list-item.css` (title span forced to its own line via `flex: 1 1 100%`), so every existing `ListItem` call site kept working unmodified.

- ~~What even is "add eligible person"?~~
  - **Done**, branch `issue-roster-partial-refresh` (same branch as above). Added inline muted-text copy in the panel clarifying "eligible" means PCO already clears this person for the position (not suggested/preferred) and that it only adds them to this one position.

- ~~When clicking a link (particularly clicking into a plan) can we show the loader until the page comes up (which probably requires nothing as it's not loading by default) to make the mobile experience indicate that it's loading a little better?~~
  - **Done**, branch `issue-client-js-polish`. Same-origin plain-link clicks now trigger the existing `#htmx-progress-bar`.

- ~~What even is "add eligible person"?~~ (duplicate of the same question above - see that entry, answered on branch `issue-roster-partial-refresh`)

- ~~I think we shoudl extend the free trial from 14 days to 30 days.~~ That'll need to be updated in the landing page and anywhere else, but given the weekly cadence of church services and the nature of this app where "the more you use it, the more useful it becomes", I think it makes sense to allow them a free month.
  - **Done**, branch `issue-trial-30-days`. New migration `00031` changes the `trial_ends_at` default to 30 days (existing mid-trial orgs not backfilled, per the decision above); landing page and README copy updated everywhere "14-day"/"14 days" appeared.

- ~~Do we have any sense of an archive or history? I could see something like that being useful, maybe with less features. Like, it's probably not worth backfilling old PCO services with our data, but being able to see them on our end, and especially being able to see our backlog of data.~~
  - **Done (past-plans list scope; analytics dashboard deferred)**, branch `issue-plan-archive-and-notes`. User chose the browsable-past-plans scope over the analytics dashboard - that dashboard idea has been written up in `ideas.md` instead of built. New Upcoming/Past tab in the plan picker; `Filter: "past"` confirmed live against the dev PCO org; past plans render existing local sets/rosters fully read-only (server-computed, not client-spoofable).

- ~~Worth adding a global service note? I'm not sure if it makes sense to integrate it with anything PCO has or not, but something that can show up at a glance in the services list on our end so that it's easier to pinpoint specific services based on something.~~
  - **Done**, branch `issue-plan-archive-and-notes` (same branch as above). New dedicated `plan_notes` table (migration `00032`, org-scoped, independent of whether a draft exists) instead of hanging it off `sets`; inline note field in the plan list on both tabs; search now matches note text too.

- Follow up issues (all done, branches merged into `staging`):
    - ~~When I open the plan screen, every song gets selected and gets a card in the sidebar. I don't want that. I don't want any songs selected by default.~~
      - **Done**, branch `issue-followup-sidebar-cards`. Root cause found and live-verified: `syncBuildTabVisibility` (scheduling.js) looped over every `.build-glance-card` to toggle tab visibility, but per-item detail cards share that class purely for CSS (no `id`, only `data-row-id`) - the loop's `card.id`-based check silently matched them too and always evaluated false, unconditionally un-hiding every song/person card on the deferred-tab load. Fixed by scoping the selector to `.build-glance-card:not(.build-detail-card)`.
    - ~~Same on teh team page, every person gets selected and gets a card by default. I don't want that to happen.~~
      - **Done**, branch `issue-followup-sidebar-cards` (same fix as above - one shared root cause covered both tabs).
    - ~~When I reduce the screen width, the first thing to wrap is still the 3 dot menu. I want it to be prioritized to not wrap, at least not by itself.~~
      - **Done**, branch `issue-followup-list-layout`. Restructured so duplicate/remove/replace-form live in their own wrapping sub-container, with the dot-menu as a sibling outside it in a `flex-wrap:nowrap` outer row - spec-guaranteed to never wrap, not dependent on `order`/`flex-shrink` within one wrapping row (which is what the earlier fix relied on and why it wasn't enough).
    - ~~On the song list below, I didn't want to wrap all the data underneath, just hte meter, bpm, etc metadata. Tags and the plus shoudl still be on the rihgt side.~~
      - **Done**, branch `issue-followup-list-layout`. `ListItem` title now shares line 1 with trailing badges/buttons (right-aligned); only `ArrangementInfo` gets its own distinguishing class forcing it to a full-width line below.
    - ~~Same with team, I didn't want the plus and the frequency wrapped to go underneath, I still want those on the right side.~~
      - **Done**, branch `issue-followup-list-layout` (same `ListItem` fix as above).
    - ~~I don't want the plan note form to appear on the list page, just the text itself. I want that to be in the plan page.~~
      - **Done**, branch `issue-followup-plan-notes-and-tabs`. Plan list now shows plain read-only note text; the editable form moved onto the plan builder page itself, above the Songs/Team tabs, gated off (shown as text) for read-only past plans.
    - ~~I don't like the way the replace button looks in teh sidebar. Given that the select and the button are inline, maybe make the button as wide as it needs to be and have hte select take up the remainder of the space.~~
      - **Done**, branch `issue-followup-sidebar-cards`. Added a more-specific `.build-detail-actions .replace-form select { max-width: none }` rule so it grows to fill the sidebar's available width, leaving the table row's own 8rem cap untouched.
    - ~~Add eligible person still looks like an accordion that won't close.~~
      - **Done**, branch `issue-followup-list-layout`. Real bug found and live-verified: it's a nested accordion (quick-add panel inside a position-group card), and `collapsible.css` used descendant selectors that matched *any* open ancestor, not just the nearest one - so whenever the outer position card was open (the normal case), the inner panel rendered as expanded regardless of its own state. Fixed with child combinators so each accordion only responds to its own immediate state.
    - ~~How do I view past plans? Can we add a link to that somewhere? Probably just on the plan list page, not the nav.~~
      - **Done**, branch `issue-followup-plan-notes-and-tabs`. Live-verified the tab bar already worked correctly - the real problem was that it visually blended in with the unrelated Songs/Team tab switcher. Restyled as a distinct filled segmented control with fuller labels, still on the plan list page.
