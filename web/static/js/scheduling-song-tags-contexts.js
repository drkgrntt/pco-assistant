// scheduling-song-tags-contexts.js - the Song Tags and Song Contexts setup
// pages' own card filtering (filterSongTagCards/filterSongContextCards),
// bulk-selection counts, and the Song Tags filter-chips' out-of-band-swap
// checked-state preservation. Split out of the former single
// scheduling.js - see scheduling-global.js's header for the same note.

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

// filterSongContextCards backs the Song Contexts setup page's own
// "Tag a song" filter box (song_contexts.templ): the free-text
// #song-context-filter box, applied against each already-rendered
// .song-context-card's data-search (song title + its context labels,
// substring match, see songContextCardSearchText). Purely client-side,
// like filterSongTagCards above - but with no tag-value-chip half to
// combine it with: a song's contexts come from a small, org-named list
// (typically a handful), not free-text values worth browsing by chip the
// way dozens of scripture-reference tag values are, so there's nothing
// equivalent to filterSongTagCards' checked-chips intersection here.
function filterSongContextCards() {
  var input = document.getElementById("song-context-filter");
  var query = input ? input.value.trim().toLowerCase() : "";

  document.querySelectorAll(".song-context-card").forEach(function (card) {
    card.hidden = query !== "" && (card.dataset.search || "").indexOf(query) === -1;
  });
}

// Re-applies the filter above after any htmx swap of the card list itself
// (adding/editing/removing a context, or a bulk add, all swap the whole
// #song-contexts-list) - without this, a filtered view would flash back
// to showing every song again after each mutation, even though
// #song-context-filter (outside the swapped div, so untouched by the
// swap) still shows its previous state. Also resets
// #song-context-bulk-count, mirroring the song-tags-list handler above -
// every .song-context-bulk-checkbox lives inside this same swapped div,
// so any mutation here always resets them all to unchecked.
document.addEventListener("htmx:afterSwap", function (event) {
  var target = event.detail && event.detail.target;
  if (!target || target.id !== "song-contexts-list") {
    return;
  }
  filterSongContextCards();
  updateSongContextBulkCount();
});

// updateSongContextBulkCount keeps #song-context-bulk-count
// (song_contexts.templ) in sync with how many of the page's
// .song-context-bulk-checkbox boxes are currently checked - called from
// each checkbox's own onchange, and from the #song-contexts-list
// afterSwap handler above, mirroring updateSongTagBulkCount exactly.
function updateSongContextBulkCount() {
  var countEl = document.getElementById("song-context-bulk-count");
  if (!countEl) {
    return;
  }
  var count = document.querySelectorAll(".song-context-bulk-checkbox:checked").length;
  countEl.textContent = count === 1 ? "1 song selected" : count + " songs selected";
}

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
