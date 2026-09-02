package scheduling

// SongInfoData is the Song Info page's view model - a small status/action
// page, not a table of every song, since the whole point is "here's how
// much of your library is cached, here's a button to fix the rest" rather
// than a per-song review workflow (that's what the set builder's own
// candidate list is for, once this cache backs it - see
// CandidateSongView.ArrangementInfo).
type SongInfoData struct {
	CachedCount int
	TotalCount  int
	// SyncInProgress reports whether a background sync (see SyncSongInfo/
	// runSongInfoSync in internal/server/controllers/
	// song_arrangement_cache.go) is currently running for this
	// organization - checked against the same in-memory guard that
	// prevents a second concurrent sync. Used to hide the "Sync now" button
	// (starting a second pass would just be rejected anyway) and show a
	// "sync in progress" message instead of a stale SyncMsg from before the
	// sync was kicked off.
	SyncInProgress bool
	// SyncMsg reports the outcome of the last "Sync now" click - either
	// that a background sync was just started, that one was already
	// running, or an error - since the sync itself now runs in the
	// background and its actual per-song results aren't known by the time
	// this renders (see runSongInfoSync's doc comment). CachedCount/
	// TotalCount above are what actually reflect progress, live, on every
	// render.
	SyncMsg string
	ErrMsg  string
}
