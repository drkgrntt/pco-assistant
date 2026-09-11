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
// package just to compute a suggestion list. Word holds whatever display
// text the extractor that built this group chose (see wordsForSong) - a
// single title word for SongTagSuggestions, a whole theme phrase (possibly
// containing spaces/punctuation) for SongThemeSuggestions.
type SongTagSuggestionGroup struct {
	Word  string
	Songs []SongTagSuggestionSong
}

type SongTagSuggestionSong struct {
	SongID        string
	Title         string
	AlreadyTagged bool
}

// wordsForSong returns a song's candidate suggestion terms as
// normalizedKey -> displayText pairs. normalizedKey drives
// grouping/matching (case-insensitive, matching the existing "already
// tagged" comparison against a tag's trimmed+lowercased value);
// displayText is what's actually shown/used as the tag value default. For
// title words these are identical (SongTitleWords already returns
// lowercase). For theme phrases they differ: PCO's Themes string arrives
// pre-capitalized ("God's Attributes", "Kingship") and that casing is
// preserved as the display default rather than lowercased and
// reconstituted - only the grouping/matching key gets lowercased.
type wordsForSong func(pco.SongData) map[string]string

// songTagSuggestionGroups is the shared grouping algorithm behind both
// SongTagSuggestions (title words) and SongThemeSuggestions (PCO Themes) -
// keeping only groups with 2+ distinct songs, skipping any key in
// ignoredWords, and flagging a song AlreadyTagged when it already has a
// tag whose value exactly matches the key (case-insensitively) - an
// intentionally exact match: a tag already renamed to "Kingship" won't
// suppress a "king" suggestion on its own, since there's no reliable
// non-AI way to know they're the same idea. A group disappears entirely
// once every song in it is already tagged - nothing left to suggest.
// Sorted by group size descending, then display text ascending, so the
// biggest/most useful groups surface first. The group's own Word is taken
// from extract(groupSongs[0])[key] - the alphabetically-first song's own
// casing for that key (groupSongs is sorted by title just above) - so
// display text is deterministic regardless of input song order, rather
// than the (possibly-lowercased) key itself.
func songTagSuggestionGroups(songs []pco.SongData, existingTags []SongTag, ignoredWords map[string]bool, extract wordsForSong) []SongTagSuggestionGroup {
	taggedWordsBySong := map[string]map[string]bool{}
	for _, tag := range existingTags {
		if taggedWordsBySong[tag.SongID] == nil {
			taggedWordsBySong[tag.SongID] = map[string]bool{}
		}
		taggedWordsBySong[tag.SongID][strings.ToLower(strings.TrimSpace(tag.Value))] = true
	}

	songsByKey := map[string][]pco.SongData{}
	for _, song := range songs {
		for key := range extract(song) {
			songsByKey[key] = append(songsByKey[key], song)
		}
	}

	var groups []SongTagSuggestionGroup
	for key, groupSongs := range songsByKey {
		if len(groupSongs) < 2 || ignoredWords[key] {
			continue
		}

		sort.Slice(groupSongs, func(i, j int) bool {
			return groupSongs[i].Attributes.Title < groupSongs[j].Attributes.Title
		})

		songViews := make([]SongTagSuggestionSong, 0, len(groupSongs))
		allTagged := true
		for _, song := range groupSongs {
			tagged := taggedWordsBySong[song.ID][key]
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

		displayText := extract(groupSongs[0])[key]
		groups = append(groups, SongTagSuggestionGroup{Word: displayText, Songs: songViews})
	}

	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Songs) != len(groups[j].Songs) {
			return len(groups[i].Songs) > len(groups[j].Songs)
		}
		return groups[i].Word < groups[j].Word
	})

	return groups
}

// titleWordsForSong is the wordsForSong extractor behind SongTagSuggestions
// - title words are already lowercase (SongTitleWords), so the normalized
// key and display text are identical.
func titleWordsForSong(song pco.SongData) map[string]string {
	words := SongTitleWords(song.Attributes.Title)
	out := make(map[string]string, len(words))
	for w := range words {
		out[w] = w
	}
	return out
}

// SongTagSuggestions groups songs by significant words their titles share
// (see SongTitleWords) - see songTagSuggestionGroups for the full grouping/
// matching/sorting rules this delegates to.
func SongTagSuggestions(songs []pco.SongData, existingTags []SongTag, ignoredWords map[string]bool) []SongTagSuggestionGroup {
	return songTagSuggestionGroups(songs, existingTags, ignoredWords, titleWordsForSong)
}

// SongThemeValues splits PCO's own Themes attribute (a comma-separated,
// already-curated string, e.g. "Kingship, Majesty / God's Attributes,
// Praise") into its individual trimmed values, dropping empty segments (a
// leading/trailing comma produces one - confirmed live, e.g. "How He
// Loves" -> ", Father, Glory, God's Attributes, Love, oh how he loves us").
// Unlike SongTitleWords, no stopword filtering or minimum-length filtering
// applies - these aren't raw free text, they're PCO/CCLI's own curated
// theme vocabulary, already meaningful as-is.
func SongThemeValues(themes string) []string {
	var values []string
	for _, v := range strings.Split(themes, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		values = append(values, v)
	}
	return values
}

// themeWordsForSong is the wordsForSong extractor behind
// SongThemeSuggestions - each of a song's SongThemeValues, keyed by its
// lowercased+trimmed form (for grouping/matching) mapped to its original
// PCO casing (for display), deduped per song the same way title words
// dedupe per title.
func themeWordsForSong(song pco.SongData) map[string]string {
	values := SongThemeValues(song.Attributes.Themes)
	out := make(map[string]string, len(values))
	for _, v := range values {
		out[strings.ToLower(strings.TrimSpace(v))] = v
	}
	return out
}

// SongThemeSuggestions groups songs sharing a value from PCO's own Themes
// field (see SongThemeValues) - mirrors SongTagSuggestions' signature and
// return type exactly (see songTagSuggestionGroups for the full grouping/
// matching/sorting rules both delegate to). Word holds a theme phrase here
// (e.g. "God's Attributes"), not a single word - PCO's own casing is
// preserved rather than lowercased.
func SongThemeSuggestions(songs []pco.SongData, existingTags []SongTag, ignoredWords map[string]bool) []SongTagSuggestionGroup {
	return songTagSuggestionGroups(songs, existingTags, ignoredWords, themeWordsForSong)
}
