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
	// PrevPlanID/NextPlanID - see RosterBuilderData's fields of the same
	// name, which this mirrors exactly.
	PrevPlanID string
	NextPlanID string
	ErrMsg     string
	// ReadOnly is true once this plan's own sort_date has already passed
	// (see buildSetBuilderData) - the past-plans archive (BuildPlansList's
	// past tab) reuses this same builder rendering, just with every
	// add/remove/move/tag/relate/publish/discard action gated off, since a
	// plan that's already happened has nothing left to build toward.
	// Server-derived from the plan's own PCO date, never a client-supplied
	// flag, so it can't be bypassed and stays correct even for a plan that
	// was still upcoming when first opened here.
	ReadOnly bool
	// PublishMsg is a status line for the last "Publish to Planning Center"
	// attempt (e.g. "Published 3 song(s)."), separate from ErrMsg so a
	// partial publish can show both what succeeded and what didn't.
	PublishMsg string

	// SetSongs is the flat list of this set's songs (any anchor group,
	// ordered by position) - what Recommendations/Candidates/conflict
	// logic care about. ServiceOrder is the fuller, displayed view: every
	// item PCO has for this plan, song or not, interleaved with these same
	// songs at their anchored position - see buildServiceOrder.
	SetSongs     []SetSongView
	ServiceOrder []ServiceOrderRow
	Suggestions  []SuggestionView

	// ReplaceCandidates is every active, not-yet-in-this-set song's id+
	// title - just enough to populate the replace-form <select> dropdowns
	// (this song's own row action in set_builder.templ, and the song
	// detail card's sidebar action in song_detail_card.templ - both share
	// this same field). Deliberately the lightweight SongOptionView, not
	// the richer CandidateSongView the "Browse all songs" list uses (see
	// CandidateSearchResults/BuildCandidatesSearch) - a plain native
	// <select> only needs an id and a title to work as a picker, so this
	// stays cheap to populate for the whole library on every render,
	// unlike that search-driven list - see buildSetBuilderData.
	ReplaceCandidates []SongOptionView

	// SongsToReinforce lists candidates that are genuinely new to the
	// church (see tracked_new_songs/TrackNewSong) and have only been sung
	// once or twice so far (see PublishedSongAppearanceCounts) - the
	// opposite nudge from SetSongView.RecentlyRepeated: not "this repeated
	// too soon" but "this hasn't stuck yet." Reuses SuggestionView (its
	// RelationshipLabel carries the reason, e.g. "Sung once - one more to
	// make it stick") rather than a new type. Deliberately its own list,
	// not merged into Suggestions: Suggestions is anchored to the last
	// song in the set and would stay empty on an empty set, but a
	// still-new song deserves a nudge regardless of what's already in the
	// set (or if there's nothing in it yet).
	SongsToReinforce []SuggestionView

	// ServiceLengthWarning is an advisory message when this plan's total
	// length is noticeably longer/shorter than this service type's recent
	// average (see the controller's serviceLengthWarning) - "" means
	// nothing to flag. The one warning check that stays a standalone,
	// whole-plan banner rather than moving onto a song's own row (see
	// SetSongView.RecentlyRepeated/ConflictTitles/KeyTransitionLabel
	// below): it isn't attributable to any one song or pair, just the set
	// as a whole. See docs/plans/shipped/in-place-set-warnings-plan.md for
	// why the other three warning checks that used to live in a
	// top-of-page list (ExistingConflicts, RepeatWarnings, and the other
	// half of what used to be PlanWarnings, keyTransitionWarnings) now
	// render inline on the row(s) they're actually about instead.
	ServiceLengthWarning string

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

	// MergeAvailableCount is how many of this plan's Planning Center songs
	// aren't tracked by any local row yet (active or tombstoned) - unlike
	// ImportableSongCount, this stays meaningful after the set already has
	// its own songs, since it's tracking songs added directly in Planning
	// Center *since* the local draft was last touched, not just "does this
	// set have anything of its own at all." Computed the same way MergeSet
	// itself would (see deriveMergeImportRows), reusing the same pcoItems
	// fetch buildSetBuilderData already does for ServiceOrder - no extra
	// PCO call. Gates the "Pull in from Planning Center" button in
	// publish_panel.templ, so it only shows when it would actually do
	// something.
	MergeAvailableCount int

	// PCOSyncFingerprint is a hash of every plan item's (id, sequence,
	// updated_at) as of this render (see pcoItemsFingerprint,
	// sets_builder.go) - the poll-while-open live-sync check's baseline
	// (docs/plans/in-progress/live-sync-reflection-plan.md). The hidden
	// #pco-sync-fp input in set_builder.templ carries this value; a
	// background poll every 30s (SyncCheck, sets_publish.go) refetches the
	// plan's items, recomputes the same fingerprint, and only shows a
	// "Planning Center has changes" banner when the two differ. Reuses the
	// exact GetItems response ServiceOrder is built from (see
	// buildSetBuilderData) - no extra PCO call, same reasoning as
	// MergeAvailableCount above. Left "" when that GetItems call failed
	// (see ErrMsg) - the poll then simply won't match until the next
	// successful render repopulates it, same best-effort tolerance as
	// everything else built from that response.
	PCOSyncFingerprint string

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
	// ContextTypes is this org's own list of song contexts ("Big church,"
	// "Youth," ...) - see song_contexts.go and RoleTypes' own doc comment
	// above, which this mirrors exactly: backs both the candidate list's
	// context-filter chips (set_builder.templ) and the quick-add panel's
	// "Tag with context" picker. Empty until the org has named at least one
	// context on the Song Contexts setup page - unlike RoleTypes, there are
	// no seeded defaults (see docs/plans/shipped/song-context-tags-plan.md).
	ContextTypes []scheduling.SongContextType

	// RosterPersonOptions is who can be picked as a song leader
	// (SetSongView.LeaderPersonID) - this plan's own local roster
	// assignments (scheduling.Store.ListRosterAssignments), the same
	// source buildSetBuilderData already reads for the vocal-range
	// mismatch warning. Deliberately not a general org-wide person search
	// the way every other PersonPickerField on this app is: PCO's real
	// "Song leader" resource (ItemAssignment) rejects assigning anyone who
	// isn't already scheduled on this plan (confirmed live - see
	// leaderForm's own doc comment, song_detail_card.templ), so offering
	// a wider pool here would just be letting someone pick a choice PCO
	// is guaranteed to reject on the next Publish/Refresh. Empty when
	// nothing's been added to this plan's Team tab yet.
	RosterPersonOptions []RosterPersonOptionView

	// AllSongVocalRanges is the same song id -> stored range map
	// buildSetBuilderData already fetches for the detail card/mismatch
	// warning (vocalRangesBySong), also exposed here so the quick-add
	// panel's "Set song vocal range" form can pre-fill from whichever song
	// its own picker selects (fillVocalRangeFromSongSelect, scheduling-setup-forms.js,
	// reading each <option>'s own vocalRangeFormFields-built
	// data-edit-fields attribute) - see SetSongView.VocalRange's doc
	// comment for why this matters. A song id missing from this map simply
	// has no range on file yet.
	AllSongVocalRanges map[string]scheduling.SongVocalRange

	// TagValuesByCategory backs the "browse by tag" chips (SetBuilderBody,
	// next to the role-filter row) - category code -> every distinct value
	// actually tagged on some song in this org, sourced from
	// DistinctSongTagValues. Purely a discoverability shortcut into the
	// candidate search box's existing tag-substring matching
	// (candidateMatchesSearch, sets_builder.go); clicking a chip doesn't add
	// any new filter semantics of its own.
	TagValuesByCategory map[string][]string

	// BrowsableSongs is the "Browse all songs" list's empty-query-and-role
	// state (CandidateSearchResults) - every active, not-yet-added song's
	// id+title, in the same title order fetchActiveSongs already returns
	// them, so someone can skim titles before typing anything. Deliberately
	// the lightweight SongOptionView, not the rich CandidateSongView the
	// same list's actual search results use: computing roles/tags/
	// conflicts/arrangement info for the whole library on every page load
	// is exactly what BuildCandidatesSearch/searchCandidates exist to avoid
	// (see their own doc comments) - this only ever needs a title, so it
	// skips all of that. Built once here from the same songs/inSet
	// buildSetBuilderData already has (see buildBrowsableSongs,
	// sets_builder.go); searchCandidates builds the identical list on its
	// own early-return path for BuildCandidatesSearch's re-renders (e.g.
	// clearing the search box back to empty).
	BrowsableSongs []SongOptionView
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
	// a round trip (see web/static/js/scheduling-build.js).
	AnchorItemID string
	// Moved is true when this song has been dragged to a new position
	// since it was last published - see scheduling.SetSong.Moved. Shown as
	// a badge right on the row, since "the order changed" without saying
	// which songs moved is easy to lose track of.
	Moved bool
	// InfoPending is true once Description, Key, or Leader has been edited
	// on an already-published song without that edit reaching Planning
	// Center yet - see scheduling.SetSong.InfoPending, the item-content
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
	// Structure is the arrangement's section-by-section structure ("Intro,
	// Verse 1, Chorus...") - see scheduling.SongArrangementInfo.Structure
	// and formatSongStructure for how it's derived. Detail-card only (see
	// songDetailCard): unlike ArrangementInfo above, it isn't shown on the
	// compact order-of-service row - there's no room there for a full
	// structure list on top of the existing summary line.
	Structure string
	// HasChordChart mirrors scheduling.SongArrangementInfo.HasChordChart -
	// true when the arrangement has a chord chart document attached in
	// Planning Center. Shown as a badge on the song detail card only, same
	// detail-card-only scope as Structure above.
	HasChordChart bool
	// Attachments is this song's cached Planning Center Attachments (lead
	// sheet/chord chart PDFs, scans, audio - arbitrary uploaded files, see
	// scheduling.SongAttachmentInfo) - a small "Files" list of links on the
	// song detail card, one per attachment, nothing shown when there are
	// none. See docs/plans/shipped/song-lead-sheet-attachments-plan.md.
	// Detail-card-only, same scope as Structure/HasChordChart above.
	Attachments []SongAttachmentView
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

	// RecentlyRepeated is true when this song was sung in each of the last
	// two past plans for this service type (see the controller's
	// fetchRecentPlanHistory) - mirrors CandidateSongView.RecentlyRepeated,
	// rendered inline via components.ConflictBadge in songBadgeSpans
	// (set_builder.templ) instead of the old top-of-page RepeatWarnings
	// list - see docs/plans/shipped/in-place-set-warnings-plan.md.
	RecentlyRepeated bool
	// ConflictTitles lists the titles of other songs already in this set
	// that this one has an avoid_pairing relationship with - mirrors
	// CandidateSongView.ConflictTitles, rendered the same way (a
	// components.ConflictBadge naming the other song(s)) instead of the old
	// top-of-page ExistingConflicts list. Empty when there's no such
	// conflict. The two songs in a pair aren't necessarily adjacent rows -
	// see the controller's conflictTitlesBySong.
	ConflictTitles []string
	// KeyTransitionLabel is a short, always-visible badge caption
	// ("half-step from previous song") when this song's key is a half-step
	// or tritone change from the song immediately before it in the order of
	// service with nothing between them (see the controller's
	// keyTransitionWarnings) - "" means no such transition. Deliberately
	// attached to the *second*, later song's row, not the first or a
	// separate spacer row between them - see
	// docs/plans/shipped/in-place-set-warnings-plan.md's resolved rendering
	// decision. KeyTransitionMessage is the full explanatory sentence,
	// revealed via a click-to-expand components.Disclosure rather than
	// shown inline (too long for a badge) - see songBadgeSpans,
	// set_builder.templ. KeyTransitionSuggestion is one more sentence
	// appended after KeyTransitionMessage in that same popover, suggesting
	// the actual move that eases the transition (e.g. hitting the V7 of
	// the destination key right before the change) - see
	// docs/plans/shipped/key-change-tip-plan.md.
	KeyTransitionLabel      string
	KeyTransitionMessage    string
	KeyTransitionSuggestion string

	// LeaderPersonID is this one occurrence's local per-occurrence song
	// leader (see scheduling.SetSong.LeaderPersonID) - "" means no leader
	// set. Editable on the song detail card via a PersonPickerField, same
	// local-draft-only semantics as Key/Description above: it reaches
	// Planning Center as a real ItemAssignment on the next Publish or
	// Refresh song info (see syncLeaderAssignment,
	// sets_publish.go/leader_note.go), never immediately.
	// LeaderPersonName is that person's resolved display name (batched
	// via personNameMapForIDs, buildSetBuilderData), falling back to the
	// raw id if the lookup misses - what the picker's search box shows on
	// load, and what read-only past-plan rendering shows as plain text.
	LeaderPersonID   string
	LeaderPersonName string

	// VocalRangeDisplay is this song's practical sung range, already
	// transposed into this row's own effective key (see the controller's
	// songEffectiveVocalRangeAbs, populated in renderServiceOrder) and
	// formatted as "G3-D5" - "" when no range is
	// on record for this song (see internal/scheduling/song_vocal_range.go
	// and the /scheduling/song-vocal-range setup page), in which case the
	// detail card shows nothing rather than a placeholder. Detail-card
	// only, same scope as Structure/HasChordChart above - Phase 2 of
	// docs/plans/in-progress/vocal-range-plan.md.
	VocalRangeDisplay string

	// VocalRangeHasPlayableNotes/VocalRangeLowPitchClass/VocalRangeLowOctave/
	// VocalRangeHighPitchClass/VocalRangeHighOctave are VocalRangeDisplay's
	// same low/high notes, already split into playable pitch-class/octave
	// ints (see the controller's songEffectiveVocalRangeAbs +
	// scheduling.PitchClassAndOctave) so the detail card's "Vocal range"
	// line can offer tap-to-hear buttons (playVocalTone,
	// web/static/js/scheduling-vocal-range.js) the same way song_vocal_range.templ's
	// setup-page rows already do (vocalRangeLine) - "can I hear what this
	// note sounds like" without leaving the Build page or reaching for an
	// instrument. HasPlayableNotes mirrors VocalRangeDisplay != "" (true
	// exactly when VocalRangeDisplay is set) - kept as its own bool rather
	// than re-checking the string, so a templ caller never has to reason
	// about which representation is authoritative.
	VocalRangeHasPlayableNotes bool
	VocalRangeLowPitchClass    int
	VocalRangeLowOctave        int
	VocalRangeHighPitchClass   int
	VocalRangeHighOctave       int

	// VocalRangeWarningLabel/VocalRangeWarningMessage/VocalRangeWarningIsTip
	// are Phase 3 of docs/plans/in-progress/vocal-range-plan.md: a
	// range-mismatch warning (or downgraded octave-shift tip) when a
	// vocalist assigned to this plan's roster has a stored
	// scheduling.PersonVocalRange that doesn't cover this song's own
	// VocalRangeDisplay range - see the controller's
	// vocalRangeWarningForSong for the full "who counts as a vocalist to
	// check"/octave-shift-downgrade logic. VocalRangeWarningLabel == ""
	// means nobody assigned mismatches at all (by far the common case - no
	// roster yet, no vocalist with a range on file, or every fitting range
	// already covers this song), in which case the row shows no badge at
	// all. VocalRangeWarningIsTip distinguishes a hard mismatch (rendered
	// like KeyTransitionLabel - a conflict-badge-styled warning) from an
	// octave-shift-fixes-it tip (rendered like TransitionNotePrompts - a
	// plain, non-warning badge) - see songBadgeSpans, set_builder.templ.
	VocalRangeWarningLabel   string
	VocalRangeWarningMessage string
	VocalRangeWarningIsTip   bool

	// VocalRange is this song's stored vocal range row, zero-valued
	// (ID == 0) when none is on file - unlike VocalRangeDisplay above
	// (already transposed for display), this is the raw stored row, used
	// to pre-fill the Build page's own "Set vocal range" forms (the
	// kebab-menu row form, set_builder.templ) with the as-measured values
	// (vocalRangeFormFields, quick_action.templ) so resubmitting reads and
	// acts like an edit rather than a second, conflicting entry -
	// scheduling.SetSongVocalRange already upserts by (organization,
	// song_id) regardless, this just makes the form honestly reflect that
	// (docs/issues.md: "I don't feel like we need multiple vocal ranges
	// since we can calculate them based on the key... fill in the form
	// with the existing data and make the save an update if applicable").
	VocalRange scheduling.SongVocalRange

	// TransitionNotePrompts is the resurfacing prompt(s) for this row - the
	// latest debrief/talking-point note for this song alone (single-song),
	// or for the pair ending here (this song immediately following another
	// one in the order), computed the same adjacency-walk way
	// KeyTransitionLabel is (see the controller's transitionNotePrompts) -
	// see docs/plans/shipped/transition-notes-plan.md. Rendered as its own
	// informational badge in songBadgeSpans (set_builder.templ),
	// deliberately styled distinctly from ConflictBadge/KeyTransitionLabel
	// above - it's history being handed back to the director, not a
	// problem being flagged. Distinct from TransitionNotes below: this is
	// latest-only and adjacency-gated (the resurfacing banner);
	// TransitionNotes is this song's full log, shown unconditionally on its
	// own detail card.
	TransitionNotePrompts []TransitionNotePromptView
	// TransitionNotes is every debrief/talking-point note on file that
	// touches this song - either as the sole subject of a single-song note,
	// or on either side of a pair (see ListTransitionNotesForSong's "either
	// side" query, mirroring ListSongRelationships) - newest first. Powers
	// the song detail card's own notes subsection (song_detail_card.templ):
	// a full chronological log, not just the latest ("post-service
	// debrief **log**" - see that plan's own Schema section on why this
	// stays append-only rather than a single overwritten value).
	TransitionNotes []TransitionNoteEntryView
}

// TransitionNotePromptView is one resurfacing prompt on a song's own row -
// see SetSongView.TransitionNotePrompts and the controller's
// transitionNotePrompts.
type TransitionNotePromptView struct {
	Kind    string // "debrief" or "talking_point"
	Label   string // short, always-visible badge text (e.g. "Debrief on file")
	Message string // the note text (plus context for a pair), revealed via click-to-expand
}

// TransitionNoteEntryView is one entry in a song's full transition-note
// log - see SetSongView.TransitionNotes. Direction/OtherTitle are both ""
// for a single-song note; otherwise Direction is "into" (this song was
// entered as song_id_a, transitioning into OtherTitle) or "from" (this
// song was song_id_b, the destination of a transition entered from
// OtherTitle) - see the controller's buildTransitionNoteViews.
type TransitionNoteEntryView struct {
	ID         int64
	Kind       string
	KindLabel  string
	Direction  string
	OtherTitle string
	Note       string
	CreatedAt  string
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

// SongAttachmentView is one of a song's cached Planning Center Attachments
// - see SetSongView.Attachments and scheduling.SongAttachmentInfo.
// Deliberately carries no URL: the link on the detail card points at this
// app's own attachment-open route (OpenSongAttachment,
// song_arrangement_cache.go), which live-fetches a fresh URL from Planning
// Center and redirects, rather than linking straight to a cached one - see
// scheduling.SongAttachmentInfo's own doc comment for why.
type SongAttachmentView struct {
	PCOAttachmentID string
	Filename        string
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
	// ItemLength is this item's own PCO Length attribute formatted as
	// "3:45" (see formatLengthSeconds) - "" when PCO has no length set for
	// it, same convention as SetSongView.ArrangementInfo omitting the
	// figure entirely rather than showing "0:00". PCO tracks Length for
	// any item type, not just songs (a sermon or video counts too - see
	// serviceOrderLengthSeconds, which already sums this into the whole
	// plan's total), so a header/media/non-song item can have a real
	// figure here worth showing on its own row. Confirmed live: Planning
	// Center silently drops whatever Length a header item is created with
	// (comes back 0 regardless) - see AddPlanItem's own doc comment - so
	// this is only ever non-"" for a plain item.
	ItemLength string
	// ItemLengthSeconds is the same figure as ItemLength, unformatted -
	// addPlanItemRows' edit form (quick_action.templ) splits this back
	// into its own minutes/seconds fields to pre-fill an edit, so a save
	// that isn't actually about the length doesn't blank it back to 0
	// (pco.UpdateItem's Length is a plain int, always sent, not an
	// optional "leave alone" - see UpdatePlanItem's own doc comment).
	ItemLengthSeconds int
	// ItemDescription is this item's own PCO Description attribute, shown
	// as a second muted line under the title (same spot a set song's own
	// Description would show, if this table rendered one) - already
	// fetched as part of the same GetItems call ServiceOrder is built
	// from, so this costs nothing extra to show.
	ItemDescription string

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
	ID    string
	Title string
	Dates string
	// Note is this plan's purely-local at-a-glance note (see
	// plan_notes/GetPlanNote) - "" means nothing's been saved for it yet.
	// Shown (and editable) in the plan list, and matched by the picker's
	// search box alongside Title/Dates - see filterUpcomingPlans.
	Note         string
	SetStatus    string
	RosterStatus string
}

// SongOptionView is a lightweight song option - just an id and a title,
// unlike the richer CandidateSongView below. Backs a plain native <select>
// picker (SetBuilderData.ReplaceCandidates) and, identically, the "Browse
// all songs" list's empty-state title-browse list
// (SetBuilderData.BrowsableSongs) - both need nothing more than an id and a
// title, so they share this one type rather than each growing their own.
type SongOptionView struct {
	SongID string
	Title  string
}

// RosterPersonOptionView is a lightweight roster person option - just an id
// and a name, the same "enough to populate a plain native <select>" shape
// as SongOptionView above. Backs the song leader picker's <select>
// (SetBuilderData.RosterPersonOptions) - see that field's own doc comment
// for why it's scoped to this plan's roster rather than a general person
// search.
type RosterPersonOptionView struct {
	PersonID string
	Name     string
}

// CandidateSongView is a song not yet in the set, offered for adding via
// the "Browse all songs" search list (see CandidateSearchResults,
// BuildCandidatesSearch) - computed only for songs that actually match a
// search/role-filter request, never for the whole library at once (see
// that handler's doc comment for why). ConflictTitles lists the titles of
// set songs it has an avoid_pairing relationship with (empty when there's
// no conflict); RolesAttr/TagsAttr are space-separated codes/values kept
// around from when filtering was client-side - now unused by any JS, but
// left on the view model since songCandidateMeta (set_builder.templ) has
// no other use for them and removing them buys nothing.
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
	// NotSungInLabel is a short badge ("Not sung recently") for a song
	// that HAS been scheduled before (unlike NewSongLabel's never-sung
	// case) but not within the "not sung in a while" threshold - see
	// notSungInLabel (sets_builder.go), which computes this. "" means not
	// applicable: never scheduled at all, scheduled recently enough, or
	// exempted because the song carries a seasonal tag (see
	// scheduling.TagCategorySeasonal) - a long gap there is by design, not
	// a signal.
	NotSungInLabel string
	// ArrangementInfo mirrors SetSongView.ArrangementInfo, but is populated
	// cache-only (see candidateLookups.candidateSongView, sets_builder.go) -
	// "" whenever this song hasn't been cached yet (never added to a set,
	// and not covered by a Song Info sync), not fetched live for the whole
	// candidate library the way a set song's is.
	ArrangementInfo string
	// Roles is this song's assigned roles, for display as badges - see
	// SetSongView.Roles, which this mirrors.
	Roles     []SongRoleView
	RolesAttr string
	// Tags is this song's theme/scripture/idea tags (see song_tags.go), for
	// display as badges.
	Tags     []SongTagView
	TagsAttr string
	// Contexts is this song's assigned contexts ("Big church," "Youth,"
	// ...), for display as badges - see song_contexts.go and
	// SetBuilderData.ContextTypes. ContextsAttr is the space-joined,
	// string-formatted context_type_id list (SongContextView.ID), matched
	// server-side against a context-filter query param by
	// candidateMatchesContext - the context equivalent of RolesAttr.
	Contexts     []SongContextView
	ContextsAttr string
}

// HasMeta reports whether c has anything at all to show in
// songCandidateMeta - used by CandidateDetails (set_builder.templ) to tell
// "nothing on record for this song yet" apart from a rendering bug, since
// an empty candidate otherwise renders as a blank space with no visible
// change on click.
func (c CandidateSongView) HasMeta() bool {
	return c.NewSongLabel != "" ||
		c.NotSungInLabel != "" ||
		c.ArrangementInfo != "" ||
		len(c.Roles) > 0 ||
		len(c.Tags) > 0 ||
		len(c.Contexts) > 0 ||
		len(c.ConflictTitles) > 0 ||
		c.RecentlyRepeated
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

// SongContextView is one context assigned to a song ("Big church," "Youth,"
// ...) - already resolved to its display label (see contextTypeLabelMap in
// the controller) rather than the raw int64 id - the context equivalent of
// SongRoleView. ID is kept (unlike SongRoleView's Code, which is the role
// itself) since song_context_types uses a surrogate integer id, not a
// human-legible code, so it's what the context-filter buttons/edit forms
// need to reference this context by.
type SongContextView struct {
	ID    int64
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
