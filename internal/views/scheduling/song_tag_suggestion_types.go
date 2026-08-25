package scheduling

import "pco-assistant/internal/scheduling"

// SongTagSuggestionsData is the Song Tag Suggestions page's full view
// model - see the controller's songTagSuggestions for how Suggestions is
// computed (shared-title-word grouping over the org's whole song library).
type SongTagSuggestionsData struct {
	Suggestions   []SongTagSuggestionGroup
	TagCategories []scheduling.SongTagCategory
	ErrMsg        string
	// SuccessMsg reports the outcome of the last Apply/Ignore action, same
	// pattern as every other setup page's PublishMsg/success line.
	SuccessMsg string
}

// SongTagSuggestionGroup is one shared-word suggestion: a word found in
// two or more song titles, plus every song that title word appears in.
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
