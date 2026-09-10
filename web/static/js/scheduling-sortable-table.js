// scheduling-sortable-table.js - generic click-a-header-to-sort wiring for
// the admin tables (song/person relationships, preferences, song roles) -
// makeSortable/sortTableByColumn plus the DOMContentLoaded/htmx:afterSwap
// rewiring (wireSortableTables), since those tables get replaced wholesale
// on every swap. Deliberately not used on the set builder's order-of-
// service table, where row order is real data, not a display convenience -
// see scheduling-build.js for that table's own pointer-based drag
// reordering. Split out of the former single scheduling.js - see
// scheduling-global.js's header for the same note.

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
