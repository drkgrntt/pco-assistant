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

// The header nav's "Setup" dropdown (see layout.templ) - a plain
// open/closed toggle, closed by clicking its button again, clicking
// anywhere else on the page, or pressing Escape.
function toggleNavDropdown(button) {
  var dropdown = button.closest(".nav-dropdown");
  var open = dropdown.classList.toggle("open");
  button.setAttribute("aria-expanded", open ? "true" : "false");
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

document.addEventListener("click", function (event) {
  if (!event.target.closest(".nav-dropdown")) {
    closeNavDropdowns();
  }
});

document.addEventListener("keydown", function (event) {
  if (event.key === "Escape") {
    closeNavDropdowns();
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

  // afterRequest covers both a normal response and an HTTP error status;
  // sendError/timeout cover the request never getting a response at all
  // (offline, connection refused, timed out) - either way the button needs
  // to come back, or a real failure would leave it disabled forever.
  document.addEventListener("htmx:afterRequest", requestSettled);
  document.addEventListener("htmx:sendError", requestSettled);
  document.addEventListener("htmx:timeout", requestSettled);
})();

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
  document.getElementById("person-search-results" + suffix).innerHTML = "";
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

  var fields = JSON.parse(button.dataset.editFields || "{}");
  Object.keys(fields).forEach(function (name) {
    var el = form.elements.namedItem(name);
    if (el) {
      el.value = fields[name];
    }
  });

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

  form.scrollIntoView({ behavior: "smooth", block: "start" });
}

function cancelSetupEdit(button) {
  resetSetupForm(button.closest("form"));
}

// Client-side filtering for the set builder's "find songs to add" list (see
// set_builder.templ) - the whole candidate pool is already rendered on the
// page (song libraries are small enough to fetch in full, unlike people),
// so filtering by typed text and/or an active role button doesn't need a
// round trip. Typed text matches either the song title or its tags
// (data-tags - see CandidateSongView.TagsAttr), so searching a sermon theme
// or scripture reference surfaces songs tagged with it even when the title
// itself doesn't mention it.
function filterCandidates() {
  var search = (document.getElementById("candidate-search").value || "").toLowerCase();
  var activeFilter = document.querySelector(".role-filter.active");
  var role = activeFilter ? activeFilter.dataset.role : "";

  document.querySelectorAll("#set-candidates .candidate-row").forEach(function (row) {
    var haystack = row.dataset.title.toLowerCase() + " " + (row.dataset.tags || "").toLowerCase();
    var matchesSearch = !search || haystack.indexOf(search) !== -1;
    var matchesRole = !role || (" " + row.dataset.roles + " ").indexOf(" " + role + " ") !== -1;
    row.style.display = matchesSearch && matchesRole ? "" : "none";
  });
}

// Role filter buttons behave like a toggle: clicking the active one turns
// it back off, clicking another switches to it. Only one is active at a
// time since a song's roles are shown, not exclusively filtered by an
// AND/OR combination.
function toggleRoleFilter(el) {
  var wasActive = el.classList.contains("active");
  document.querySelectorAll(".role-filter").forEach(function (btn) {
    btn.classList.remove("active");
  });
  if (!wasActive) {
    el.classList.add("active");
  }
  filterCandidates();
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
    if (rowToken(draggedRow.previousElementSibling) === after) {
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
    dragOverRow = null;
    row.classList.add("dragging");

    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", onPointerUp);
    document.addEventListener("pointercancel", endDrag);
  });
})();
