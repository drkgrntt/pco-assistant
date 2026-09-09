// Applies the saved theme preference (light/dark) as early as possible -
// this file loads in <head>, before <body> parses, so this runs before the
// page paints and avoids a flash of the wrong theme. "system" (the
// default, nothing saved) needs no attribute at all: tokens.css's plain
// :root/@media rules already handle it.
(function () {
  var saved = localStorage.getItem("theme");
  if (saved === "light" || saved === "dark") {
    document.documentElement.setAttribute("data-theme", saved);
  }
})();

// Same flash-prevention trick as the theme preference above, for the
// early-preview banner (layout.templ): if it's already been dismissed,
// mark <html> before <body> parses so app.css's .banner-dismissed rule
// hides it immediately, rather than showing it for a moment and then
// yanking it away once this file's dismissSiteBanner() runs.
//
// sessionStorage, not localStorage: the warning it's attached to needs to
// stay apparent until it's genuinely no longer true, not something a
// viewer can silence for good on one visit and then never see again -
// closing it just keeps the current visit clutter-free, and it's back
// next time they open the app.
(function () {
  if (sessionStorage.getItem("bannerDismissed") === "true") {
    document.documentElement.classList.add("banner-dismissed");
  }
})();

// The theme toggle in the header (see layout.templ) is a simple two-state
// switch, not a three-way system/light/dark picker - it starts wherever
// the system preference currently resolves to (no saved override yet) and
// flips to an explicit choice from there. Which icon shows is handled
// entirely by CSS (app.css, keyed off the same data-theme attribute/media
// query as the rest of the palette), not JS, so it can never drift out of
// sync with the actual applied theme.
function currentEffectiveTheme() {
  var saved = localStorage.getItem("theme");
  if (saved === "light" || saved === "dark") {
    return saved;
  }
  return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function toggleTheme() {
  var next = currentEffectiveTheme() === "dark" ? "light" : "dark";
  document.documentElement.setAttribute("data-theme", next);
  localStorage.setItem("theme", next);
}

// Dismisses the early-preview banner (layout.templ) for the rest of this
// browser session only (see the sessionStorage note above) - it comes
// back next time the tab/browser is reopened, so it stays something a
// viewer is actually reminded of rather than a one-time notice they can
// permanently opt out of.
function dismissSiteBanner() {
  document.documentElement.classList.add("banner-dismissed");
  sessionStorage.setItem("bannerDismissed", "true");
}

// createTourController builds a small driver behind a guided <dialog> tour
// - shared by the home page's onboardingTour (home.templ) and the Build
// page's buildTour (build.templ, Phase 5 of
// docs/plans/in-progress/onboarding-and-ambient-framing-plan.md), so the
// open/close/step-render/auto-open logic that used to be hand-written once
// for the home tour alone exists in exactly one place, not copied for each
// new one. config:
//   - dialogId: id of this tour's own <dialog>.
//   - storageKey: localStorage flag recording "seen" - checked once per
//     browser on load (see the auto-open wiring below), set once, on the
//     dialog's native "close" event, regardless of how it closed (a
//     button, Escape, or backdrop click all fire "close" alike).
//   - queryParam: a location.search flag (e.g. "tour" for ?tour=1) that
//     forces this tour open on load regardless of the storageKey flag.
//
// Every DOM lookup below is scoped to this tour's own <dialog> element
// (dialog.querySelectorAll(...), never a bare document-wide query), so two
// tour instances' .tour-step/.tour-dot elements can never cross-wire, even
// though only one of these dialogs is ever actually on screen at a time in
// practice. Step state (`step`) lives in this closure, private to each
// controller instance, rather than one shared module-level variable the way
// the original single-tour implementation had it - that would have been
// actively wrong the moment a second tour's controller existed alongside
// this one.
function createTourController(config) {
  var step = 0;

  function dialogEl() {
    return document.getElementById(config.dialogId);
  }

  function stepCount(dialog) {
    return dialog.querySelectorAll(".tour-step").length;
  }

  // Back/Next/Done are looked up by an id *suffix* ("-back"/"-next"/
  // "-done"), not one exact id both tours share - the home tour's are
  // "tour-back" etc., the Build tour's are "build-tour-back" etc. (kept
  // distinct per-dialog on purpose, see buildTour's own doc comment,
  // build.templ) - but both do follow this same suffix convention.
  function actionButton(dialog, suffix) {
    return dialog.querySelector('[id$="' + suffix + '"]');
  }

  function render(dialog) {
    var count = stepCount(dialog);
    dialog.querySelectorAll(".tour-step").forEach(function (el) {
      el.hidden = Number(el.dataset.step) !== step;
    });
    dialog.querySelectorAll(".tour-dot").forEach(function (dot, index) {
      dot.classList.toggle("active", index === step);
    });
    var back = actionButton(dialog, "-back");
    var next = actionButton(dialog, "-next");
    var done = actionButton(dialog, "-done");
    var isLast = step === count - 1;
    if (back) {
      back.hidden = step === 0;
    }
    if (next) {
      next.hidden = isLast;
    }
    if (done) {
      done.hidden = !isLast;
    }
  }

  // Shows step 0 through .showModal() (falls back to the open attribute
  // for a browser without the method, though every current browser has
  // it) - always resets to the first step, so a manual replay (a forced
  // query/session flag, or the Account page's links) never opens mid-way
  // through wherever a previous visit left off.
  function open() {
    var dialog = dialogEl();
    if (!dialog) {
      return;
    }
    step = 0;
    render(dialog);
    if (dialog.showModal) {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
  }

  // Closing (Skip, or the final step's "Got it, thanks!") just closes the
  // dialog - recording the dismissal is handled once, centrally, by the
  // "close" listener wired below, which also covers the two ways of
  // closing this doesn't go through (Escape, clicking the backdrop) -
  // <dialog> fires "close" for all of them alike, native buttons included.
  function close() {
    var dialog = dialogEl();
    if (!dialog) {
      return;
    }
    if (dialog.close) {
      dialog.close();
    } else {
      dialog.removeAttribute("open");
    }
  }

  function back() {
    var dialog = dialogEl();
    if (dialog && step > 0) {
      step--;
      render(dialog);
    }
  }

  function next() {
    var dialog = dialogEl();
    if (dialog && step < stepCount(dialog) - 1) {
      step++;
      render(dialog);
    }
  }

  document.addEventListener("DOMContentLoaded", function () {
    var dialog = dialogEl();
    if (!dialog) {
      return;
    }
    // This is one-time orientation, not something to reopen on every visit
    // once it's been seen once - recorded here regardless of how the
    // dialog closed (button, Escape, or backdrop click all fire "close"
    // alike).
    dialog.addEventListener("close", function () {
      try {
        localStorage.setItem(config.storageKey, "true");
      } catch (err) {
        // Storage blocked - dismissal only lasts this page view; see the
        // auto-open check below for why that's an acceptable trade here.
      }
    });
  });

  // Auto-opens once per browser, or unconditionally when forced (the
  // queryParam query string). Fails *closed* on a storage-blocked browser
  // (skip auto-opening) rather than the "fail open" convention used
  // elsewhere in this file (theme, site banner, install prompt) - those are
  // small, low-cost affordances where showing once too often costs
  // nothing; a full-screen dialog reappearing on every single page load for
  // someone whose browser blocks storage would be a real annoyance, not a
  // minor miss. The forcing path is unaffected either way, since it never
  // consults localStorage.
  document.addEventListener("DOMContentLoaded", function () {
    var dialog = dialogEl();
    if (!dialog) {
      return;
    }

    var forced = new RegExp("(^|[?&])" + config.queryParam + "=1(&|$)").test(window.location.search);

    if (forced) {
      open();
      return;
    }
    try {
      if (localStorage.getItem(config.storageKey) !== "true") {
        open();
      }
    } catch (err) {
      // Storage blocked - see doc comment above.
    }
  });

  return { open: open, close: close, back: back, next: next };
}

// The home page's guided first-run tour (home.templ's onboardingTour) -
// see createTourController's own doc comment for the shared mechanics.
var onboardingTourController = createTourController({
  dialogId: "onboarding-tour",
  storageKey: "onboardingTourSeen",
  queryParam: "tour",
});

// Inline onclick="" attributes in home.templ need real global function
// names, not a reference to the controller object itself - these just
// delegate straight through to it.
function openOnboardingTour() {
  onboardingTourController.open();
}

function closeOnboardingTour() {
  onboardingTourController.close();
}

function tourBack() {
  onboardingTourController.back();
}

function tourNext() {
  onboardingTourController.next();
}

// The Build page's own guided first-run tour (build.templ's buildTour,
// Phase 5 of docs/plans/in-progress/onboarding-and-ambient-framing-plan.md)
// - same mechanics as the home tour above, via the same
// createTourController. Manual replay is via its plain ?build-tour=1 query
// param only (BuildPlanPage requires a real :service_type_id/:plan_id, so
// there's no one fixed URL to link a replay flow at ahead of time, but
// nothing needs one any more - a page's own Help dialog no longer needs a
// session handoff to reach it, since Help finds and opens this tour
// directly once someone is actually on a real plan's Build page). Used to
// also carry a sessionForceKey ("buildTourForce") session-storage handoff
// for the Account page's now-removed "Replay the Build page tour" link -
// removed once help-system-and-nav-consolidation-plan.md's own setup-page
// Next-link chain generalized that same one-shot-handoff idea into
// setPageHelpForce()/pageHelpForce below, making this Build-specific copy
// redundant.
var buildTourController = createTourController({
  dialogId: "build-tour",
  storageKey: "buildTourSeen",
  queryParam: "build-tour",
});

function openBuildTour() {
  buildTourController.open();
}

function closeBuildTour() {
  buildTourController.close();
}

function buildTourBack() {
  buildTourController.back();
}

function buildTourNext() {
  buildTourController.next();
}

// openHelpDialog/closeHelpDialog are the minimal show/hide pair backing
// every plain, single-step help <dialog> - the 8 setup-page dialogs (see
// each scheduling/*.templ's own "-help" dialog) and layout.templ's
// genericHelpDialog fallback. Deliberately NOT routed through
// createTourController: that factory exists specifically for the
// auto-open-once-per-browser + localStorage dismissal-tracking +
// forced-reopen-via-query-string behavior the Home/Build tours need: none
// of that applies here - these only ever open because someone clicked
// Help, and have exactly one step, so there's no back/next/dot state to
// track either. A controller object per dialog would be pure overhead.
function openHelpDialog(id) {
  var dialog = document.getElementById(id);
  if (!dialog) {
    return;
  }
  if (dialog.showModal) {
    dialog.showModal();
  } else {
    dialog.setAttribute("open", "");
  }
}

function closeHelpDialog(id) {
  var dialog = document.getElementById(id);
  if (!dialog) {
    return;
  }
  if (dialog.close) {
    dialog.close();
  } else {
    dialog.removeAttribute("open");
  }
}

// openPageHelp is the site footer's Help button (layout.templ) - the one
// Help control on every authenticated page, dispatching to whichever
// dialog actually belongs to the current page. Dispatch is by
// presence-of-element (does this page's own DOM have an #onboarding-tour,
// a #build-tour, or a .page-help-dialog?), not a data attribute on <body>
// or a second argument threaded through Layout's own call signature:
// Layout is called by every page in this app and its signature doesn't
// change here, so the simplest thing that works is to just look for
// whichever dialog that page's own {children...} already rendered -
// findable by id/class regardless of where in the DOM it actually sits.
// Checked in this order:
//   1. #onboarding-tour (home.templ) - reuses onboardingTourController.open()
//      rather than a raw .showModal(), so Help gets the same reset-to-step-0
//      and focus-trap behavior a real tour open always gets.
//   2. #build-tour (build.templ) - same reasoning, via buildTourController.
//   3. The first .page-help-dialog on the page - the 8 single-step setup-
//      page dialogs (relationships.templ, song_roles.templ, etc.) all share
//      this class; each page only ever renders one, so "first" is safe.
//   4. #generic-help-dialog (layout.templ's genericHelpDialog) - the
//      last-resort fallback for every other page (Account, Billing, the
//      style guide, ...), so this button is never a dead end.
function openPageHelp() {
  if (document.getElementById("onboarding-tour")) {
    onboardingTourController.open();
    return;
  }
  if (document.getElementById("build-tour")) {
    buildTourController.open();
    return;
  }
  var pageDialog = document.querySelector(".page-help-dialog");
  if (pageDialog) {
    openHelpDialog(pageDialog.id);
    return;
  }
  openHelpDialog("generic-help-dialog");
}

// setPageHelpForce/pageHelpForce are the one-shot sessionStorage handoff
// behind each setup-page help dialog's optional "Next: <page> ->" link (see
// songRelationshipsHelp's doc comment, relationships.templ, for the full
// chain reasoning and home.go's buildOnboardingChecklist for the sequence
// it mirrors) - a full page navigation, not an htmx swap, so "land on the
// next page with its own Help dialog already open" needs a flag that
// survives the navigation, the same shape buildTourController's now-removed
// "buildTourForce" one-off used for the Account page's old Build-tour
// replay link. Generalized here into one reusable pair instead of a copy
// per page.
//
// setPageHelpForce is the plain <a href="..."> Next link's onclick - sets
// the flag just before the browser's normal navigation to the next page
// proceeds. Doesn't preventDefault - a blocked/failed write just means the
// destination doesn't auto-open Help, same fail-quiet convention as the
// rest of this file's storage access; navigation itself must never be
// blocked by this.
function setPageHelpForce() {
  try {
    sessionStorage.setItem("pageHelpForce", "1");
  } catch (err) {
    // Storage blocked - navigation still proceeds normally.
  }
}

// Checked once per page load, then cleared immediately regardless of
// whether it was set - so it can never linger and force-open Help on some
// later, unrelated visit to that page (the same one-shot shape
// createTourController's own forced-reopen check uses for its queryParam
// flag).
document.addEventListener("DOMContentLoaded", function () {
  var forced = false;
  try {
    forced = !!sessionStorage.getItem("pageHelpForce");
    sessionStorage.removeItem("pageHelpForce");
  } catch (err) {
    // Storage blocked - just means this page load never auto-opens Help.
  }
  if (forced) {
    openPageHelp();
  }
});

// The header nav's "Setup" dropdown (see layout.templ) - a plain
// open/closed toggle, closed by clicking its button again, clicking
// anywhere else on the page, or pressing Escape.
function toggleNavDropdown(button) {
  var dropdown = button.closest(".nav-dropdown");
  var open = dropdown.classList.toggle("open");
  button.setAttribute("aria-expanded", open ? "true" : "false");

  if (open) {
    positionNavDropdown(dropdown);
  }
}

// .nav-dropdown-menu's default (left: 0, see app.css) grows rightward from
// the toggle - fine when there's room, but "Setup" sits near the right
// edge of the header, so on a narrow/mobile viewport that growth runs the
// menu off the edge of the screen. Re-measured every time the menu opens
// (not just once) so rotating the device or resizing the window never
// leaves it stuck on the wrong side.
function positionNavDropdown(dropdown) {
  var menu = dropdown.querySelector(".nav-dropdown-menu");
  if (!menu) {
    return;
  }

  dropdown.classList.remove("align-right");
  var overflowsRight = menu.getBoundingClientRect().right > window.innerWidth;
  dropdown.classList.toggle("align-right", overflowsRight);
}

function closeNavDropdowns() {
  document.querySelectorAll(".nav-dropdown.open").forEach(function (dropdown) {
    dropdown.classList.remove("open");
    var toggle = dropdown.querySelector(".nav-dropdown-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", "false");
    }
  });
}

// The mobile header's hamburger toggle (see layout.templ) - below
// header.site's own row/column breakpoint, .site-nav/.site-actions collapse
// behind this until .menu-open is set on the header itself (see app.css);
// at that breakpoint and up they're just always visible and this button
// doesn't render at all, so there's nothing to toggle there.
function toggleMobileMenu(button) {
  var header = button.closest(".site");
  var open = header.classList.toggle("menu-open");
  button.setAttribute("aria-expanded", open ? "true" : "false");
}

function closeMobileMenu() {
  var header = document.querySelector(".site.menu-open");
  if (!header) {
    return;
  }
  header.classList.remove("menu-open");
  var toggle = header.querySelector(".mobile-menu-toggle");
  if (toggle) {
    toggle.setAttribute("aria-expanded", "false");
  }
}

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

// App-wide "your click registered" feedback for every htmx-driven action.
// Several of this app's own endpoints are themselves several sequential PCO
// API calls deep (Publish/Revert/Import walk every song or assignment one
// at a time - see the scheduling controller), so a click can go a real
// moment with nothing on screen changing. Two things happen for the
// duration of any htmx request, app-wide, with no per-template changes
// needed:
//   1. The thin bar in layout.templ (#htmx-progress-bar) sweeps across the
//      top of the page, the same idea as a browser tab's own loading
//      spinner - visible regardless of where on the page the action was.
//   2. The actual control clicked gets really disabled (not just dimmed),
//      both giving that specific button/form its own immediate feedback and
//      stopping an impatient second click from firing a duplicate request.
//      Where hx-post/hx-delete/etc. lives on a <form> rather than the
//      button inside it (most setup-page forms), evt.detail.elt is the
//      form, so the actual submit button is found within it.
// Listens on `document`, not `document.body` - this script tag is in
// <head> (see layout.templ), so it runs before the parser has even reached
// <body>, and document.body is still null at that point. document itself
// always exists, and htmx's events bubble up to it exactly the same way,
// so nothing here needs to wait for DOMContentLoaded. #htmx-progress-bar is
// looked up fresh inside each handler for the same reason - caching
// document.getElementById("htmx-progress-bar") at parse time would just
// cache null.
(function () {
  var requestsInFlight = 0;

  function progressBar() {
    return document.getElementById("htmx-progress-bar");
  }

  function submitControlFor(el) {
    if (!el || !el.matches) {
      return null;
    }
    if (el.matches("button, input[type=submit]")) {
      return el;
    }
    return el.querySelector ? el.querySelector("button[type=submit], button:not([type])") : null;
  }

  document.addEventListener("htmx:beforeRequest", function (event) {
    requestsInFlight++;
    var bar = progressBar();
    if (bar) {
      bar.classList.add("active");
    }

    var control = submitControlFor(event.detail.elt);
    if (control && !control.disabled) {
      control.disabled = true;
      control.dataset.htmxWasEnabled = "1";
    }
  });

  function requestSettled(event) {
    requestsInFlight = Math.max(0, requestsInFlight - 1);
    if (requestsInFlight === 0) {
      var bar = progressBar();
      if (bar) {
        bar.classList.remove("active");
      }
    }

    var control = submitControlFor(event.detail.elt);
    if (control && control.dataset.htmxWasEnabled) {
      control.disabled = false;
      delete control.dataset.htmxWasEnabled;
    }
  }

  // htmx:afterRequest alone covers every terminal outcome - a normal
  // response, an HTTP error status, and (checked directly against the
  // vendored htmx.min.js source) the no-response cases too: onerror/
  // onabort/ontimeout each explicitly fire "htmx:afterRequest" themselves
  // before their own sendError/sendAbort/timeout event. Listening to those
  // as well used to double-decrement requestsInFlight for the exact same
  // request - harmless with only one request in flight (the Math.max(0,
  // ...) clamp ate it), but with two-plus concurrent requests, one
  // erroring/timing out could zero the counter early and hide the bar (or
  // re-enable a button) while another request was still genuinely running.
  document.addEventListener("htmx:afterRequest", requestSettled);
  // onload's own response/swap handling runs inside a try/catch that fires
  // "htmx:onLoadError" and *skips* "htmx:afterRequest" entirely if
  // anything in there throws (a detached swap target, a bad oob id, etc.) -
  // without this, that path left requestsInFlight permanently incremented
  // and the bar stuck on for the rest of the page's life, since nothing
  // else ever decrements it. Rare (htmx already guards the obvious cases),
  // but exactly the kind of "no specific circumstances" occasional stuck
  // spinner this is here to close off.
  document.addEventListener("htmx:onLoadError", requestSettled);

  // A stuck #htmx-progress-bar has one more source that isn't a request-
  // lifecycle bug at all: the plain-<a> click handler further down this
  // file turns the bar on for an ordinary full-page navigation and
  // deliberately never turns it back off itself, betting the page is about
  // to be replaced. That bet fails whenever this exact page survives
  // instead - the user clicks a link, then hits Back/Stop before it
  // finishes loading, and later browses back to this same page via the
  // browser's bfcache. bfcache restores the page (DOM, JS state - this
  // closure's own requestsInFlight included - and all) exactly as it was
  // at the moment they navigated away, .active class included, so the bar
  // resumes sweeping on a page where nothing is actually loading. Resets
  // requestsInFlight too, not just the class: leaving it at whatever
  // stale, nonzero count it had would mean the *next* real request's own
  // requestSettled decrements to a still-nonzero number instead of 0,
  // silently breaking the "clear the bar" case above without the bar ever
  // going stuck-visible again to notice by. `pageshow` fires on both a
  // fresh load and a bfcache restore; `event.persisted` is only true for
  // the latter, so this never touches a normal load's own real state.
  window.addEventListener("pageshow", function (event) {
    if (!event.persisted) {
      return;
    }
    requestsInFlight = 0;
    var bar = progressBar();
    if (bar) {
      bar.classList.remove("active");
    }
  });
})();

// Plain <a href> full-page navigation (e.g. the plan links in
// build.templ's plan list) isn't htmx-driven, so none of the
// htmx:beforeRequest/afterRequest machinery above ever runs for it - the
// browser's own loading chrome is all that shows, and that's often subtle
// or invisible on mobile, leaving nothing on screen between the tap and
// the next page's paint. Reuses the same #htmx-progress-bar for that case:
// any plain, same-origin, unmodified left-click on a link starts it
// sweeping right away. There's deliberately no matching "stop" here - the
// page is about to be replaced by the browser's own navigation, the same
// way its native loading indicator just runs until that happens.
document.addEventListener("click", function (event) {
  // Anything other than a plain left-click - the middle button, or a
  // held modifier that opens the link in a new tab/window/download
  // instead of navigating this one away - isn't really "this page is
  // about to navigate," so there's nothing here to show a cue for.
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
    return;
  }

  var link = event.target.closest("a[href]");
  if (!link || link.closest("[hx-boost]") || link.hasAttribute("hx-get") || link.hasAttribute("hx-post")) {
    return;
  }
  // target="_blank" (or any non-default target) opens elsewhere rather
  // than navigating this tab away; download links save a file instead of
  // navigating at all - neither leaves this page loading.
  if ((link.target && link.target !== "_self") || link.hasAttribute("download")) {
    return;
  }

  var url;
  try {
    url = new URL(link.href, window.location.href);
  } catch (e) {
    return;
  }
  // Cross-origin links (including mailto:/tel:, whose "origin" never
  // matches) navigate away from the app entirely - nothing here to show
  // a same-app loading cue for.
  if (url.origin !== window.location.origin) {
    return;
  }
  // A same-page fragment link (#section on the current URL) doesn't
  // trigger a real navigation/reload either.
  if (url.pathname === window.location.pathname && url.search === window.location.search && url.hash) {
    return;
  }

  var bar = document.getElementById("htmx-progress-bar");
  if (bar) {
    bar.classList.add("active");
  }
});

// selectPerson fills a person picker's hidden person_id input and visible
// search box from a clicked search result's data attributes (see
// PersonSearchResults in internal/views/scheduling/person_picker.templ),
// then clears that picker's results list. data-suffix identifies which
// picker this result belongs to ("" for the original single-picker ids, "a"
// / "b" etc. for pages with more than one picker on the same form - e.g.
// person relationships' "Person A"/"Person B") - see idSuffix in
// person_picker.templ for the matching id scheme.
function selectPerson(el) {
  var suffix = el.dataset.suffix ? "-" + el.dataset.suffix : "";
  document.getElementById("person-id-input" + suffix).value = el.dataset.personId;
  document.getElementById("person-search-input" + suffix).value = el.dataset.personName;
  // Deferred, not synchronous: this handler runs during the click event's
  // target phase (invoked by the clicked .search-result div's own
  // onclick), before the event finishes bubbling up to document.
  // Clearing this container's innerHTML synchronously would detach the
  // just-clicked element (the event's own target) from the DOM right
  // then, so by the time the click reaches document's own listener
  // further down this file, event.target.closest(...) can no longer find
  // ancestors that were genuinely there when the click happened (e.g. a
  // picker embedded inside a .build-detail-card, like the song leader
  // picker) - closest() on a detached node returns null, so that listener
  // wrongly concludes the click landed outside the card and closes it.
  // setTimeout(fn, 0) pushes this mutation to a fresh task, after the
  // current event has fully finished bubbling, so every listener sees the
  // real DOM as it was at click time. The value assignments above stay
  // synchronous - only this container's clear needs deferring.
  var resultsContainer = document.getElementById("person-search-results" + suffix);
  setTimeout(function () {
    resultsContainer.innerHTML = "";
  }, 0);
}

// Any further edit to a person search box invalidates whatever was
// previously selected in that same picker - without this, editing the text
// after clicking a result would leave that picker's hidden id input still
// pointing at the old (now-mismatched) person while the visible box shows
// different, unconfirmed text. Setting .value from selectPerson() above
// doesn't fire "input", so this doesn't clear the selection it just made.
// Matches "person-search-input" or "person-search-input-<suffix>" and clears
// the correspondingly-suffixed hidden input.
document.addEventListener("input", function (event) {
  var id = event.target.id || "";
  if (id.indexOf("person-search-input") !== 0) {
    return;
  }
  var hidden = document.getElementById("person-id-input" + id.slice("person-search-input".length));
  if (hidden) {
    hidden.value = "";
  }
});

// Shared reset for the setup pages' create/edit forms (song relationships,
// song/person preferences, song roles, person relationships) - restores a
// form to its "add a new one" state: native reset() (clears every field,
// including person pickers - see selectPerson - whose hidden id and
// visible search box both default back to empty) along with the hidden
// "id" field that switches the next submit from update back to create,
// plus the submit button/Cancel affordance out of "editing" mode. Called
// both after a successful create/update submit (hx-on::after-request
// below) and from the Cancel button (cancelSetupEdit).
function resetSetupForm(form) {
  form.reset();
  form.classList.remove("editing");
  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn && submitBtn.dataset.createLabel) {
    submitBtn.textContent = submitBtn.dataset.createLabel;
  }
  var cancelBtn = form.querySelector(".cancel-edit");
  if (cancelBtn) {
    cancelBtn.hidden = true;
  }
}

// applyEditFields writes a data-edit-fields-style JSON object (plain
// form-field name -> value, see editFieldsAttr) into form's matching named
// fields - the field-filling core editRow below uses, pulled out so
// fillVocalRangeFromSongSelect (a <select>-driven prefill, not a button
// click) can reuse the exact same fill logic instead of duplicating it.
function applyEditFields(form, fields) {
  Object.keys(fields).forEach(function (name) {
    var el = form.elements.namedItem(name);
    if (el) {
      el.value = fields[name];
    }
  });
}

// editRow fills a setup page's create/edit form from a table row's Edit
// button and switches it into "editing" mode, so the next submit updates
// this row (via its now-populated hidden "id" field) instead of creating a
// new one. data-edit-fields is a JSON object of plain form-field name ->
// value (see editFieldsAttr); data-edit-people is a JSON object of person-
// picker suffix -> {id, name} (see editPeopleAttr, PersonPickerField) for
// pages that search people rather than using a plain <select>.
function editRow(button) {
  var form = document.getElementById(button.dataset.editForm);
  if (!form) {
    return;
  }

  applyEditFields(form, JSON.parse(button.dataset.editFields || "{}"));

  var people = JSON.parse(button.dataset.editPeople || "{}");
  Object.keys(people).forEach(function (suffix) {
    var idSuffix = suffix ? "-" + suffix : "";
    var idInput = document.getElementById("person-id-input" + idSuffix);
    var searchInput = document.getElementById("person-search-input" + idSuffix);
    if (idInput) {
      idInput.value = people[suffix].id;
    }
    if (searchInput) {
      searchInput.value = people[suffix].name;
    }
  });

  form.classList.add("editing");
  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn) {
    if (!submitBtn.dataset.createLabel) {
      submitBtn.dataset.createLabel = submitBtn.textContent;
    }
    submitBtn.textContent = "Update";
  }
  var cancelBtn = form.querySelector(".cancel-edit");
  if (cancelBtn) {
    cancelBtn.hidden = false;
  }

  // The Song Tags page's own add-tag form (song_tags.templ) sits collapsed
  // inside a <details> to stay compact - editing an existing tag needs it
  // open, since a closed <details> hides the very fields editRow just
  // filled in. Every other setup page's form isn't wrapped in one, so
  // form.closest("details") is simply null there and this is a no-op.
  var details = form.closest("details");
  if (details) {
    details.open = true;
  }

  form.scrollIntoView({ behavior: "smooth", block: "start" });
}

function cancelSetupEdit(button) {
  var form = button.closest("form");
  resetSetupForm(form);
  var details = form.closest("details");
  if (details) {
    details.open = false;
  }
}

// fillVocalRangeFromSongSelect pre-fills the Build page's quick-add "Set
// song vocal range" form from whichever song its own picker just selected
// - docs/issues.md: "fill in the form with the existing data and make the
// save an update if applicable" (scheduling.SetSongVocalRange already
// upserts by song id regardless of what the form shows - this just makes
// the form honestly reflect that a song already has a range on file,
// mirroring editRow's fill-and-relabel behavior above but triggered by a
// <select>'s change instead of an Edit button's click). The selected
// <option>'s own data-edit-fields (same JSON shape/helper editRow already
// reads, editFieldsAttr/vocalRangeFormFields) carries reference_key/
// lowest_note/highest_note when that song has a range on file - an empty
// object otherwise, in which case every field is cleared back to blank
// instead of keeping some previously-selected song's stale values.
function fillVocalRangeFromSongSelect(select) {
  var form = select.closest("form");
  if (!form) {
    return;
  }

  var opt = select.selectedOptions[0];
  var fields = opt ? JSON.parse(opt.dataset.editFields || "{}") : {};
  applyEditFields(form, {
    reference_key: fields.reference_key || "",
    lowest_note: fields.lowest_note || "",
    highest_note: fields.highest_note || "",
  });

  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn) {
    if (!submitBtn.dataset.createLabel) {
      submitBtn.dataset.createLabel = submitBtn.textContent;
    }
    submitBtn.textContent = fields.reference_key
      ? "Update range"
      : submitBtn.dataset.createLabel;
  }
}

// filterSongTagCards backs the Song Tags setup page's own filter controls
// (song_tags.templ): the free-text #song-tag-filter box and the checked
// .song-tag-filter-checkbox tag chips above the list, both applied
// together - a card must match the typed text (if any) AND carry at
// least one of the checked tags (if any are checked). Purely client-side,
// hiding/showing each already-rendered .song-tag-card: the text query
// against its data-search (song title + tag values, substring match, see
// songTagCardSearchText) and the checked tags against its data-tags
// (tag values only, exact membership via songTagCardTagsAttr's pipe
// delimiting, so a chip for "Grace" doesn't also match a card just
// because some other tag's value contains "grace" as a substring). Every
// song renders unconditionally on that page (unlike the Build page's
// search-gated candidate list, which re-fetches from the server as you
// type - see the comment above #candidate-search's own handling below),
// so there's nothing to fetch here, just hide/show against data already
// in the DOM.
function filterSongTagCards() {
  var input = document.getElementById("song-tag-filter");
  var query = input ? input.value.trim().toLowerCase() : "";
  var checked = Array.prototype.slice
    .call(document.querySelectorAll(".song-tag-filter-checkbox:checked"))
    .map(function (checkbox) {
      return checkbox.value;
    });

  document.querySelectorAll(".song-tag-card").forEach(function (card) {
    var matchesText =
      query === "" || (card.dataset.search || "").indexOf(query) !== -1;
    var tags = card.dataset.tags || "";
    var matchesTags =
      checked.length === 0 ||
      checked.some(function (tag) {
        return tags.indexOf("|" + tag + "|") !== -1;
      });
    card.hidden = !(matchesText && matchesTags);
  });
}

// Re-applies those same filters after any htmx swap of the card list
// itself (adding/editing/removing a tag all swap the whole
// #song-tags-list) - without this, a filtered view would flash back to
// showing every song again after each mutation, even though
// #song-tag-filter and the tag filter chips (both outside the swapped
// div, so untouched by the swap) still show their previous state.
document.addEventListener("htmx:afterSwap", function (event) {
  var target = event.detail && event.detail.target;
  if (!target || target.id !== "song-tags-list") {
    return;
  }
  filterSongTagCards();
  updateSongTagBulkCount();
});

// updateSongTagBulkCount keeps #song-tag-bulk-count (song_tags.templ) in
// sync with how many of the page's .song-tag-bulk-checkbox boxes are
// currently checked - called from each checkbox's own onchange, and from
// the #song-tags-list afterSwap handler above since every one of those
// checkboxes lives inside that div (an add/edit/delete/bulk-add anywhere
// on the page always resets them all to unchecked).
function updateSongTagBulkCount() {
  var countEl = document.getElementById("song-tag-bulk-count");
  if (!countEl) {
    return;
  }
  var count = document.querySelectorAll(".song-tag-bulk-checkbox:checked").length;
  countEl.textContent = count === 1 ? "1 song selected" : count + " songs selected";
}

// The Song Tags page's #song-tag-filter-chips container gets a full
// out-of-band outerHTML replace on every create/edit
// (songTagFilterChipsOOB, song_tags.templ) so a newly-used tag value's
// chip shows up without a reload - but that replace is built fresh from
// the server's own tag data, with no idea which checkboxes this browser
// had checked. Capture them right before the swap and re-check the
// matching ones right after, so an in-progress filter survives a tag
// mutation elsewhere on the page instead of silently resetting to "show
// everything".
var songTagFilterChipsCheckedBeforeSwap = null;

document.addEventListener("htmx:oobBeforeSwap", function (event) {
  var target = event.detail && event.detail.target;
  if (!target || target.id !== "song-tag-filter-chips") {
    return;
  }
  songTagFilterChipsCheckedBeforeSwap = Array.prototype.slice
    .call(target.querySelectorAll(".song-tag-filter-checkbox:checked"))
    .map(function (checkbox) {
      return checkbox.value;
    });
});

document.addEventListener("htmx:oobAfterSwap", function (event) {
  var target = event.detail && event.detail.target;
  if (
    !target ||
    target.id !== "song-tag-filter-chips" ||
    !songTagFilterChipsCheckedBeforeSwap
  ) {
    return;
  }
  var checkedValues = songTagFilterChipsCheckedBeforeSwap;
  songTagFilterChipsCheckedBeforeSwap = null;
  target.querySelectorAll(".song-tag-filter-checkbox").forEach(function (checkbox) {
    if (checkedValues.indexOf(checkbox.value) !== -1) {
      checkbox.checked = true;
    }
  });
  filterSongTagCards();
});

// Generic collapsible sections used across the build page (see
// collapsibleSection in the scheduling views package, and the position-
// group cards in roster_builder.templ, which build the same .collapsible/
// .collapsible-toggle/.collapsible-body structure by hand) - same class-
// toggle-plus-aria-expanded idiom as toggleNavDropdown above, just without
// the floating-menu positioning since these sit inline in the page rather
// than popping over it.
//
// Every explicit toggle here is also remembered in localStorage, keyed by
// the section's own stable id (find-songs-candidates, position-group-N,
// etc. - see collapsible.templ) so a person who collapses sections they
// don't need doesn't have to redo that on every reload - see
// applyStoredCollapsibleState below for the read-back half of this. This
// is the reload equivalent of openCollapsibleIDs further down, which only
// covers state surviving an htmx swap within the same page load; the two
// don't conflict since a swap's restore just reapplies whatever's already
// live in the DOM (itself already reconciled with localStorage by the
// time any swap can happen).
//
// openCandidatesSection()/toggleRoleFilter() below force a section open by
// touching its classList/aria-expanded directly rather than calling this
// function, specifically so that force-open never gets recorded here as
// an explicit "open" choice - and, symmetrically, a stored "closed" state
// never overrides one of those force-opens, since applyStoredCollapsibleState
// only ever runs once, on initial page load, well before either of those
// can fire.
function collapsibleStorageKey(id) {
  return "collapsible-" + id;
}

function toggleCollapse(button) {
  var section = button.closest(".collapsible");
  var open = section.classList.toggle("open");
  button.setAttribute("aria-expanded", open ? "true" : "false");
  if (section.id) {
    localStorage.setItem(collapsibleStorageKey(section.id), open ? "open" : "closed");
  }
}

// Read-back half of the above: applies whichever open/closed state was
// last explicitly chosen for each collapsible section, once, on initial
// page load - a section with no stored state (never toggled, or a newer
// section id nothing's been saved for yet) just keeps whatever the
// server rendered it as (see collapsibleSection/collapsibleCard's own
// `open` param). Deliberately DOMContentLoaded only, not also wired to
// htmx:afterSwap the way wireSortableTables is - the swap-time restore
// below (openCollapsibleIDs) already carries the live, currently-correct
// open/closed set forward across swaps, so re-reading localStorage there
// would be redundant at best.
function applyStoredCollapsibleState() {
  document.querySelectorAll(".collapsible[id]").forEach(function (section) {
    var stored = localStorage.getItem(collapsibleStorageKey(section.id));
    if (stored !== "open" && stored !== "closed") {
      return;
    }
    var open = stored === "open";
    section.classList.toggle("open", open);
    var toggle = section.querySelector(".collapsible-toggle");
    if (toggle) {
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    }
  });
}
document.addEventListener("DOMContentLoaded", applyStoredCollapsibleState);

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
// (set_builder.templ) - resets both the typed search text and any active
// role filter (mirroring toggleRoleFilter's own "turn it back off" branch)
// back to "", then re-triggers the same search request, landing back on
// CandidateSearchResults' empty-query-and-role "browse titles" state.
// Without this, clicking a role filter or tag chip (which both just type
// into #candidate-search / set the hidden role input) left no way back to
// that list short of manually erasing the search box - not obvious, and
// didn't clear a role filter's own "active" button styling either.
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

  if (window.htmx) {
    htmx.trigger(searchInput, "search");
  }
}

// Sortable admin tables (relationships/preferences/song roles - marked
// class="sortable" in their templates). Deliberately NOT used on the set
// builder's order-of-service table, where row order is the actual data,
// not just a display convenience. Click a header to sort by that column's
// text, click again to reverse; sort-asc/sort-desc classes on the <th>
// drive the ▲/▼ indicator via CSS (components.css).
function makeSortable(table) {
  table.querySelectorAll("thead th").forEach(function (th, index) {
    if (!th.textContent.trim()) {
      return; // empty header = an actions column, nothing to sort by
    }
    th.classList.add("sortable-col");
    th.addEventListener("click", function () {
      sortTableByColumn(table, index, th);
    });
  });
}

function sortTableByColumn(table, index, th) {
  var tbody = table.querySelector("tbody");
  var rows = Array.prototype.slice.call(tbody.querySelectorAll("tr"));
  var ascending = !th.classList.contains("sort-asc");

  table.querySelectorAll("thead th").forEach(function (other) {
    other.classList.remove("sort-asc", "sort-desc");
  });
  th.classList.add(ascending ? "sort-asc" : "sort-desc");

  rows.sort(function (a, b) {
    var aText = ((a.children[index] && a.children[index].textContent) || "").trim();
    var bText = ((b.children[index] && b.children[index].textContent) || "").trim();
    var cmp = aText.localeCompare(bText, undefined, { numeric: true, sensitivity: "base" });
    return ascending ? cmp : -cmp;
  });

  rows.forEach(function (row) {
    tbody.appendChild(row);
  });
}

// Tables get replaced wholesale on every htmx swap (outerHTML on the
// wrapping div), so sorting has to be rewired after each swap too - not
// just once on initial load.
function wireSortableTables() {
  document.querySelectorAll("table.sortable").forEach(makeSortable);
}
document.addEventListener("DOMContentLoaded", wireSortableTables);
document.addEventListener("htmx:afterSwap", wireSortableTables);

// Order-of-service drag reordering (set builder). Built on Pointer Events
// rather than the HTML5 Drag and Drop API on purpose: HTML5 DnD doesn't
// reliably fire from touchscreens without extra plumbing, while pointer
// events cover mouse, touch, and pen through one code path. Dragging is
// scoped to each row's .drag-handle (touch-action: none in app.css keeps a
// touch drag on the handle from also scrolling the page), not the whole
// row, so tapping Remove/Replace elsewhere in the row still works.
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
  var dragOverRow = null;

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

  function findDropTargetRow(tbody, clientY) {
    var rows = tbody.querySelectorAll("tr[data-token]");
    var target = null;
    for (var i = 0; i < rows.length; i++) {
      var rect = rows[i].getBoundingClientRect();
      if (clientY >= rect.top + rect.height / 2) {
        target = rows[i];
      }
    }
    return target;
  }

  function clearDragOverStyles(tbody) {
    tbody.querySelectorAll(".drag-over, .drag-over-top").forEach(function (el) {
      el.classList.remove("drag-over", "drag-over-top");
    });
  }

  function onPointerMove(event) {
    if (!dragging) {
      return;
    }
    var tbody = dragging.closest("tbody");
    var target = findDropTargetRow(tbody, event.clientY);
    if (target === dragging) {
      target = dragOverRow; // dropping "on itself" - keep the last real target
    }
    clearDragOverStyles(tbody);
    dragOverRow = target;
    if (target) {
      target.classList.add("drag-over");
      return;
    }
    // No target row means the pointer is above every row's midpoint - i.e.
    // dropping at the very top of service. There's no "row above" to draw
    // a bottom-edge marker on, so flag the first row's top edge instead.
    var firstRow = tbody.querySelector("tr[data-token]");
    if (firstRow && firstRow !== dragging) {
      firstRow.classList.add("drag-over-top");
    }
  }

  function endDrag() {
    document.removeEventListener("pointermove", onPointerMove);
    document.removeEventListener("pointerup", onPointerUp);
    document.removeEventListener("pointercancel", endDrag);
    if (dragging) {
      dragging.classList.remove("dragging");
      clearDragOverStyles(dragging.closest("tbody"));
    }
    dragging = null;
    dragOverRow = null;
  }

  function onPointerUp() {
    if (!dragging) {
      return;
    }
    var draggedRow = dragging;
    var after = rowToken(dragOverRow);
    endDrag();

    var rowID = draggedRow.dataset.rowId;
    var url = draggedRow.dataset.repositionUrl;
    if (!rowID || !url) {
      return;
    }
    // No-op if dropped back where it already was.
    if (rowToken(previousTargetRow(draggedRow)) === after) {
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
    // Defaults to the row's own current predecessor, not null - the
    // dragged row is itself one of the candidates findDropTargetRow
    // considers, so if the pointer never actually crosses a genuinely
    // different row's midpoint (most reliably reproduced by dragging the
    // *last* row, where there's nothing below to cross), it resolves to
    // "dropped on itself" every single pointermove, and previously fell
    // back to a still-null dragOverRow - read by onPointerUp as "no
    // target at all," which onPointerMove's own fallback treats as
    // dropping at the very top of service. Starting from the real
    // previous row instead means that same "never found a new target"
    // case now correctly resolves to "still after whatever it already
    // was," which the existing no-op check below then catches instead of
    // firing off a real (and wrong) reposition to the top.
    dragOverRow = previousTargetRow(row);
    row.classList.add("dragging");

    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", onPointerUp);
    document.addEventListener("pointercancel", endDrag);
  });
})();

// --- Vocal range reference tone -----------------------------------------
//
// Step 2 ("later") of docs/ideas.md's "Vocal range per vocalist" bullet:
// tap-to-hear a reference tone for a stored note, so a director unsure
// what a "G4" actually sounds like doesn't have to work it out by ear or
// on an instrument. Originally built as a synthesized tone only (a plain
// sine OscillatorNode via the Web Audio API) - no asset files needed, but
// a synthesized sine wave is a thinner, less musical reference than an
// actual piano note. Now that assets/piano-sound-samples/ exists (a real
// chromatic E2-C6 piano recording set, MIT-licensed, see that folder's
// MAPPING.md, served unauthenticated at /piano-samples/ - see routes.go)
// a real recorded sample is the primary sound: a genuine piano note is a
// more faithful and pleasant reference tone than a synthesized sine, and
// E2-C6 already covers typical choir range (that folder's own MAPPING.md).
// The original synthesis code is kept, unchanged, as the fallback for the
// rare note outside that sampled range (very low bass or very high
// soprano/countertenor notes) - see playVocalTone below. See
// docs/plans/in-progress/vocal-range-plan.md's status note for where this
// landed.

// vocalAbsoluteSemitone combines a pitch class (0-11) + octave into one
// internally-consistent absolute integer - octave*12 + pitchClass,
// mirroring scheduling.AbsoluteSemitone exactly (Go,
// internal/scheduling/key_engine.go) so a played tone always matches what
// that same pitch class/octave pair means when stored. Shared by
// vocalNoteFrequency (the synthesis fallback) and playVocalTone's
// sampled-range bounds check below, so the formula lives in exactly one
// place on the JS side rather than being hardcoded twice.
function vocalAbsoluteSemitone(pitchClass, octave) {
  return octave * 12 + pitchClass;
}

// vocalNoteFrequency converts a pitch class (0-11) + octave into a
// frequency in Hz, standard A440 equal temperament - backs the synthesis
// fallback only (see playVocalTone). A4 (octave 4, pitch class 9) is
// absolute semitone 57, which is where the formula's "-57" reference
// point comes from.
function vocalNoteFrequency(pitchClass, octave) {
  var absoluteSemitone = vocalAbsoluteSemitone(pitchClass, octave);
  return 440 * Math.pow(2, (absoluteSemitone - 57) / 12);
}

// vocalSampleNoteLetters is vocalNotePitchClasses' inverse - pitch class
// (0-11) -> the sharp-only filename spelling assets/piano-sound-samples/
// uses (e.g. pitch class 1 -> "Cs", matching Cs4.mp3). That pack only
// recorded sharp spellings (see its own MAPPING.md), so this never needs
// a flat variant.
var vocalSampleNoteLetters = [
  "C", "Cs", "D", "Ds", "E", "F", "Fs", "G", "Gs", "A", "As", "B",
];

// vocalSampleMinAbsoluteSemitone/vocalSampleMaxAbsoluteSemitone bound the
// real recordings available under assets/piano-sound-samples/ - a
// chromatic run from E2 through C6 (see that folder's MAPPING.md), chosen
// to cover typical choir range. A pitch class/octave pair outside this
// span has no recorded sample and falls back to synthesis (see
// playVocalTone).
var vocalSampleMinAbsoluteSemitone = vocalAbsoluteSemitone(4, 2); // E2
var vocalSampleMaxAbsoluteSemitone = vocalAbsoluteSemitone(0, 6); // C6

// vocalSampleURL builds a piano-sample file's URL for a pitch class +
// octave - e.g. pitch class 1, octave 4 -> "/piano-samples/Cs4.mp3". The
// mount itself is registered in routes.go (s.App.Static) and exempted
// from RequireAuth alongside /static/* (see publicPrefixes,
// internal/server/middleware/auth.go) - a sound file carries no
// auth-relevant content, same trust level as CSS/JS already served
// openly.
function vocalSampleURL(pitchClass, octave) {
  return "/piano-samples/" + vocalSampleNoteLetters[pitchClass] + octave + ".mp3";
}

// vocalNotePitchClasses mirrors keyPitchClasses/vocalNoteRe's letter ->
// pitch class table (internal/server/controllers/vocal_range.go) - kept
// as a separate JS-side table rather than shared with the Go code (no
// realistic way to share a lookup table across that language boundary),
// but must stay numerically identical to it.
var vocalNotePitchClasses = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };

// vocalNoteTextRe mirrors parseVocalNote's vocalNoteRe exactly (Go,
// internal/server/controllers/vocal_range.go): a note letter A-G, an
// optional single sharp/flat accidental (ASCII or Unicode), then a
// signed integer octave.
var vocalNoteTextRe = /^([A-Ga-g])(#|♯|b|♭)?(-?[0-9]+)$/;

// parseVocalNoteText parses the same note+octave text format
// parseVocalNote (Go) accepts, returning {pitchClass, octave} or null if
// it doesn't parse - so "hear it" always matches what would actually be
// saved, and a not-yet-valid in-progress input (still being typed) just
// silently does nothing rather than erroring.
function parseVocalNoteText(text) {
  if (typeof text !== "string") {
    return null;
  }
  var m = vocalNoteTextRe.exec(text.trim());
  if (!m) {
    return null;
  }
  var pitchClass = vocalNotePitchClasses[m[1].toUpperCase()];
  if (pitchClass === undefined) {
    return null;
  }
  if (m[2] === "#" || m[2] === "♯") {
    pitchClass += 1;
  } else if (m[2] === "b" || m[2] === "♭") {
    pitchClass -= 1;
  }
  pitchClass = ((pitchClass % 12) + 12) % 12;

  var octave = parseInt(m[3], 10);
  if (isNaN(octave)) {
    return null;
  }
  return { pitchClass: pitchClass, octave: octave };
}

// vocalToneAudioContext is a single lazily-created AudioContext, reused
// across every played tone rather than one per call - browsers require
// it to originate from a user gesture anyway (a button click satisfies
// that), and creating a fresh one per play would be wasteful and can hit
// browser-imposed limits on concurrent contexts.
var vocalToneAudioContext = null;

// playVocalToneSynthesized plays a short sine reference tone at the given
// pitch class/octave's frequency (~0.8s, with a brief linear attack/
// release envelope on a GainNode to avoid a click/pop at start/stop) -
// the original (and, before the piano sample set existed, only)
// implementation of playVocalTone, kept unchanged and now used only as
// the fallback for a note outside the sampled range (see playVocalTone).
// Wrapped entirely in try/catch: a browser blocking audio (autoplay
// policy, no AudioContext support at all) should mean the button silently
// does nothing, never a broken page or a thrown error.
function playVocalToneSynthesized(pitchClass, octave) {
  try {
    if (!vocalToneAudioContext) {
      var AudioContextCtor = window.AudioContext || window.webkitAudioContext;
      if (!AudioContextCtor) {
        return;
      }
      vocalToneAudioContext = new AudioContextCtor();
    }
    var ctx = vocalToneAudioContext;
    if (ctx.state === "suspended") {
      ctx.resume();
    }

    var frequency = vocalNoteFrequency(pitchClass, octave);
    var now = ctx.currentTime;
    var attack = 0.02;
    var duration = 0.8;
    var peakGain = 0.3;

    var oscillator = ctx.createOscillator();
    oscillator.type = "sine";
    oscillator.frequency.setValueAtTime(frequency, now);

    var gainNode = ctx.createGain();
    gainNode.gain.setValueAtTime(0, now);
    gainNode.gain.linearRampToValueAtTime(peakGain, now + attack);
    gainNode.gain.setValueAtTime(peakGain, now + duration - attack);
    gainNode.gain.linearRampToValueAtTime(0, now + duration);

    oscillator.connect(gainNode);
    gainNode.connect(ctx.destination);

    oscillator.start(now);
    oscillator.stop(now + duration);
  } catch (err) {
    // Autoplay blocked, no Web Audio support, etc. - silently do nothing.
  }
}

// playVocalTone plays a reference tone for the given pitch class/octave -
// a real recorded piano note (assets/piano-sound-samples/, served at
// /piano-samples/) when one exists for it, falling back to
// playVocalToneSynthesized's sine tone otherwise (a note outside the
// sampled E2-C6 range - rare in practice, since that range already covers
// typical choir range). Wrapped entirely in try/catch, same reasoning as
// playVocalToneSynthesized: a blocked/failed play should stay silent,
// never a broken page or a thrown error - any failure to even start the
// sample playback falls through to the synthesized fallback rather than
// just going silent, so a director always hears *something*.
function playVocalTone(pitchClass, octave) {
  var absoluteSemitone = vocalAbsoluteSemitone(pitchClass, octave);
  if (
    absoluteSemitone >= vocalSampleMinAbsoluteSemitone &&
    absoluteSemitone <= vocalSampleMaxAbsoluteSemitone
  ) {
    try {
      var audio = new Audio(vocalSampleURL(pitchClass, octave));
      audio.play().catch(function () {
        // Autoplay blocked, network error, etc. - silently do nothing
        // (matches playVocalToneSynthesized's own silent-failure
        // convention). Deliberately not falling back to synthesis here:
        // Audio.play()'s promise rejection fires asynchronously, well
        // after this function has already returned, so a fallback here
        // would risk playing a late, unexpected tone after the button
        // press moment has passed.
      });
      return;
    } catch (err) {
      // Synchronous construction/playback failure - fall through to
      // synthesis below rather than staying silent.
    }
  }
  playVocalToneSynthesized(pitchClass, octave);
}

// playVocalNoteFromInput reads a text input's current value (the add-
// range form's still-being-typed Lowest/Highest note fields), parses it,
// and plays it if valid - does nothing otherwise, so previewing a note
// before save never errors on an in-progress, not-yet-valid value.
function playVocalNoteFromInput(inputId) {
  var input = document.getElementById(inputId);
  if (!input) {
    return;
  }
  var parsed = parseVocalNoteText(input.value);
  if (!parsed) {
    return;
  }
  playVocalTone(parsed.pitchClass, parsed.octave);
}

// --- Song vocal range key/octave preview --------------------------------
//
// docs/issues.md: "I feel like we're missing the ability to change the
// relative key of an existing range to hear what the modulated range
// would be. Could also be helpful to do things like bring it down an
// octave to differentiate for male/female type of thing." Preview-only -
// this never changes what's actually stored (song_vocal_range.templ's "As
// measured"/"Current key" lines, or the underlying DB row); it just
// recomputes a transposed low/high pair from the row's already-saved
// reference key + exact low/high notes, expressed as absolute semitones
// (the data-reference-key/data-low-abs/data-high-abs attributes
// vocalRangePreviewControls, song_vocal_range.templ, renders per row) and
// plays it - the same "hear it before it's committed" spirit as
// playVocalNoteFromInput above, just previewing a transposition of an
// already-saved row instead of an in-progress form value.
//
// The semitone arithmetic below deliberately mirrors
// internal/scheduling/key_engine.go and EffectiveVocalRange
// (song_vocal_range.go) function-for-function (vocalKeyPitchClass ~
// ParseKeyPitchClass, vocalSignedSemitoneDelta ~ SignedSemitoneDelta,
// vocalPitchClassAndOctave ~ PitchClassAndOctave) rather than
// round-tripping through htmx on every key keystroke/octave click - same
// "duplicate the primitives, keep them numerically identical" call this
// file already makes for vocalAbsoluteSemitone/vocalNoteFrequency
// mirroring AbsoluteSemitone above. If key_engine.go's math ever changes,
// these need updating to match. No tonic-anchor octave to duplicate here
// any more (migration 00043 removed that whole concept from
// EffectiveVocalRange server-side) - just the saved low/high absolute
// semitones, shifted by the same signed delta the server applies.

// vocalNoteDisplayNames mirrors vocalNoteDisplayNames (Go,
// internal/views/scheduling/vocal_range.templ) - sharp spelling only, a
// computed preview note has one canonical display form same as a stored
// one does.
var vocalNoteDisplayNames = [
  "C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B",
];

// vocalNoteDisplayText mirrors formatVocalNoteDisplay (Go, vocal_range
// .templ).
function vocalNoteDisplayText(pitchClass, octave) {
  return vocalNoteDisplayNames[((pitchClass % 12) + 12) % 12] + octave;
}

// vocalKeyPitchClass parses a bare key string ("G", "Bb", "F#m") into a
// 0-11 pitch class, mirroring ParseKeyPitchClass (Go,
// internal/scheduling/key_engine.go) exactly, including that function's
// two quirks: major/minor quality (a trailing lowercase "m") is stripped
// and otherwise ignored entirely, and the accidental character ("#"/"♯"/
// "b"/"♭") is matched case-sensitively, unmodified, against whatever
// follows the (uppercased) letter. Reuses vocalNotePitchClasses, the same
// letter -> pitch-class table parseVocalNoteText already uses for note
// parsing - see that table's own doc comment. Returns null for anything
// that doesn't parse, the same "still being typed" tolerance
// parseVocalNoteText already applies.
function vocalKeyPitchClass(text) {
  if (typeof text !== "string") {
    return null;
  }
  var key = text.trim();
  if (key.slice(-1) === "m") {
    key = key.slice(0, -1);
  }
  if (key === "") {
    return null;
  }

  var pitchClass = vocalNotePitchClasses[key[0].toUpperCase()];
  if (pitchClass === undefined) {
    return null;
  }

  var accidental = key.slice(1);
  if (accidental === "#" || accidental === "♯") {
    pitchClass += 1;
  } else if (accidental === "b" || accidental === "♭") {
    pitchClass -= 1;
  } else if (accidental !== "") {
    return null;
  }

  return ((pitchClass % 12) + 12) % 12;
}

// vocalSignedSemitoneDelta mirrors SignedSemitoneDelta (Go,
// key_engine.go) exactly - the minimal SIGNED shift (-6..6) from pitch
// class fromPC to pitch class toPC, a tie at exactly 6 resolving to +6.
function vocalSignedSemitoneDelta(fromPC, toPC) {
  var delta = (((toPC - fromPC) % 12) + 12) % 12;
  if (delta > 6) {
    delta -= 12;
  }
  return delta;
}

// vocalPitchClassAndOctave mirrors PitchClassAndOctave (Go,
// key_engine.go) exactly - AbsoluteSemitone's inverse, recovering the
// 0-11 pitch class and octave (which may be negative) that combine back
// into a given absolute semitone value.
function vocalPitchClassAndOctave(absoluteSemitone) {
  var pitchClass = ((absoluteSemitone % 12) + 12) % 12;
  var octave = (absoluteSemitone - pitchClass) / 12;
  return { pitchClass: pitchClass, octave: octave };
}

// vocalRangePreviewContainer finds the .vocal-range-preview wrapper
// (vocalRangePreviewControls, song_vocal_range.templ) an event originated
// inside - both entry points below (the key input and the two octave
// buttons) are nested inside exactly one of these per row.
function vocalRangePreviewContainer(el) {
  return el.closest(".vocal-range-preview");
}

// shiftVocalRangePreviewOctave bumps a row's preview octave shift (stored
// on the container's own data-octave-shift attribute, so each row's shift
// is independent) by delta (-1 or +1, an octave down/up), updates the
// small shift indicator next to the buttons, and re-renders the preview
// line with the new shift applied.
function shiftVocalRangePreviewOctave(button, delta) {
  var container = vocalRangePreviewContainer(button);
  if (!container) {
    return;
  }

  var shift = parseInt(container.getAttribute("data-octave-shift"), 10);
  if (isNaN(shift)) {
    shift = 0;
  }
  shift += delta;
  container.setAttribute("data-octave-shift", String(shift));

  var shiftLabel = container.querySelector(".vocal-range-preview-shift");
  if (shiftLabel) {
    shiftLabel.textContent =
      shift === 0 ? "" : (shift > 0 ? "+" + shift : String(shift)) + " 8ve";
  }

  renderVocalRangePreview(container.querySelector(".vocal-range-preview-key-input"));
}

// renderVocalRangePreview recomputes and renders one row's preview line
// from its container's data-reference-key/data-low-abs/data-high-abs (r's
// own stored values, see vocalRangePreviewControls) plus whatever key text
// is currently in the input and the container's current octave shift. An
// empty key input falls back to the row's own
// reference key - so the octave buttons alone (no key ever typed) still
// preview something, the "bring it down an octave... male/female" use
// case docs/issues.md describes. Hides the preview line entirely (quiet
// skip, this codebase's established convention - e.g.
// keyTransitionWarnings' unparseable-key skip) when the key text doesn't
// parse.
function renderVocalRangePreview(input) {
  var container = vocalRangePreviewContainer(input);
  if (!container) {
    return;
  }
  var line = container.querySelector(".vocal-range-preview-line");
  if (!line) {
    return;
  }

  var referenceKey = container.getAttribute("data-reference-key") || "";
  var lowAbs = parseInt(container.getAttribute("data-low-abs"), 10);
  var highAbs = parseInt(container.getAttribute("data-high-abs"), 10);
  var octaveShift = parseInt(container.getAttribute("data-octave-shift"), 10);
  if (isNaN(octaveShift)) {
    octaveShift = 0;
  }

  var keyText = input.value.trim() || referenceKey;
  var targetPitchClass = vocalKeyPitchClass(keyText);
  var referencePitchClass = vocalKeyPitchClass(referenceKey);

  if (
    targetPitchClass === null ||
    referencePitchClass === null ||
    isNaN(lowAbs) ||
    isNaN(highAbs)
  ) {
    line.hidden = true;
    line.innerHTML = "";
    return;
  }

  var delta = vocalSignedSemitoneDelta(referencePitchClass, targetPitchClass);
  var octaveOffsetSemitones = octaveShift * 12;

  var low = vocalPitchClassAndOctave(lowAbs + delta + octaveOffsetSemitones);
  var high = vocalPitchClassAndOctave(highAbs + delta + octaveOffsetSemitones);

  line.hidden = false;
  line.innerHTML =
    "Preview (" + keyText + "): " +
    vocalNoteDisplayText(low.pitchClass, low.octave) +
    " " + vocalRangePreviewPlayButtonHTML("lowest", low.pitchClass, low.octave) +
    " - " +
    vocalNoteDisplayText(high.pitchClass, high.octave) +
    " " + vocalRangePreviewPlayButtonHTML("highest", high.pitchClass, high.octave);
}

// vocalRangePreviewPlayButtonHTML builds one tap-to-hear button for a
// preview line's dynamically-computed note. Deliberately matches
// IconPlay's exact SVG markup (internal/views/components/icons.templ) and
// .icon-btn's styling by hand, so a client-side-generated button looks
// identical to every server-rendered play button on this page - see this
// section's own doc comment for why duplicating rather than sharing a
// template is the right call here. label is always one of this file's own
// two literal strings ("lowest"/"highest") and pitchClass/octave are
// always plain integers - no untrusted input ever reaches this string, so
// building it via concatenation rather than DOM APIs is safe.
function vocalRangePreviewPlayButtonHTML(label, pitchClass, octave) {
  return (
    '<button type="button" class="icon-btn" aria-label="Play previewed ' +
    label + ' note" title="Play previewed ' + label + ' note" ' +
    'onclick="playVocalTone(' + pitchClass + ', ' + octave + ')">' +
    '<svg class="icon" width="16" height="16" viewBox="0 0 24 24" fill="currentColor">' +
    '<path d="M6 4l14 8-14 8V4Z"></path></svg>' +
    '</button>'
  );
}
