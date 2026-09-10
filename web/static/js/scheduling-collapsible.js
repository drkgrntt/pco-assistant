// scheduling-collapsible.js - the generic collapsible-section open/close
// mechanism (collapsibleSection/collapsibleCard in the scheduling views
// package, and roster_builder.templ's position-group cards): toggleCollapse
// plus its localStorage-backed applyStoredCollapsibleState restore on
// initial page load. Split out of the former single scheduling.js - see
// scheduling-global.js's header for the same note.

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
