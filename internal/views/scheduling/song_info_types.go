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
	// SyncMsg reports the outcome of the last "Sync now" action (e.g.
	// "Synced 12 song(s): 3 newly cached, 9 already cached, 0 failed."),
	// same pattern as every other setup page's success line.
	SyncMsg string
	ErrMsg  string
}
