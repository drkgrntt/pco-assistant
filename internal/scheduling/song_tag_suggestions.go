package scheduling

import (
	"context"
	"sort"
	"strings"
	"unicode"

	pco "github.com/drkgrntt/go-pco"
)

// IgnoreSongTagSuggestion permanently dismisses word from this org's
// future song-tag suggestions (see ListIgnoredSongTagSuggestionWords, and
// the suggestion algorithm in the controllers package that reads it) -
// idempotent, so ignoring an already-ignored word is a harmless no-op
// rather than an error.
func (s *Store) IgnoreSongTagSuggestion(ctx context.Context, organizationID, word string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ignored_song_tag_suggestions (organization_id, word)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, word) DO NOTHING
	`, organizationID, word)
	return err
}

// ListIgnoredSongTagSuggestionWords returns every word this org has
// dismissed, as a lookup set - the suggestion algorithm skips any group
// whose shared word is in this set.
func (s *Store) ListIgnoredSongTagSuggestionWords(ctx context.Context, organizationID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT word FROM ignored_song_tag_suggestions WHERE organization_id = $1
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	words := map[string]bool{}
	for rows.Next() {
		var word string
		if err := rows.Scan(&word); err != nil {
			return nil, err
		}
		words[word] = true
	}

	return words, rows.Err()
}

// SongTitleStopwords is the fixed list of common English words excluded
// from song-tag suggestions - without it, nearly every title would share
// "the"/"of"/"my" and produce meaningless groups. Deliberately a plain,
// hand-picked list rather than anything smarter (stemming, part-of-speech
// tagging, AI) - this whole feature is scoped to literal, explainable
// word-matching, per the "no AI" decision it was designed against.
//
// Moved here from the controllers package (which still owns rendering
// this into full display groups - see songTagSuggestions there) so the
// nav's lightweight "is there anything pending" check (middleware.
// PopulateNavState) can share the exact same rules, rather than a second
// copy quietly drifting from the real page's behavior.
var SongTitleStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "in": true, "on": true, "to": true,
	"and": true, "or": true, "but": true, "is": true, "am": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true,
	"my": true, "your": true, "our": true, "his": true, "her": true, "its": true, "their": true,
	"i": true, "you": true, "he": true, "she": true, "it": true, "we": true, "they": true,
	"this": true, "that": true, "these": true, "those": true,
	"for": true, "with": true, "as": true, "at": true, "by": true, "from": true,
	"up": true, "down": true, "out": true, "all": true, "no": true, "not": true, "so": true,
	"if": true, "than": true, "then": true, "when": true,
	"will": true, "would": true, "can": true, "could": true, "shall": true, "should": true,
	"do": true, "does": true, "did": true, "has": true, "have": true, "had": true,
	"me": true, "us": true, "them": true,
	"who": true, "what": true, "why": true, "how": true, "there": true, "here": true, "one": true,
}

// SongTitleWords splits a title into its deduped, filtered significant
// words - lowercased, split on anything that isn't a letter or digit (so
// "King's" becomes "king" - the trailing "s" is dropped by the length
// filter below), excluding stopwords, anything shorter than 3 runes, and
// purely-numeric tokens. Deduped per title so "King of Kings" only
// contributes "king" once, not twice.
func SongTitleWords(title string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) < 3 || SongTitleStopwords[w] || isAllDigits(w) {
			continue
		}
		words[w] = true
	}
	return words
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// SongTagSuggestionGroup/SongTagSuggestionSong are the plain-data shape of
// one suggestion group - the same information the controllers package
// renders into views.SongTagSuggestionGroup/views.SuggestedSongView, kept
// view-free here so this package doesn't need to depend on the views
// package just to compute a suggestion list.
type SongTagSuggestionGroup struct {
	Word  string
	Songs []SongTagSuggestionSong
}

type SongTagSuggestionSong struct {
	SongID        string
	Title         string
	AlreadyTagged bool
}

// SongTagSuggestions groups songs by significant words their titles share
// (see SongTitleWords), keeping only groups with 2+ distinct songs,
// skipping any word in ignoredWords, and flagging a song AlreadyTagged
// when it already has a tag whose value exactly matches the word
// (case-insensitively) - an intentionally exact match: a tag already
// renamed to "Kingship" won't suppress a "king" suggestion on its own,
// since there's no reliable non-AI way to know they're the same idea. A
// group disappears entirely once every song in it is already tagged -
// nothing left to suggest. Sorted by group size descending, then word
// ascending, so the biggest/most useful groups surface first.
func SongTagSuggestions(songs []pco.SongData, existingTags []SongTag, ignoredWords map[string]bool) []SongTagSuggestionGroup {
	taggedWordsBySong := map[string]map[string]bool{}
	for _, tag := range existingTags {
		if taggedWordsBySong[tag.SongID] == nil {
			taggedWordsBySong[tag.SongID] = map[string]bool{}
		}
		taggedWordsBySong[tag.SongID][strings.ToLower(strings.TrimSpace(tag.Value))] = true
	}

	songsByWord := map[string][]pco.SongData{}
	for _, song := range songs {
		for word := range SongTitleWords(song.Attributes.Title) {
			songsByWord[word] = append(songsByWord[word], song)
		}
	}

	var groups []SongTagSuggestionGroup
	for word, groupSongs := range songsByWord {
		if len(groupSongs) < 2 || ignoredWords[word] {
			continue
		}

		sort.Slice(groupSongs, func(i, j int) bool {
			return groupSongs[i].Attributes.Title < groupSongs[j].Attributes.Title
		})

		songViews := make([]SongTagSuggestionSong, 0, len(groupSongs))
		allTagged := true
		for _, song := range groupSongs {
			tagged := taggedWordsBySong[song.ID][word]
			if !tagged {
				allTagged = false
			}
			songViews = append(songViews, SongTagSuggestionSong{
				SongID:        song.ID,
				Title:         song.Attributes.Title,
				AlreadyTagged: tagged,
			})
		}
		if allTagged {
			continue
		}

		groups = append(groups, SongTagSuggestionGroup{Word: word, Songs: songViews})
	}

	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Songs) != len(groups[j].Songs) {
			return len(groups[i].Songs) > len(groups[j].Songs)
		}
		return groups[i].Word < groups[j].Word
	})

	return groups
}
