// Drives the custom in-app "Install this app" affordance - see
// docs/plans/shipped/pwa-install-improvements-plan.md for the full design.
// Backs two render locations that share this one script (Layout's
// dismissible banner and account.templ's persistent Account-page section,
// see layout.templ/account.templ) rather than duplicating this logic.
//
// Two entirely different install paths, since only one of them has a
// programmatic API at all:
//
// - Chromium (Chrome/Edge, desktop and Android): listen for
//   `beforeinstallprompt`, stash the event, and call its own .prompt()
//   from our own button on our own timing instead of letting the browser
//   show its native mini-infobar whenever it feels like it.
// - iOS Safari: there is no such event and never will be (Apple has never
//   shipped one) - the only path is the manual Share -> "Add to Home
//   Screen" flow, which can only be explained, not triggered. Detected via
//   plain user-agent sniffing, one of the few defensible uses of it since
//   there's no feature-detectable alternative.
//
// Follows this codebase's existing plain-ES5 JS style (see scheduling.js/
// register-sw.js) - var/function, no arrow functions or template literals.

var deferredInstallPrompt = null;

// Persistent "don't nag" dismissal - a genuinely per-viewer, non-critical
// UI preference (localStorage, not this app's own database - see plan
// doc). RENAG_DAYS matches the plan's own starting guess.
var INSTALL_DISMISS_KEY = "installPromptDismissedAt";
var INSTALL_RENAG_DAYS = 14;

// Day-count gate for the dismissible banner only (the Account page section
// always renders regardless of this - it's the deliberate, findable home
// for the feature, not the transient nudge). Counts distinct days the app
// was opened, not page loads, so navigating within one visit doesn't rack
// up "days" on its own.
var INSTALL_DAY_COUNT_KEY = "installPromptDayCount";
var INSTALL_LAST_SEEN_DAY_KEY = "installPromptLastSeenDay";
var INSTALL_BANNER_MIN_DAYS = 3;

// Every localStorage read/write below is wrapped in try/catch and fails
// open (treat a throw as "allowed to show" / "not yet dismissed") per the
// plan doc's reasoning: a private window or blocked site data throws
// rather than returning null, and failing open here costs nothing (worst
// case the prompt shows one extra time) while failing closed would
// silently and permanently suppress the feature for anyone whose browser
// blocks storage access.

function installPromptShouldShow() {
  try {
    var dismissed = localStorage.getItem(INSTALL_DISMISS_KEY);
    if (!dismissed) {
      return true;
    }
    var daysSince = (Date.now() - Number(dismissed)) / 86400000;
    return daysSince > INSTALL_RENAG_DAYS;
  } catch (err) {
    return true;
  }
}

function installPromptRecordDismissal() {
  try {
    localStorage.setItem(INSTALL_DISMISS_KEY, String(Date.now()));
  } catch (err) {
    // Storage blocked - nothing to persist; worst case the banner just
    // reappears next visit instead of staying dismissed.
  }
}

// Bumps the day counter at most once per calendar day and returns the
// current count. Called once per page load (see bootstrap below), not
// once per refresh, so it can't double-count within a single visit.
function installPromptBumpDayCount() {
  try {
    var today = new Date().toISOString().slice(0, 10);
    var lastSeen = localStorage.getItem(INSTALL_LAST_SEEN_DAY_KEY);
    var count = Number(localStorage.getItem(INSTALL_DAY_COUNT_KEY)) || 0;
    if (lastSeen !== today) {
      count += 1;
      localStorage.setItem(INSTALL_LAST_SEEN_DAY_KEY, today);
      localStorage.setItem(INSTALL_DAY_COUNT_KEY, String(count));
    }
    return count;
  } catch (err) {
    // Fail open: treat storage-blocked visitors as already past the
    // "wait a few days" gate rather than never showing the banner at all.
    return INSTALL_BANNER_MIN_DAYS;
  }
}

var installPromptDayCount = 0;

// True once running as an installed app (standalone window), on either
// path: matchMedia covers Chromium/Android, navigator.standalone is the
// iOS Safari equivalent (no matching media feature there).
function installPromptIsStandalone() {
  return (
    (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) ||
    window.navigator.standalone === true
  );
}

// Plain user-agent detection for iOS Safari specifically - not Chrome/
// Firefox-on-iOS (which are still just Safari's WebKit under the hood and
// never get an install path either, but showing Safari-specific "tap the
// Share icon" copy in a browser whose chrome doesn't have that icon in
// that place would be actively wrong) and not iPadOS 13+, which reports a
// desktop Mac user agent - distinguished by touch support.
function installPromptIsIOSSafari() {
  var ua = window.navigator.userAgent;
  var isIOSDevice = /iPad|iPhone|iPod/.test(ua) && !window.MSStream;
  var isIPadOS = /Macintosh/.test(ua) && "ontouchend" in document;
  var isSafari = /Safari/.test(ua) && !/CriOS|FxiOS|EdgiOS|OPiOS|Chrome|Android/.test(ua);
  return (isIOSDevice || isIPadOS) && isSafari;
}

function installPromptSetHidden(id, hide) {
  var el = document.getElementById(id);
  if (el) {
    el.hidden = !!hide;
  }
}

// Reconciles every install-UI element (both the Account page section and
// the dismissible banner) with current state. Safe to call repeatedly -
// on initial load, again whenever beforeinstallprompt actually fires
// (which can happen after DOMContentLoaded), and again once installed.
function installPromptRefreshUI() {
  var standalone = installPromptIsStandalone();
  var iosEligible = !standalone && installPromptIsIOSSafari();
  var chromiumEligible = !standalone && deferredInstallPrompt !== null;
  var anyPathEligible = iosEligible || chromiumEligible;

  // Account page section: always the findable, deliberate home for this
  // feature once installing is actually possible here at all - not gated
  // on the banner's dismissal/day-count logic below.
  installPromptSetHidden("account-install-button", !chromiumEligible);
  installPromptSetHidden("account-install-ios-copy", !iosEligible);
  var accountSection = document.getElementById("account-install-section");
  if (accountSection) {
    accountSection.hidden = !anyPathEligible;
  }

  // Dismissible banner: same per-path button/copy visibility, plus the
  // "not on first visit, not every visit once dismissed" gate.
  installPromptSetHidden("install-banner-button", !chromiumEligible);
  installPromptSetHidden("install-banner-ios-copy", !iosEligible);
  var banner = document.getElementById("install-prompt-banner");
  if (banner) {
    var bannerEligible =
      anyPathEligible &&
      installPromptDayCount >= INSTALL_BANNER_MIN_DAYS &&
      installPromptShouldShow();
    banner.hidden = !bannerEligible;
  }
}

// Fires the actual Chromium install prompt from our own button - source
// is just for a future hook (e.g. distinguishing which affordance was
// used) and unused today.
function handleInstallPromptClick(source) {
  if (!deferredInstallPrompt) {
    return;
  }
  var promptEvent = deferredInstallPrompt;
  promptEvent.prompt();
  promptEvent.userChoice
    .catch(function () {
      // No-op: a rejected userChoice promise (rare) still means the
      // one-shot prompt event is spent either way - fall through to the
      // same cleanup as a normal accept/dismiss below.
    })
    .then(function () {
      deferredInstallPrompt = null;
      installPromptRefreshUI();
    });
}

function dismissInstallBanner() {
  installPromptRecordDismissal();
  installPromptSetHidden("install-prompt-banner", true);
}

// preventDefault suppresses the browser's own passive mini-infobar/banner,
// deferring the install moment until this app's own UI calls .prompt() -
// see plan doc section 1. Registered immediately (not on DOMContentLoaded)
// so an event firing early is never missed.
window.addEventListener("beforeinstallprompt", function (e) {
  e.preventDefault();
  deferredInstallPrompt = e;
  installPromptRefreshUI();
});

// Fires once genuinely installed (Chromium path only - iOS has no
// equivalent event, but installPromptIsStandalone() covers it on the next
// load via navigator.standalone). The affordance should simply stop
// appearing at this point, dismissal timer or not.
window.addEventListener("appinstalled", function () {
  deferredInstallPrompt = null;
  installPromptRefreshUI();
});

document.addEventListener("DOMContentLoaded", function () {
  installPromptDayCount = installPromptBumpDayCount();
  installPromptRefreshUI();
});
