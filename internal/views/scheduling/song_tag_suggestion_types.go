package scheduling

// SongTagSuggestionsPanelData is the Song Tags page's Suggestions panel
// view model (SongTagSuggestionsPanel, song_tags.templ) - see the
// controller's songTagSuggestions for how TitleGroups is computed
// (shared-title-word grouping over the org's whole song library), and
// songThemeSuggestions for how ThemeGroups is computed (grouping by a
// shared value from PCO's own Themes field on a song). Unlike the former
// standalone page's SongTagSuggestionsData, there's no ErrMsg/SuccessMsg
// here - the panel is rendered as an OOB swap alongside SongTagsList's own
// success/error messaging (song_tags.templ), so a second copy of that
// would be redundant.
type SongTagSuggestionsPanelData struct {
	TitleGroups []SongTagSuggestionGroup
	ThemeGroups []SongTagSuggestionGroup
}

// SongTagSuggestionGroup is one shared-value suggestion group - Word holds
// a shared title word for a TitleGroups entry, or a shared PCO theme
// phrase (possibly containing spaces/punctuation) for a ThemeGroups
// entry - plus every song that value appears on.
type SongTagSuggestionGroup struct {
	Word  string
	Songs []SuggestedSongView
}

// SuggestedSongView is one song within a suggestion group. AlreadyTagged
// is true when the song already carries a tag whose value exactly matches
// the group's word (case-insensitively) - see songTagSuggestions' doc
// comment for why this is an exact match, not a fuzzy one.
type SuggestedSongView struct {
	SongID        string
	Title         string
	AlreadyTagged bool
}
