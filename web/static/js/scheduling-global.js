// scheduling-global.js - app-wide chrome shared by every page: theme
// toggle, the early-preview banner's dismissal, the guided-tour controller
// (home page + Build page tours), the generic single-step help dialogs plus
// the footer's openPageHelp dispatcher, the header nav dropdown + mobile
// hamburger menu, and the app-wide htmx request feedback (progress bar +
// button-disable, and the plain-<a> navigation loading cue). Split out of
// the former single scheduling.js - see web/static/css/ for the same
// per-concern split already used for this app's CSS
// (internal/views/layout.templ's designSystemCSSFiles).

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

// Scroll-shadow affordance for horizontally-scrollable tables
// (components.SortableTableWrap/TableWrap, table.css's .table-wrap) - a
// narrow phone can't fit every setup-page table (or the Build page's
// order-of-service table) without horizontal scroll, and there was
// previously no visual cue that more columns sit off-screen. Toggles
// .can-scroll-left/.can-scroll-right on each .table-wrap based on its own
// scrollLeft/scrollWidth, which drives the edge-fade box-shadow in
// table.css. Tables get replaced wholesale on every htmx swap (outerHTML
// on the wrapping div), same as scheduling-sortable-table.js's own
// wireSortableTables - rewires on DOMContentLoaded and htmx:afterSwap for
// the same reason, plus window resize/orientation change since a wrap's
// scrollability itself can flip at a breakpoint (e.g. rotating a phone to
// landscape). data-scroll-affordance-wired guards against attaching a
// second scroll listener to a wrap that survives a resize (a swap always
// produces a brand-new element with no dataset flag, so it never needs
// the guard - only resize revisits already-wired wraps).
(function () {
  function updateScrollState(wrap) {
    var canScroll = wrap.scrollWidth > wrap.clientWidth + 1; // +1: sub-pixel rounding slop
    wrap.classList.toggle("can-scroll-left", canScroll && wrap.scrollLeft > 0);
    wrap.classList.toggle(
      "can-scroll-right",
      canScroll && wrap.scrollLeft < wrap.scrollWidth - wrap.clientWidth - 1,
    );
  }

  function wireTableScrollAffordance() {
    document.querySelectorAll(".table-wrap").forEach(function (wrap) {
      if (wrap.dataset.scrollAffordanceWired) {
        updateScrollState(wrap);
        return;
      }
      wrap.dataset.scrollAffordanceWired = "true";
      wrap.addEventListener("scroll", function () {
        updateScrollState(wrap);
      });
      updateScrollState(wrap);
    });
  }

  document.addEventListener("DOMContentLoaded", wireTableScrollAffordance);
  document.addEventListener("htmx:afterSwap", wireTableScrollAffordance);
  window.addEventListener("resize", wireTableScrollAffordance);
})();
