package scheduling

// The set builder page pulls together data from several sources (the DB
// store, live PCO plan/song data, and role/relationship lookups) that the
// controller has to resolve before rendering - these plain view-model types
// carry that already-resolved data into the templates. They live here
// rather than in the controllers package so the templ files can reference
// them without an import cycle (controllers already imports this package).

type SetBuilderData struct {
	ServiceTypeID string
	PlanID        string
	PlanTitle     string
	PlanDates     string
	ErrMsg        string
	// PublishMsg is a status line for the last "Publish to Planning Center"
	// attempt (e.g. "Published 3 song(s)."), separate from ErrMsg so a
	// partial publish can show both what succeeded and what didn't.
	PublishMsg string

	// SetSongs is the flat list of this set's songs (any anchor group,
	// ordered by position) - what Recommendations/Candidates/conflict
	// logic care about. ServiceOrder is the fuller, displayed view: every
	// item PCO has for this plan, song or not, interleaved with these same
	// songs at their anchored position - see buildServiceOrder.
	SetSongs          []SetSongView
	ServiceOrder      []ServiceOrderRow
	Suggestions       []SuggestionView
	Candidates        []CandidateSongView
	ExistingConflicts []string

	// RepeatWarnings flags songs already in this set that were also sung in
	// each of the last two past plans for this service type - see the
	// controller's recentlyRepeatedSongs. Rendered the same way as
	// ExistingConflicts, just a different reason.
	RepeatWarnings []string

	// UnpublishedCount is how many SetSongs still need a "Publish" push -
	// creates and reordering. PendingDeleteCount is how many
	// removed/replaced-but-previously-published songs still need their PCO
	// item deleted. Either drives whether the Publish button renders, and
	// its confirmation text.
	UnpublishedCount   int
	PendingDeleteCount int
}

type SetSongView struct {
	// ID is the set_songs row id - a song can appear more than once in a
	// set now (see DuplicateSongInSet), so this row id, not SongID, is
	// what identifies which occurrence an action (remove/replace/
	// reposition/duplicate) targets.
	ID         int64
	SongID     string
	Title      string
	Position   int
	IsOpener   bool
	IsResponse bool
	// Published is true once this song has been pushed to Planning Center
	// as a real plan item (see the scheduling.SetSong.PCOItemID it comes
	// from).
	Published bool
	// AnchorItemID is which item (by PCO item id, "" for top of service)
	// this song is currently placed after - carried as a data attribute so
	// the order-of-service drag-and-drop can compute new positions without
	// a round trip (see web/static/scheduling.js).
	AnchorItemID string
}

// ServiceOrderRow is one row of the full order of service shown on the set
// builder page - either a plain PCO item this tool doesn't manage (IsSong
// false: a header, media, or a song from outside this local set) shown
// read-only for context, or one of this set's own songs (IsSong true) with
// its full row of controls.
type ServiceOrderRow struct {
	IsSong bool

	// Set when IsSong is false.
	ItemID    string
	ItemTitle string
	ItemType  string

	// Set when IsSong is true.
	Song SetSongView
}

// PlanRow is one row of the Sets page's plan-picker list. Status is one of
// "" (nothing local, and Planning Center has no songs here either),
// "todo" (Planning Center already has song content but there's no local
// draft yet), "pending" (a local draft exists with unpublished or
// pending-deletion songs), or "planned" (a local draft exists and is fully
// synced) - see the scheduling controller's planStatus.
type PlanRow struct {
	ID     string
	Title  string
	Dates  string
	Status string
}

// CandidateSongView is a song not yet in the set, offered for adding via
// the search/role-filter list. ConflictTitles lists the titles of set songs
// it has an avoid_pairing relationship with (empty when there's no
// conflict); RolesAttr is a space-separated role list ("opener response")
// for the candidate list's client-side filtering - see filterCandidates in
// web/static/scheduling.js.
type CandidateSongView struct {
	SongID         string
	Title          string
	IsOpener       bool
	IsResponse     bool
	ConflictTitles []string
	// RecentlyRepeated is true when this song was sung in each of the last
	// two past plans for this service type (see the controller's
	// recentlyRepeatedSongs) - a nudge to think twice before adding it a
	// third time in a row.
	RecentlyRepeated bool
	RolesAttr        string
}

type SuggestionView struct {
	SongID string
	Title  string
	// RelationshipLabel is every reason this song is suggested, already
	// joined for display (e.g. "flows_well, same_theme") - a song related
	// to the anchor song by more than one relationship type is one
	// suggestion with both reasons, not a separate suggestion per type.
	RelationshipLabel string
}
