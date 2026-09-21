// scheduling-song-library.js - the Song Library index page's own card
// filtering (filterSongLibraryCards), the song-side counterpart of
// scheduling-song-tags-contexts.js's filterSongTagCards/filterSongContextCards.
// Split out as its own file rather than folded into that one since this
// page (song_library.templ) is a single free-text filter over a single
// data-search string (title + role/context labels + tag count), not that
// page's combined text-plus-checked-chips filtering - see
// docs/plans/not-started/song-person-detail-pages-plan.md.

// filterSongLibraryCards backs the Song Library index page's
// #song-library-filter text box: a card matches when its data-search
// (songLibraryCardSearchText, song_library.templ) contains the typed
// query as a substring, case-insensitively (data-search is already
// lowercased at render time; lowercase the query here to match). Purely
// client-side, hiding/showing each already-rendered .song-library-card -
// every active song renders unconditionally on page load (same "cheap
// regardless of library size" reasoning as Song Tags/Song Contexts), so
// there's nothing to fetch here, just hide/show against data already in
// the DOM.
function filterSongLibraryCards() {
  var input = document.getElementById("song-library-filter");
  var query = input ? input.value.trim().toLowerCase() : "";

  document.querySelectorAll(".song-library-card").forEach(function (card) {
    var matches = query === "" || (card.dataset.search || "").indexOf(query) !== -1;
    card.hidden = !matches;
  });
}
