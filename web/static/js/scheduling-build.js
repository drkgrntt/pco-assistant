// scheduling-build.js - the combined Build a Service page (Songs + Team
// tabs): set-builder row popovers/badges (row menu, key-transition/
// transition-note/vocal-range/badge-overflow badges), quick-action rows,
// add/edit plan-item rows, song/person detail-card selection,
// jumpToTeamPosition, the popover-closing document click/keydown listeners,
// the Songs/Team tab switcher, the roster builder's hideable team-group
// cards, the set builder/roster builder's htmx-swap collapsible-state
// memory, the "find songs to add" candidate search filters, and the
// order-of-service pointer-based drag reordering. Split out of the former
// single scheduling.js - see scheduling-global.js's header for the same
// note. Calls a few functions defined in other split files
// (toggleCollapse from scheduling-collapsible.js; closeNavDropdowns/
// closeMobileMenu from scheduling-global.js, in the document click/keydown
// listeners below that also close this file's own row-menu/badge/detail-
// card popovers) - safe since every split file loads globally on every
// page (see layout.templ), same as the original single file.

// The set builder's per-row "more actions"/"replace" menus (song rows -
// see row-menu/row-menu-toggle in set_builder.templ, both the dot menu
// and the replace-song picker use this same wrapper class) - same
// open/closed idiom as toggleNavDropdown above, just without the
// edge-of-screen repositioning: the actions column is already the
// table's rightmost edge, so a plain right-anchored menu (see
// .row-menu-menu in app.css) never needs to flip sides. Closes every
// other open row-menu first (not just this one's siblings) so a row with
// more than one - dot menu and replace - never shows two popups at once.
function toggleRowMenu(button) {
  var wrapper = button.closest(".row-menu");
  var opening = !wrapper.classList.contains("open");
  closeRowMenus();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeRowMenus() {
  document.querySelectorAll(".row-menu.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".row-menu-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The set builder's per-row key-transition badge (see songBadgeSpans,
// set_builder.templ) - a short always-visible caption ("half-step from
// previous song") that click-to-expands into the full explanatory
// sentence, per docs/plans/shipped/in-place-set-warnings-plan.md's resolved
// decision to use a Disclosure rather than a hover-only title= tooltip.
// Same open/closed idiom as toggleRowMenu above, but kept as its own
// wrapper class rather than reusing .row-menu: this popup is an
// explanation, not an action menu, and shouldn't close (or be closed by)
// the row's own "more actions"/"replace" menus.
function toggleKeyTransitionBadge(button) {
  var wrapper = button.closest(".key-transition-badge");
  var opening = !wrapper.classList.contains("open");
  closeKeyTransitionBadges();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeKeyTransitionBadges() {
  document.querySelectorAll(".key-transition-badge.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".key-transition-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The set builder's per-row transition-note resurfacing prompt (see
// songBadgeSpans, set_builder.templ) - same open/close idiom as
// toggleKeyTransitionBadge/closeKeyTransitionBadges above, kept as its own
// wrapper class for the same reason: this popup is an explanation, not an
// action menu, and shouldn't close (or be closed by) the row's other
// menus/badges.
function toggleTransitionNoteBadge(button) {
  var wrapper = button.closest(".transition-note-badge");
  var opening = !wrapper.classList.contains("open");
  closeTransitionNoteBadges();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeTransitionNoteBadges() {
  document.querySelectorAll(".transition-note-badge.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".transition-note-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The set builder's per-row vocal-range mismatch/tip badge (see
// songBadgeSpans, set_builder.templ) - Phase 3 of docs/plans/in-progress/
// vocal-range-plan.md. Same open/close idiom as toggleKeyTransitionBadge/
// toggleTransitionNoteBadge above, kept as its own wrapper class for the
// same reason: this popup is an explanation, not an action menu, and
// shouldn't close (or be closed by) the row's other menus/badges.
function toggleVocalRangeBadge(button) {
  var wrapper = button.closest(".vocal-range-badge");
  var opening = !wrapper.classList.contains("open");
  closeVocalRangeBadges();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeVocalRangeBadges() {
  document.querySelectorAll(".vocal-range-badge.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".vocal-range-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// TagBadge's click-to-expand treatment for a long tag value (badge.templ,
// candidateBadgeExtras/song_tags.templ) - same open/close idiom as
// toggleKeyTransitionBadge/toggleTransitionNoteBadge/toggleVocalRangeBadge
// above, kept as its own wrapper class for the same reason: this popup is
// an explanation, not an action menu, and shouldn't close (or be closed
// by) the row's other menus/badges.
function toggleTagBadge(button) {
  var wrapper = button.closest(".tag-badge-wrap");
  var opening = !wrapper.classList.contains("open");
  closeTagBadges();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeTagBadges() {
  document.querySelectorAll(".tag-badge-wrap.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".tag-badge-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The set builder's per-row/per-candidate "+N more" badge overflow (see
// songBadgeSpans/songCandidateMeta, set_builder.templ) - collapses the
// lower-priority badges (roles, tags, resurfaced transition-note prompts)
// once a row's total badge count crosses badgeCollapseThreshold, per
// docs/issues.md's "busy row" feedback. Same open/close idiom as
// toggleRowMenu above (only one open across the page at a time - a plain
// dropdown, not several independently-open popovers).
function toggleBadgeOverflow(button) {
  var wrapper = button.closest(".badge-overflow");
  var opening = !wrapper.classList.contains("open");
  closeBadgeOverflows();
  if (opening) {
    wrapper.classList.add("open");
    button.setAttribute("aria-expanded", "true");
  }
}

function closeBadgeOverflows() {
  document.querySelectorAll(".badge-overflow.open").forEach(function (wrapper) {
    wrapper.classList.remove("open");
    var toggle = wrapper.querySelector(".badge-overflow-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The "Browse all songs" plain title list's per-row detail toggle
// (songBrowseRow, set_builder.templ) - purely the CSS open/close; the
// row's title button also carries its own hx-get/hx-trigger="click once"
// fetching the actual metadata into .song-browse-detail the first time a
// row opens (htmx handles that independently of this). Unlike
// toggleRowMenu/toggleKeyTransitionBadge above, this doesn't close
// sibling rows - see songBrowseRow's own doc comment for why several rows
// staying open at once is the point here, not a floating popover that'd
// need to avoid overlapping.
function toggleSongBrowseDetail(button) {
  var row = button.closest(".song-browse-row");
  var opening = !row.classList.contains("open");
  row.classList.toggle("open", opening);
  button.setAttribute("aria-expanded", opening ? "true" : "false");
}

// Reveals the hidden tag/relate/role form for one song row and hides its
// two siblings - each song gets one <tr class="quick-action-row"> "slot"
// per action, all sharing the same data-row-id as the song's own row (see
// set_builder.templ), so only one of the three is ever open at a time.
// Closes whichever row-menu triggered this too, same as picking a real nav
// link closes the nav dropdown.
//
// button's own data-row-id wins when present - the song detail card's
// Tag/Relate/Mark role buttons (song_detail_card.templ) carry it directly
// since they aren't nested inside the song's own <tr>, unlike the row
// menu's copies of these same buttons. Triggering from the card also
// scrolls the now-revealed row into view, since it lives in the table
// above rather than right next to the button that opened it.
function openQuickAction(button, action) {
  var rowID = button.dataset.rowId || button.closest("tr.song-row").dataset.rowId;
  closeRowMenus();
  var revealed = null;
  document.querySelectorAll('tr.quick-action-row[data-row-id="' + rowID + '"]').forEach(function (row) {
    row.hidden = row.dataset.action !== action;
    if (!row.hidden) {
      revealed = row;
    }
  });
  if (revealed && button.closest(".build-detail-card")) {
    revealed.scrollIntoView({ behavior: "smooth", block: "center" });
  }
}

// Toggles the Order of service table's trailing "+ Add item to plan" row
// (addPlanItemRows, quick_action.templ) between its plain button state and
// its inline form state - same expand-in-place idiom as openQuickAction
// just above, just for this one add-a-foreign-item action rather than a
// per-row menu, so there's only ever one row pair to toggle, not a
// data-row-id/data-action lookup.
function toggleAddPlanItemRow(open) {
  var triggerRow = document.getElementById("add-plan-item-trigger-row");
  var formRow = document.getElementById("add-plan-item-form-row");
  if (!triggerRow || !formRow) {
    return;
  }
  triggerRow.hidden = open;
  formRow.hidden = !open;
  if (open) {
    var titleInput = formRow.querySelector('[name="title"]');
    if (titleInput) {
      titleInput.focus();
    }
  }
}

// addPlanItemRows' item-type select - Planning Center silently drops
// whatever Length a header item is created with (confirmed live, see
// AddPlanItem's own doc comment), so hides and disables the length inputs
// for "Section header" rather than leaving up fields that quietly do
// nothing for that choice. Clears any typed value on the way in, so
// switching to Section header and submitting can't silently carry a
// length along that Planning Center is just going to drop anyway.
function toggleAddPlanItemLengthFields(select) {
  var isHeader = select.value === "header";
  ["add-plan-item-length-min-label", "add-plan-item-length-sec-label"].forEach(function (id) {
    var label = document.getElementById(id);
    if (!label) {
      return;
    }
    label.hidden = isHeader;
    var input = label.querySelector("input");
    if (input) {
      input.disabled = isHeader;
      if (isHeader) {
        input.value = "";
      }
    }
  });
}

// Clicking a song's row in the order of service (set_builder.templ) opens
// that song's detail card in the sidebar (song_detail_card.templ) - its
// metadata, notes, and every one of the row's own actions spelled out with
// the room a table cell doesn't have. Clicking the same row again, or its
// card's own Close button, closes it - a plain toggle, same idiom as
// toggleRowMenu. Ignores clicks on the row's own action cell (buttons,
// forms, the replace <select>), drag handle, and the key-transition/
// transition-note/vocal-range/badge-overflow badges - those already do
// something on their own; selecting the row on top of that would just be
// noise (and, for the drag handle, fire on every drag as an unwanted side
// effect).
function selectSongRow(row, event) {
  if (
    event.target.closest(".set-song-actions") ||
    event.target.closest(".drag-handle") ||
    event.target.closest(".key-transition-badge") ||
    event.target.closest(".transition-note-badge") ||
    event.target.closest(".vocal-range-badge") ||
    event.target.closest(".badge-overflow")
  ) {
    return;
  }
  var rowID = row.dataset.rowId;
  var alreadySelected = row.classList.contains("selected");
  closeSongDetail();
  if (alreadySelected) {
    return;
  }
  row.classList.add("selected");
  // Scoped to #build-detail-songs, not just .build-detail-card - a set_song
  // row id and a roster_assignment row id are independent sequences that
  // can collide, and closePersonDetail's card group shares the same class.
  var card = document.querySelector('#build-detail-songs .build-detail-card[data-row-id="' + rowID + '"]');
  if (card) {
    card.hidden = false;
  }
}

function closeSongDetail() {
  document.querySelectorAll("tr.song-row.selected").forEach(function (row) {
    row.classList.remove("selected");
  });
  document.querySelectorAll("#build-detail-songs .build-detail-card").forEach(function (card) {
    card.hidden = true;
  });
}

// The people equivalent of selectSongRow/closeSongDetail above - clicking
// a row in a position's assignment list OR candidate list
// (roster_builder.templ) opens that person's detail card
// (person_detail_card.templ: personDetailCard for an assigned row,
// candidateDetailCard for a candidate row - both share the same
// buildDetailCardShell, so this doesn't need to know which kind of row it
// was clicked from) in the sidebar. Ignores clicks on the row's own
// Remove/Add button, same reasoning as selectSongRow ignoring its actions
// cell.
function selectPersonRow(row, event) {
  if (event.target.closest("button")) {
    return;
  }
  var rowID = row.dataset.rowId;
  var alreadySelected = row.classList.contains("selected");
  closePersonDetail();
  if (alreadySelected) {
    return;
  }
  row.classList.add("selected");
  // Scoped to #build-detail-team - see selectSongRow's own comment on why
  // this can't just be .build-detail-card[data-row-id=...].
  var card = document.querySelector('#build-detail-team .build-detail-card[data-row-id="' + rowID + '"]');
  if (card) {
    card.hidden = false;
  }
}

function closePersonDetail() {
  document.querySelectorAll(".assignment-list li.selected, .candidate-list li.selected").forEach(function (row) {
    row.classList.remove("selected");
  });
  document.querySelectorAll("#build-detail-team .build-detail-card").forEach(function (card) {
    card.hidden = true;
  });
}

// jumpToTeamPosition is the "Team, at a glance" sidebar card's own
// navigation (TeamGlanceList, status_summary.templ) - clicking a position
// or a named person there switches to the Team tab, force-opens that
// exact position's own card (a filled position collapses by default - see
// positionGroupCard's own doc comment - so it needs opening even though
// it already exists in the DOM), and scrolls to it. el is the clicked
// button itself (data-position-id/data-row-id carry which position/row -
// this codebase's usual way of passing dynamic ids to a handler, see
// selectPersonRow/toggleCollapse below). Composes toggleBuildTab/
// toggleCollapse/selectPersonRow rather than reimplementing any of their
// logic - toggleCollapse in particular so the force-opened state
// persists in localStorage exactly like a manual click would.
function jumpToTeamPosition(el) {
  var positionID = el.dataset.positionId;
  var rowID = el.dataset.rowId;
  toggleBuildTab("team");
  var card = document.getElementById(positionID);
  if (!card) {
    return;
  }
  if (!card.classList.contains("open")) {
    var toggle = card.querySelector("h3 > .collapsible-toggle");
    if (toggle) {
      toggleCollapse(toggle);
    }
  }
  card.scrollIntoView({ behavior: "smooth", block: "center" });
  if (rowID) {
    var row = card.querySelector('.assignment-list li[data-row-id="' + rowID + '"]');
    if (row) {
      selectPersonRow(row, { target: row });
    }
  }
  // Clear any highlight already in progress (including this same card's
  // own, if it's clicked again mid-fade) before restarting it - removing
  // and re-adding the class with a reflow forced in between is what makes
  // the CSS animation (jump-highlight-fade, card.css) actually restart
  // rather than a no-op class toggle silently doing nothing.
  document.querySelectorAll(".jump-highlight").forEach(function (el) {
    el.classList.remove("jump-highlight");
  });
  void card.offsetWidth;
  card.classList.add("jump-highlight");
  card.addEventListener("animationend", function handler() {
    card.classList.remove("jump-highlight");
    card.removeEventListener("animationend", handler);
  });
}

document.addEventListener("click", function (event) {
  if (!event.target.closest(".nav-dropdown")) {
    closeNavDropdowns();
  }
  if (!event.target.closest(".row-menu")) {
    closeRowMenus();
  }
  if (!event.target.closest(".key-transition-badge")) {
    closeKeyTransitionBadges();
  }
  if (!event.target.closest(".transition-note-badge")) {
    closeTransitionNoteBadges();
  }
  if (!event.target.closest(".vocal-range-badge")) {
    closeVocalRangeBadges();
  }
  if (!event.target.closest(".tag-badge-wrap")) {
    closeTagBadges();
  }
  if (!event.target.closest(".badge-overflow")) {
    closeBadgeOverflows();
  }
  // Deliberately no "click outside closes the song/person detail card"
  // here (there used to be one) - it made the card on the Build page's
  // Songs tab, and the person card on its Team tab, close on almost any
  // stray click nearby, which read as broken rather than helpful. Both
  // cards already close via their own Close button (buildDetailCardShell
  // -> closeSongDetail()/closePersonDetail()), clicking the same row again
  // (selectSongRow/selectPersonRow's toggle), or Escape (the keydown
  // listener below) - that's enough ways to close one without this.
});

document.addEventListener("keydown", function (event) {
  if (event.key === "Escape") {
    closeNavDropdowns();
    closeRowMenus();
    closeKeyTransitionBadges();
    closeTransitionNoteBadges();
    closeVocalRangeBadges();
    closeTagBadges();
    closeBadgeOverflows();
    closeMobileMenu();
    closeSongDetail();
    closePersonDetail();
  }
});

// The combined Build a Service page's Songs/Team tabs (see BuildPlan,
// build.templ) - a plain client-side visibility toggle between the two
// panels, each of which is already its own fully self-contained builder
// (SetBuilderBody/RosterBuilderBody, with their own htmx swap targets).
// Switching tabs never touches either builder's markup or triggers a
// request - it just moves the "hidden" attribute and syncs aria-selected,
// same class-toggle-plus-aria idiom as toggleNavDropdown above.
function toggleBuildTab(tabName) {
  document.querySelectorAll(".build-tab").forEach(function (tab) {
    var active = tab.getAttribute("aria-controls") === "build-tab-" + tabName;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", active ? "true" : "false");
  });
  syncBuildTabVisibility(tabName);
  // Keeps the address bar (and anything copied from it) pointing at
  // whichever tab is actually open - see BuildPlanPage's ?tab= query param.
  // replaceState, not pushState: switching tabs is a view change, not a
  // new place to land on with the back button, same as htmx's own default
  // history behavior elsewhere on this page.
  var url = new URL(window.location.href);
  url.searchParams.set("tab", tabName);
  window.history.replaceState(null, "", url);
}

// syncBuildTabVisibility applies tabName's hidden/shown state to every
// tab-scoped element on the Build page - the panels, and the sidebar's
// glance/publish/detail-card groups. Split out of toggleBuildTab so it can
// also re-run after any htmx swap settles (see the htmx:afterSettle
// listener below), independent of an actual click on a tab button: the
// deferred tab's data is fetched lazily and can still be in flight when
// the user switches to it by hand (see pco-api-risks.md's mitigation #3 -
// it's usually the slower tab, which is exactly why it's deferred at
// all). That fetch's own out-of-band fragments (rosterPublishPanelLazyOOB
// etc., build.templ) only know whether this tab was still inactive when
// the *request* went out - if the response lands after the user already
// clicked over, applying that stale hidden state would silently undo what
// the click just set: hiding this tab's own publish/detail cards and
// showing its glance card, exactly backwards from what should be visible
// now that it's the open tab. Re-running this after every swap makes
// "whichever tab's button is currently aria-selected" the one source of
// truth for hidden state, no matter which of the two (the click, or the
// background fetch) actually finishes last.
function syncBuildTabVisibility(tabName) {
  document.querySelectorAll(".build-tab-panel").forEach(function (panel) {
    panel.hidden = panel.id !== "build-tab-" + tabName;
  });
  // The sidebar "at a glance" card (see build.templ) always shows whichever
  // tab just went hidden, never the one that's now open - no point glancing
  // at a summary of the tab you're already looking at.
  //
  // :not(.build-detail-card) matters here: every song/person detail card
  // (buildDetailCardShell, build_detail_card.templ) also carries the
  // "build-glance-card" class - purely to inherit its shared card-shell
  // CSS (see build-detail-card.css's own comment), not because it's one of
  // these tab-summary cards. Without this exclusion, this loop's `card.id`
  // read hits those cards too - they have no id (only data-row-id), so
  // cardTab comes out "" and "" === tabName is always false, which forced
  // every single one hidden=false regardless of tabName, undoing
  // selectSongRow/selectPersonRow's "only the clicked row's card is open"
  // state as soon as this ran (on every htmx:afterSettle, including the
  // very first one - the deferred tab's own initial "load once" fetch -
  // which is why every song/person looked selected from page load with no
  // click at all).
  document.querySelectorAll(".build-glance-card:not(.build-detail-card)").forEach(function (card) {
    var cardTab = card.id.replace("build-glance-", "");
    card.hidden = cardTab === tabName;
  });
  // The sidebar's publish card is the opposite: only the tab you're
  // actually ON has anything to publish from here, so it's the only one
  // shown (see SongPublishPanel/RosterPublishPanel, build.templ).
  document.querySelectorAll(".build-publish-card").forEach(function (card) {
    var cardTab = card.id.replace("build-publish-", "");
    card.hidden = cardTab !== tabName;
  });
  // The song/person detail card groups (see SongDetailCards/
  // PersonDetailCards) are scoped to their own tab the same way - only
  // the open tab's own group can have anything selected to show.
  document.querySelectorAll(".build-detail-group").forEach(function (group) {
    var groupTab = group.id.replace("build-detail-", "");
    group.hidden = groupTab !== tabName;
  });
}

// currentBuildTab reads back whichever tab is actually open right now, by
// finding the tab button toggleBuildTab last marked aria-selected - its
// own bookkeeping, read back rather than tracked in a second place. ""
// (no Build page tabs on this view) is a safe no-op for
// syncBuildTabVisibility's caller below.
function currentBuildTab() {
  var active = document.querySelector('.build-tab[aria-selected="true"]');
  if (!active) {
    return "";
  }
  return active.getAttribute("aria-controls").replace("build-tab-", "");
}

// Re-syncs Build tab visibility after every htmx swap settles, not just
// after an explicit toggleBuildTab click - see syncBuildTabVisibility's
// own doc comment for the race this closes. afterSettle (not afterSwap)
// so this runs once both the primary swap target and every out-of-band
// fragment in the same response have already landed - running any earlier
// would just get overwritten by whichever OOB fragment hadn't applied
// yet. A no-op on every page without Build tabs, since currentBuildTab()
// returns "" there.
document.addEventListener("htmx:afterSettle", function () {
  var tab = currentBuildTab();
  if (tab) {
    syncBuildTabVisibility(tab);
  }
});
// Hiding a team's whole card off the roster builder's Team tab
// (teamGroupCard, roster_builder.templ) - a separate, coarser control from
// collapsing (above): collapsing still shows the card, just shrunk to its
// heading; hiding removes it from the grid entirely, with a "show it
// again" chip left behind in #hidden-teams-bar. Same per-browser
// localStorage-only storage as collapse state (see teamGroupCard's own
// doc comment for why there's no server-side preference here) but one
// JSON array under a single key, not one key per team, since - unlike
// collapse/open, which only ever needs "is this one id open or not" -
// rebuilding the hidden-teams bar after a reload needs the *whole* list
// at once.
var hiddenTeamGroupsKey = "hidden-team-groups";

function readHiddenTeamGroupIDs() {
  try {
    var raw = localStorage.getItem(hiddenTeamGroupsKey);
    var ids = raw ? JSON.parse(raw) : [];
    return Array.isArray(ids) ? ids : [];
  } catch (e) {
    return [];
  }
}

function writeHiddenTeamGroupIDs(ids) {
  localStorage.setItem(hiddenTeamGroupsKey, JSON.stringify(ids));
}

// hideChipID/showTeamGroup below key off teamID (data-team-id), not the
// card's own hashed element id (TeamGroupElementID) - teamID is what's
// actually stored in localStorage (a raw PCO id, not a hash worth
// recomputing client-side just to compare), and what a hidden card's team
// is actually identified by once it's no longer in the DOM to look up.
function hideChipID(teamID) {
  return "hidden-team-chip-" + teamID;
}

// hideTeamGroup is teamGroupCard's own hide-icon-button handler: records
// this team's id as hidden, hides its card, and adds a "show it again"
// chip to #hidden-teams-bar - the only place that bar's chips get created
// from a live click (restoreHiddenTeamGroups below rebuilds the same
// chips from localStorage alone, e.g. after a reload, since the card that
// would otherwise supply data-team-name isn't necessarily still in the
// DOM to read from at that point either way - both paths build the chip
// the same way, from whatever team name they have on hand).
function hideTeamGroup(button) {
  var card = button.closest(".team-group");
  if (!card) {
    return;
  }
  var teamID = card.dataset.teamId;
  var teamName = card.dataset.teamName || teamID;

  var ids = readHiddenTeamGroupIDs();
  if (ids.indexOf(teamID) === -1) {
    ids.push(teamID);
    writeHiddenTeamGroupIDs(ids);
  }

  card.hidden = true;
  addHiddenTeamChip(teamID, teamName);
}

// showTeamGroup is a hidden-teams-bar chip's own click handler - the
// reverse of hideTeamGroup: drops the id from localStorage, un-hides the
// card if it's still on the page (it always is on the roster builder
// itself; harmless no-op if this ever rendered somewhere the card isn't),
// and removes its own chip.
function showTeamGroup(teamID) {
  var ids = readHiddenTeamGroupIDs().filter(function (id) {
    return id !== teamID;
  });
  writeHiddenTeamGroupIDs(ids);

  var card = document.querySelector('.team-group[data-team-id="' + CSS.escape(teamID) + '"]');
  if (card) {
    card.hidden = false;
  }

  var chip = document.getElementById(hideChipID(teamID));
  if (chip) {
    chip.remove();
  }

  updateHiddenTeamsBarVisibility();
}

function addHiddenTeamChip(teamID, teamName) {
  var bar = document.getElementById("hidden-teams-bar");
  if (!bar || document.getElementById(hideChipID(teamID))) {
    updateHiddenTeamsBarVisibility();
    return;
  }

  var chip = document.createElement("button");
  chip.type = "button";
  chip.id = hideChipID(teamID);
  chip.className = "hidden-team-chip";
  chip.title = "Show " + teamName + "'s card again";
  chip.textContent = "+ " + teamName;
  chip.onclick = function () {
    showTeamGroup(teamID);
  };
  bar.appendChild(chip);
  updateHiddenTeamsBarVisibility();
}

function updateHiddenTeamsBarVisibility() {
  var bar = document.getElementById("hidden-teams-bar");
  if (bar) {
    bar.hidden = bar.children.length === 0;
  }
}

// restoreHiddenTeamGroups is hideTeamGroup's read-back half, the hidden-
// team equivalent of applyStoredCollapsibleState - run on every page load
// and, unlike that collapse-state restore, also after every htmx swap
// (not just DOMContentLoaded): the roster builder's Publish/Discard
// actions re-render the *whole* #roster-builder-body (see the
// openCollapsibleIDs comment above), which recreates every .team-group
// card from scratch and would otherwise silently un-hide a previously
// hidden team on the very next mutation. Harmless to also run after
// swaps that touch no .team-group at all (the Songs tab, say) - it just
// finds nothing to do. Rebuilds #hidden-teams-bar's chips from scratch
// each time rather than diffing, since a full-body swap is exactly the
// case where any previously-live chip elements are gone anyway.
function restoreHiddenTeamGroups() {
  var bar = document.getElementById("hidden-teams-bar");
  if (!bar) {
    return;
  }
  bar.innerHTML = "";

  readHiddenTeamGroupIDs().forEach(function (teamID) {
    var card = document.querySelector('.team-group[data-team-id="' + CSS.escape(teamID) + '"]');
    if (!card) {
      return;
    }
    card.hidden = true;
    addHiddenTeamChip(teamID, card.dataset.teamName || teamID);
  });
}
document.addEventListener("DOMContentLoaded", restoreHiddenTeamGroups);
document.addEventListener("htmx:afterSwap", restoreHiddenTeamGroups);

// Most mutations on the set builder re-render the whole #set-builder-body
// (see the htmx attributes throughout set_builder.templ), which would
// otherwise snap every .collapsible back to its default open/closed state
// on every single add/remove/publish click. The roster builder mostly
// avoids that same full-body reload - its own mutations (roster_builder.templ)
// instead swap just one #position-group-<id> card at a time (falling back
// to a full #roster-builder-body swap only for Publish/Discard, which can
// touch every position at once) - but that narrower target still needs the
// same memory trick, since a position-group card can itself contain a
// nested .collapsible (its own "+ Add eligible person" panel) whose open
// state would otherwise reset on every add/remove for that position.
// Recorded right before whichever target - full body or single card - is
// about to swap, and reapplied right after - scoped by id so one target's
// remembered state is never applied to a different swap that happens to
// follow it.
var openCollapsibleIDs = null;

function isCollapsibleMemoryTarget(target) {
  return !!target && (target.id === "set-builder-body" || target.id === "roster-builder-body" || target.id.indexOf("position-group-") === 0);
}

document.addEventListener("htmx:beforeSwap", function (event) {
  var target = event.detail.target;
  if (!isCollapsibleMemoryTarget(target)) {
    return;
  }

  openCollapsibleIDs = [];
  // Include the target itself, not just its descendants - a
  // #position-group-<id> card is itself the .collapsible.open element
  // being swapped (unlike #set-builder-body/#roster-builder-body, which
  // only ever *contain* collapsibles, never are one).
  if (target.matches(".collapsible.open[id]")) {
    openCollapsibleIDs.push(target.id);
  }
  target.querySelectorAll(".collapsible.open[id]").forEach(function (el) {
    openCollapsibleIDs.push(el.id);
  });
});

document.addEventListener("htmx:afterSwap", function () {
  if (!openCollapsibleIDs) {
    return;
  }

  var ids = openCollapsibleIDs;
  openCollapsibleIDs = null;
  ids.forEach(function (id) {
    var section = document.getElementById(id);
    if (!section) {
      return;
    }
    section.classList.add("open");
    var toggle = section.querySelector(".collapsible-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "true");
    }
  });
});

// applyStoredCollapsibleState (scheduling-collapsible.js) is deliberately
// DOMContentLoaded-only by its own doc comment's reasoning: every other
// swap target already carries its own live open/closed state forward via
// openCollapsibleIDs above, so re-reading localStorage at swap time would
// just recompute the same thing. The "Team, at a glance" card's per-team
// sections (TeamGlanceElementID, status_summary.templ) are the one real
// exception to that: whenever the Songs tab is the one initially active,
// this card starts out as a bare loading skeleton (build.templ) with no
// .collapsible elements in it at all, and its real content - the very
// first time those .collapsible elements exist in the DOM - only arrives
// later via an hx-swap-oob="true" lazy-load response (rosterGlanceLazyOOB).
// openCollapsibleIDs' live-DOM-carry-forward can't help there (there's no
// prior live state on a skeleton to carry), and DOMContentLoaded's own
// restore already ran before this content existed - so a person's stored
// collapse choice from an earlier visit never got applied until their
// *next* full reload happened to land on the Team tab directly. Re-running
// the same stored-state read-back after every OOB swap fixes this:
// harmless/idempotent on every later mutation-triggered refresh (the live
// DOM there already matches localStorage, since every explicit
// toggleCollapse click writes to it synchronously) and exactly the fix
// needed for this one first-render case. Wrapped in a closure rather than
// passed by reference directly (`..., applyStoredCollapsibleState)`) since
// schedulingJSFiles loads this file before scheduling-collapsible.js -  a
// bare reference here would resolve before that function exists; deferring
// the lookup into a callback only actually invoked later avoids that
// ordering hazard.
document.addEventListener("htmx:oobAfterSwap", function () {
  applyStoredCollapsibleState();
});

// The set builder's "find songs to add" list (see set_builder.templ) is
// search-driven, not client-side filtered: #candidate-search's own
// hx-get/hx-trigger (set_builder.templ) fires the actual request to
// BuildCandidatesSearch as the user types, debounced. What's left here is
// just the role-filter buttons' toggle behavior and the "open this section
// the moment it's actually used" nicety - both used to live inside
// filterCandidates, back when filtering ran entirely in the browser against
// an already-rendered list.

// openCandidatesSection opens the "Browse all songs" collapsible the moment
// a search term is typed or a role filter is clicked - called from
// #candidate-search's oninput and from toggleRoleFilter below, since either
// one can be the first interaction. The list itself defaults open already
// (see collapsibleSection's own param in set_builder.templ), but someone
// who's explicitly collapsed it shouldn't have to reopen it by hand just to
// see search results land.
function openCandidatesSection() {
  var candidates = document.getElementById("find-songs-candidates");
  if (candidates && !candidates.classList.contains("open")) {
    candidates.classList.add("open");
    var toggle = candidates.querySelector(".collapsible-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "true");
    }
  }
}

// Role filter buttons behave like a toggle: clicking the active one turns
// it back off, clicking another switches to it. Only one is active at a
// time since a song's roles are shown, not exclusively filtered by an
// AND/OR combination. The active role has no form control of its own, so
// it's mirrored into the hidden #candidate-role-filter input (pulled into
// the search request via hx-include - see set_builder.templ) before
// re-triggering that same request via a synthetic "search" event, the way
// clicking Search normally would.
function toggleRoleFilter(el) {
  var wasActive = el.classList.contains("active");
  document.querySelectorAll(".role-filter").forEach(function (btn) {
    btn.classList.remove("active");
  });
  var roleInput = document.getElementById("candidate-role-filter");
  if (!wasActive) {
    el.classList.add("active");
    if (roleInput) {
      roleInput.value = el.dataset.role;
    }
  } else if (roleInput) {
    roleInput.value = "";
  }

  openCandidatesSection();

  var searchInput = document.getElementById("candidate-search");
  if (searchInput && window.htmx) {
    htmx.trigger(searchInput, "search");
  }
}

// toggleContextFilter is toggleRoleFilter's literal copy for context
// filter chips (set_builder.templ) - targets .context-filter/
// #candidate-context-filter instead of .role-filter/#candidate-role-filter
// so the two stay independent toggle groups (a song can be filtered by
// role and context at once), not one shared "clear everything" group.
function toggleContextFilter(el) {
  var wasActive = el.classList.contains("active");
  document.querySelectorAll(".context-filter").forEach(function (btn) {
    btn.classList.remove("active");
  });
  var contextInput = document.getElementById("candidate-context-filter");
  if (!wasActive) {
    el.classList.add("active");
    if (contextInput) {
      contextInput.value = el.dataset.context;
    }
  } else if (contextInput) {
    contextInput.value = "";
  }

  openCandidatesSection();

  var searchInput = document.getElementById("candidate-search");
  if (searchInput && window.htmx) {
    htmx.trigger(searchInput, "search");
  }
}

// applyTagFilter is the "browse by tag" chips' click handler
// (set_builder.templ). Unlike toggleRoleFilter above, a tag chip isn't a
// persistent exclusive toggle with a hidden filter input of its own - it's
// a shortcut that types the clicked value straight into the existing
// search box, reusing the search endpoint's own already-correct
// tag-substring matching (candidateMatchesSearch, sets_builder.go) rather
// than adding a second filtering mechanism.
function applyTagFilter(el) {
  var searchInput = document.getElementById("candidate-search");
  if (!searchInput) {
    return;
  }
  searchInput.value = el.dataset.value;

  openCandidatesSection();

  if (window.htmx) {
    htmx.trigger(searchInput, "search");
  }
}

// clearCandidateSearch is the "Browse all songs" search box's clear button
// (set_builder.templ) - resets the typed search text and any active
// role/context filter (mirroring toggleRoleFilter/toggleContextFilter's own
// "turn it back off" branch) back to "", then re-triggers the same search
// request, landing back on CandidateSearchResults' empty-query/role/context
// "browse titles" state. Without this, clicking a role filter, context
// filter, or tag chip (which just type into #candidate-search / set a
// hidden filter input) left no way back to that list short of manually
// erasing the search box - not obvious, and didn't clear either filter's
// own "active" button styling either.
function clearCandidateSearch() {
  var searchInput = document.getElementById("candidate-search");
  if (!searchInput) {
    return;
  }
  searchInput.value = "";

  var roleInput = document.getElementById("candidate-role-filter");
  if (roleInput) {
    roleInput.value = "";
  }
  document.querySelectorAll(".role-filter").forEach(function (btn) {
    btn.classList.remove("active");
  });

  var contextInput = document.getElementById("candidate-context-filter");
  if (contextInput) {
    contextInput.value = "";
  }
  document.querySelectorAll(".context-filter").forEach(function (btn) {
    btn.classList.remove("active");
  });

  if (window.htmx) {
    htmx.trigger(searchInput, "search");
  }
}
// Order-of-service drag reordering (set builder). Built on Pointer Events
// rather than the HTML5 Drag and Drop API on purpose: HTML5 DnD doesn't
// reliably fire from touchscreens without extra plumbing, while pointer
// events cover mouse, touch, and pen through one code path. Dragging is
// scoped to each row's .drag-handle (touch-action: none in app.css keeps a
// touch drag on the handle from also scrolling the page), not the whole
// row, so tapping Remove/Replace elsewhere in the row still works.
//
// The dragged <tr> is physically moved through the live DOM as the pointer
// crosses other rows' midpoints (docs/issues.md: "I'd like to see the
// whole row to drag and take the spot, not just a line") rather than
// staying put with a separate insertion-point indicator drawn elsewhere.
// Its own hidden quick-action-row sibling(s) (set_builder.templ) are
// deliberately left behind at the old position rather than moved along
// with it - they render nothing while hidden, openQuickAction (above)
// finds them by data-row-id via a document-wide query rather than DOM
// adjacency, and the whole tbody gets replaced by the server's own
// re-render on drop anyway, so nothing depends on where they sit mid-drag.
//
// Every row in the table - song or foreign, section header or plain item -
// carries data-token identifying itself as a drop target ("row:<set_song
// row id>" or "item:<pco_item_id>"). A song can appear more than once in
// the set now (see the "duplicate" action), so rows are identified by
// their own row id, not by song id. Dropping always means "place after the
// row the pointer ends up below the midpoint of," so the dragged song
// adopts that row's token as its ?after= value; dropping above every row
// sends "" (top of service). See RepositionSetSong/parseAfterToken in the
// Go controller for how that token turns into an anchor + position.
(function () {
  var dragging = null;
  var startAfterToken = "";
  var startNextSibling = null;

  function rowToken(row) {
    return (row && row.dataset.token) || "";
  }

  // The nearest actual drop-target row before this one - not just
  // previousElementSibling, which can land on a hidden, token-less
  // <tr class="quick-action-row"> (each song row has up to three of these
  // as its own siblings - see set_builder.templ) rather than the real
  // previous song/header row.
  function previousTargetRow(row) {
    var sibling = row.previousElementSibling;
    while (sibling && !sibling.dataset.token) {
      sibling = sibling.previousElementSibling;
    }
    return sibling;
  }

  // Excludes draggingRow - now that it's physically reordered through the
  // list as it moves (see onPointerMove), it's one of tbody's own
  // tr[data-token] rows and would otherwise sometimes match its own
  // current position instead of a genuinely different row.
  function findDropTargetRow(tbody, clientY, draggingRow) {
    var rows = tbody.querySelectorAll("tr[data-token]");
    var target = null;
    for (var i = 0; i < rows.length; i++) {
      if (rows[i] === draggingRow) {
        continue;
      }
      var rect = rows[i].getBoundingClientRect();
      if (clientY >= rect.top + rect.height / 2) {
        target = rows[i];
      }
    }
    return target;
  }

  function onPointerMove(event) {
    if (!dragging) {
      return;
    }
    var tbody = dragging.closest("tbody");
    var target = findDropTargetRow(tbody, event.clientY, dragging);
    if (target) {
      if (target.nextElementSibling !== dragging) {
        target.insertAdjacentElement("afterend", dragging);
      }
      return;
    }
    // No target row means the pointer is above every row's midpoint - i.e.
    // dropping at the very top of service.
    var firstRow = tbody.querySelector("tr[data-token]");
    if (firstRow && firstRow !== dragging && tbody.firstElementChild !== dragging) {
      tbody.insertBefore(dragging, firstRow);
    }
  }

  // Puts the dragged row back exactly where it started - used when a drag
  // is cancelled (e.g. a touch gesture interrupted by a scroll) instead of
  // dropped, so it doesn't visually stay wherever it last got dragged to
  // with no matching server-side reposition.
  function restoreStartPosition() {
    if (!dragging) {
      return;
    }
    var tbody = dragging.closest("tbody");
    if (tbody) {
      tbody.insertBefore(dragging, startNextSibling);
    }
  }

  function endDrag() {
    document.removeEventListener("pointermove", onPointerMove);
    document.removeEventListener("pointerup", onPointerUp);
    document.removeEventListener("pointercancel", onPointerCancel);
    if (dragging) {
      dragging.classList.remove("dragging");
    }
    dragging = null;
    startNextSibling = null;
  }

  function onPointerCancel() {
    restoreStartPosition();
    endDrag();
  }

  function onPointerUp() {
    if (!dragging) {
      return;
    }
    var draggedRow = dragging;
    // The row's own live DOM position already *is* the drop target, since
    // it's been physically sitting there since the last onPointerMove.
    var after = rowToken(previousTargetRow(draggedRow));
    var unchanged = after === startAfterToken;
    endDrag();

    var rowID = draggedRow.dataset.rowId;
    var url = draggedRow.dataset.repositionUrl;
    if (!rowID || !url || unchanged) {
      return;
    }

    htmx.ajax("POST", url, {
      target: "#set-builder-body",
      swap: "outerHTML",
      values: { after: after },
    });
  }

  document.addEventListener("pointerdown", function (event) {
    var handle = event.target.closest(".drag-handle");
    if (!handle) {
      return;
    }
    var row = handle.closest("tr[data-row-id]");
    if (!row) {
      return;
    }

    event.preventDefault();
    dragging = row;
    startAfterToken = rowToken(previousTargetRow(row));
    startNextSibling = row.nextElementSibling;
    row.classList.add("dragging");

    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", onPointerUp);
    document.addEventListener("pointercancel", onPointerCancel);
  });
})();
