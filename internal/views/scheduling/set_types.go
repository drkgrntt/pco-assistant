package scheduling

import (
	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

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

	// SongsToReinforce lists candidates that are genuinely new to the
	// church (see tracked_new_songs/TrackNewSong) and have only been sung
	// once or twice so far (see PublishedSongAppearanceCounts) - the
	// opposite nudge from RepeatWarnings below: not "this repeated too
	// soon" but "this hasn't stuck yet." Reuses SuggestionView (its
	// RelationshipLabel carries the reason, e.g. "Sung once - one more to
	// make it stick") rather than a new type. Deliberately its own list,
	// not merged into Suggestions: Suggestions is anchored to the last
	// song in the set and would stay empty on an empty set, but a
	// still-new song deserves a nudge regardless of what's already in the
	// set (or if there's nothing in it yet).
	SongsToReinforce []SuggestionView

	// RepeatWarnings flags songs already in this set that were also sung in
	// each of the last two past plans for this service type - see the
	// controller's fetchRecentPlanHistory. Rendered the same way as
	// ExistingConflicts, just a different reason.
	RepeatWarnings []string

	// PlanWarnings covers the whole plan rather than any one song or
	// pairing: today, a total length that's noticeably longer/shorter than
	// this service type's recent average (serviceLengthWarning), and a
	// half-step key change between two back-to-back songs with nothing
	// between them (keyTransitionWarnings). Rendered the same way as
	// RepeatWarnings/ExistingConflicts, just a different reason - kept as
	// its own field rather than folded into RepeatWarnings since neither
	// check is actually about repetition.
	PlanWarnings []string

	// UnpublishedCount is how many SetSongs still need a "Publish" push -
	// creates and reordering. PendingDeleteCount is how many
	// removed/replaced-but-previously-published songs still need their PCO
	// item deleted. Either drives whether the Publish button renders, and
	// its confirmation text.
	UnpublishedCount   int
	PendingDeleteCount int
	// ReorderPending is true when a song already published to Planning
	// Center has been moved locally (drag-and-drop) since the last
	// publish - see scheduling.Set.ReorderPending. Neither
	// UnpublishedCount nor PendingDeleteCount catches this case (the moved
	// song is still published, and nothing was removed), so without this
	// the "Discard changes" button never appeared for a pure reorder even
	// though there was a real local change to discard or publish.
	ReorderPending bool
	// InfoPendingCount is how many already-published SetSongs have a
	// Description or Key edit that hasn't reached Planning Center yet -
	// see scheduling.SetSong.InfoPending, the item-content equivalent of
	// ReorderPending above, and for the same reason: a note/key edit alone
	// doesn't touch UnpublishedCount, PendingDeleteCount, or
	// ReorderPending, so without this "Discard changes" wouldn't appear
	// for a real local change sitting there unpushed.
	InfoPendingCount int
	// PendingChanges spells out exactly what's changed since this set was
	// last published, one line per change (e.g. "Added: Amazing Grace",
	// "Song order changed") - see buildSetBuilderData. Empty means fully
	// in sync with Planning Center.
	PendingChanges []string

	// ImportableSongCount is how many of this plan's Planning Center songs
	// aren't reflected here yet - always 0 once this set has anything of its
	// own (see buildSetBuilderData), so it only ever fires for a plan that
	// was already built out directly in Planning Center and never opened
	// here before. Drives the "import from Planning Center" prompt in
	// SetBuilderBody, so someone can start editing an already-planned
	// service instead of just seeing it read-only.
	ImportableSongCount int

	// SongPairPrompt, when set, offers a one-shot inline "remember that
	// these two go together?" relationship capture right after adding a
	// song that landed right after another one - see SongPairPromptView
	// and AddSetSong (sets.go), the only handler that ever sets this.
	SongPairPrompt *SongPairPromptView

	// AllSongs/TagCategories/RelationshipTypes/RoleTypes back the "Quick
	// add" panel's <select> pickers (SetBuilderBody) - letting someone tag a
	// song, relate two songs, or mark an opener/response right from the
	// build page instead of a dedicated setup page. AllSongs is the same
	// slice buildSetBuilderData already fetches via fetchAllSongs for the
	// candidate list, just also exposed here so the quick-add forms don't
	// need a song picker of their own; TagCategories is likewise the same
	// slice already fetched for candidate tag badges. RelationshipTypes and
	// RoleTypes aren't otherwise needed by this page (recommendations read
	// relationship/role data directly, not through these lookup tables), so
	// they're fetched only for this panel.
	AllSongs          []pco.SongData
	TagCategories     []scheduling.SongTagCategory
	RelationshipTypes []scheduling.SongRelationshipType
	RoleTypes         []scheduling.SongRoleType
}

type SetSongView struct {
	// ID is the set_songs row id - a song can appear more than once in a
	// set now (see DuplicateSongInSet), so this row id, not SongID, is
	// what identifies which occurrence an action (remove/replace/
	// reposition/duplicate) targets.
	ID       int64
	SongID   string
	Title    string
	Position int
	// Roles is this song's assigned roles (opener, response, communion,
	// etc. - see SongRoleView), shown as badges next to its order-of-service
	// row. Purely descriptive/display - see song_roles.go for why no role
	// code gets special Go-side treatment anymore.
	Roles []SongRoleView
	// Published is true once this song has been pushed to Planning Center
	// as a real plan item (see the scheduling.SetSong.PCOItemID it comes
	// from).
	Published bool
	// AnchorItemID is which item (by PCO item id, "" for top of service)
	// this song is currently placed after - carried as a data attribute so
	// the order-of-service drag-and-drop can compute new positions without
	// a round trip (see web/static/js/scheduling.js).
	AnchorItemID string
	// Moved is true when this song has been dragged to a new position
	// since it was last published - see scheduling.SetSong.Moved. Shown as
	// a badge right on the row, since "the order changed" without saying
	// which songs moved is easy to lose track of.
	Moved bool
	// InfoPending is true once Description or Key has been edited on an
	// already-published song without that edit reaching Planning Center
	// yet - see scheduling.SetSong.InfoPending, the item-content
	// equivalent of Moved above. Shown as its own badge for the same
	// reason Moved is: without it, an edited-but-not-yet-refreshed song
	// looked identical to one already in sync, with nothing anywhere on
	// the page saying so until the numbers underneath happened to change.
	InfoPending bool
	// ArrangementInfo is a pre-formatted "G · 72 BPM · 4/4 · 3:45" summary
	// of this song's Planning Center arrangement (whatever PCO's own "add
	// song" flow set up, dbseed/UpdateArrangement set, or a manual Song
	// Info sync backfilled - see resolveArrangementInfo), empty when
	// there's no arrangement or every field on it is blank. Read from the
	// song_arrangement_cache table when cached; a set song not yet cached
	// still gets a live Planning Center fetch (and is cached from then on)
	// since a set has at most a handful of songs - see
	// CandidateSongView.ArrangementInfo for why the full candidate list
	// doesn't get the same live-fetch fallback.
	ArrangementInfo string
	// Description is this one occurrence's local per-occurrence note (see
	// scheduling.SetSong.Description) - "cut the bridge this week," not a
	// persistent fact about the song. Editing this only updates the local
	// draft (UpdateSetSongDescription); it reaches Planning Center on the
	// next Publish or Refresh song info, same as every other local set
	// change.
	Description string
	// Key is this song's effective key for this one occurrence - the local
	// per-occurrence override (scheduling.SetSong.KeyOverride) when set,
	// falling back to the arrangement's own cached default key (the same
	// one ArrangementInfo's summary line shows) otherwise. Editable on the
	// song detail card; like Description, it only updates the local draft
	// and reaches Planning Center as a Key relationship on the next
	// Publish or Refresh song info (see resolveKeyID, scheduling.go) -
	// never the arrangement's own default key, unlike editing it directly
	// in Planning Center would.
	Key string
	// PCOItemID is this song's real Planning Center plan item id, once
	// published ("" otherwise) - see scheduling.SetSong.PCOItemID, which
	// Published already derives from. Exposed here (unlike AnchorItemID,
	// which is a different item entirely - wherever this song is placed
	// after) because the song detail sidebar card needs it directly, to
	// post a note against *this* item (see Notes/SongNoteView below).
	PCOItemID string
	// Notes is one entry per Planning Center note category (Audio/Visual,
	// Band, Vocals, etc. - configured per service type, see
	// pco.ItemNoteCategory) already resolved against whatever note this
	// song's plan item actually has in each one - not the same thing as
	// the song's Description above, or its arrangement's own persistent
	// Notes (which this app doesn't expose an editor for - it isn't the
	// notes surface anyone actually looks at in Planning Center's own UI).
	// Only populated for a published song (PCOItemID != "") - there's no
	// plan item to attach a note to otherwise; nil for an unpublished one,
	// which the song detail card takes as "publish this first."
	Notes []SongNoteView
}

// SongNoteView is one Planning Center note category's state for a specific
// published song - see SetSongView.Notes. NoteID is "" when this category
// has no note on this item yet (the detail card creates one on save
// instead of updating); Content is "" in that case too.
type SongNoteView struct {
	CategoryID   string
	CategoryName string
	NoteID       string
	Content      string
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

// PlanRow is one row of the Build page's plan-picker list - one row per
// plan, with independent statuses for its song lineup and its team, since
// a plan can be e.g. fully planned on songs but still todo on people.
// Each status is one of "" (nothing local, and Planning Center has no
// content here either), "todo" (Planning Center already has content but
// there's no local draft yet), "pending" (a local draft exists with
// unpublished and/or pending-deletion rows), or "planned" (a local draft
// exists and is fully synced) - see the scheduling controller's
// planStatus/rosterStatus.
type PlanRow struct {
	ID           string
	Title        string
	Dates        string
	SetStatus    string
	RosterStatus string
}

// CandidateSongView is a song not yet in the set, offered for adding via
// the search/role-filter list. ConflictTitles lists the titles of set songs
// it has an avoid_pairing relationship with (empty when there's no
// conflict); RolesAttr is a space-separated role list ("opener response")
// for the candidate list's client-side filtering - see filterCandidates in
// web/static/js/scheduling.js.
type CandidateSongView struct {
	SongID         string
	Title          string
	ConflictTitles []string
	// RecentlyRepeated is true when this song was sung in each of the last
	// two past plans for this service type (see the controller's
	// recentlyRepeatedSongs) - a nudge to think twice before adding it a
	// third time in a row.
	RecentlyRepeated bool
	// NewSongLabel is a short badge ("Sung once"/"Sung twice") for a song
	// that's genuinely new to the church and hasn't been sung enough times
	// to stick yet - see SetBuilderData.SongsToReinforce, which this
	// mirrors for the candidate list specifically. "" means not applicable
	// (not tracked as new, never sung, or already graduated past
	// reinforcement).
	NewSongLabel string
	// ArrangementInfo mirrors SetSongView.ArrangementInfo, but is populated
	// cache-only (see buildSetBuilderData's Candidates loop) - "" whenever
	// this song hasn't been cached yet (never added to a set, and not
	// covered by a Song Info sync), not fetched live for the whole
	// candidate library the way a set song's is.
	ArrangementInfo string
	// Roles is this song's assigned roles, for display as badges - see
	// SetSongView.Roles, which this mirrors. RolesAttr is the same roles'
	// codes, space-joined, as a data attribute for the candidate list's
	// client-side role-filter buttons and search - see filterCandidates in
	// web/static/js/scheduling.js.
	Roles     []SongRoleView
	RolesAttr string
	// Tags is this song's theme/scripture/idea tags (see song_tags.go), for
	// display as badges. TagsAttr is the same tags' values, space-joined and
	// lowercased, as a data attribute for the candidate list's client-side
	// search - see filterCandidates in web/static/js/scheduling.js, which
	// matches typed text against title or tags the same way RolesAttr
	// already backs the role-filter buttons.
	Tags     []SongTagView
	TagsAttr string
}

// SongTagView is one tag on a candidate song, already resolved to its
// display category label (see songTagCategoryLabelMap) rather than the raw
// category code.
type SongTagView struct {
	Category string
	Value    string
}

// SongRoleView is one role assigned to a song (opener, response, communion,
// etc. - see song_role_types), already resolved to its display label (see
// songRoleTypeLabelMap in the controller) rather than the raw code - the
// role equivalent of SongTagView. No role code gets special treatment
// here; the set builder's role-filter buttons are generated straight from
// SetBuilderData.RoleTypes, so a role added to song_role_types (no code
// change needed) is filterable and visible immediately, not just storable.
type SongRoleView struct {
	Code  string
	Label string
}

// SuggestionView is a not-yet-added song recommended for a specific reason
// (RelationshipLabel) - "Recommended next" (SetBuilderData.Suggestions) and
// "Could use one more sing" (SetBuilderData.SongsToReinforce). Embeds
// CandidateSongView so both lists can show the same arrangement/role/tag
// metadata the plain candidate list does (see set_builder.templ's
// songCandidateMeta, the block shared by all three lists) instead of just a
// bare title + reason - SongID/Title come along with the embed, so callers
// keep writing sug.SongID/sug.Title exactly as before.
type SuggestionView struct {
	CandidateSongView
	// RelationshipLabel is every reason this song is suggested, already
	// joined for display (e.g. "flows_well, same_theme") - a song related
	// to the anchor song by more than one relationship type is one
	// suggestion with both reasons, not a separate suggestion per type.
	RelationshipLabel string
}

// SongPairPromptView is SetBuilderData.SongPairPrompt's payload - the two
// songs a "remember that these go together?" prompt is offering to relate,
// already resolved to their titles for display.
type SongPairPromptView struct {
	PreviousSongID    string
	PreviousSongTitle string
	NewSongID         string
	NewSongTitle      string
}
