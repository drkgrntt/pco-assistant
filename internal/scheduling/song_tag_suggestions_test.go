package scheduling

import (
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

func song(id, title string) pco.SongData {
	s := pco.SongData{ID: id}
	s.Attributes.Title = title
	return s
}

func songTag(songID, category, value string) SongTag {
	return SongTag{SongID: songID, Category: category, Value: value}
}

func TestSongTagSuggestionsGroupsSharedTitleWords(t *testing.T) {
	songs := []pco.SongData{
		song("1", "King of Kings"),
		song("2", "Christ the King"),
		song("3", "All Hail King Jesus"),
		song("4", "10,000 Reasons"), // shares nothing with anything else
		song("5", "The Lion and the Lamb"),
		song("6", "Lion of Judah"),
	}

	groups := SongTagSuggestions(songs, nil, nil)

	var kingTitles, lionTitles []string
	for _, g := range groups {
		switch g.Word {
		case "king":
			for _, s := range g.Songs {
				kingTitles = append(kingTitles, s.Title)
			}
		case "lion":
			for _, s := range g.Songs {
				lionTitles = append(lionTitles, s.Title)
			}
		}
	}

	if len(kingTitles) != 3 {
		t.Errorf(`expected 3 songs in the "king" group, got %d: %v`, len(kingTitles), kingTitles)
	}
	if len(lionTitles) != 2 {
		t.Errorf(`expected 2 songs in the "lion" group, got %d: %v`, len(lionTitles), lionTitles)
	}
}

// TestSongTagSuggestionsExcludesStopwordsAndSingletons confirms the two
// guards that keep the common case (most titles share "the"/"of"/etc.)
// from drowning out real suggestions: a stopword never becomes its own
// group, and a word only one song's title has never becomes a group
// either (nothing to suggest tagging together).
func TestSongTagSuggestionsExcludesStopwordsAndSingletons(t *testing.T) {
	songs := []pco.SongData{
		song("1", "The Lion and the Lamb"),
		song("2", "Way Maker"),
		song("3", "Reckless Love"),
	}

	groups := SongTagSuggestions(songs, nil, nil)

	for _, g := range groups {
		if SongTitleStopwords[g.Word] {
			t.Errorf("stopword %q leaked through as a suggestion", g.Word)
		}
		if len(g.Songs) < 2 {
			t.Errorf("group %q has fewer than 2 songs: %+v", g.Word, g.Songs)
		}
	}
}

func TestSongTagSuggestionsSkipsIgnoredWords(t *testing.T) {
	songs := []pco.SongData{
		song("1", "King of Kings"),
		song("2", "Christ the King"),
	}

	groups := SongTagSuggestions(songs, nil, map[string]bool{"king": true})

	for _, g := range groups {
		if g.Word == "king" {
			t.Errorf("ignored word %q still produced a group", g.Word)
		}
	}
}

// TestSongTagSuggestionsFlagsAlreadyTaggedSongs confirms the exact-match
// rule documented on SongTagSuggestions: a song already tagged "King"
// (any category) is flagged AlreadyTagged for the "king" group, and a
// song in the same group that isn't tagged that way is not.
func TestSongTagSuggestionsFlagsAlreadyTaggedSongs(t *testing.T) {
	songs := []pco.SongData{
		song("1", "King of Kings"),
		song("2", "Christ the King"),
	}
	tags := []SongTag{songTag("1", TagCategoryTheme, "King")}

	groups := SongTagSuggestions(songs, tags, nil)

	var taggedByID = map[string]bool{}
	for _, g := range groups {
		if g.Word != "king" {
			continue
		}
		for _, s := range g.Songs {
			taggedByID[s.SongID] = s.AlreadyTagged
		}
	}

	if !taggedByID["1"] {
		t.Errorf("song 1 should be AlreadyTagged for \"king\"")
	}
	if taggedByID["2"] {
		t.Errorf("song 2 should NOT be AlreadyTagged for \"king\"")
	}
}

// TestSongTagSuggestionsOmitsFullyAppliedGroups confirms a group
// disappears entirely once every song in it already carries the
// suggested tag - AlreadyTagged alone isn't enough (that's per-song
// display), the group itself has to stop being suggested, or it would
// sit in the list forever showing "Already tagged" on every row with
// nothing left to actually do.
func TestSongTagSuggestionsOmitsFullyAppliedGroups(t *testing.T) {
	songs := []pco.SongData{
		song("1", "King of Kings"),
		song("2", "Christ the King"),
	}
	tags := []SongTag{
		songTag("1", TagCategoryTheme, "King"),
		songTag("2", TagCategoryTheme, "King"),
	}

	groups := SongTagSuggestions(songs, tags, nil)

	for _, g := range groups {
		if g.Word == "king" {
			t.Errorf("fully-applied group %q should have been omitted, got %+v", g.Word, g.Songs)
		}
	}
}

// TestSongTagSuggestionsKeepsPartiallyAppliedGroups is the flip side of
// the fully-applied case above - a group stays listed as long as at
// least one song in it still needs the tag.
func TestSongTagSuggestionsKeepsPartiallyAppliedGroups(t *testing.T) {
	songs := []pco.SongData{
		song("1", "King of Kings"),
		song("2", "Christ the King"),
	}
	tags := []SongTag{songTag("1", TagCategoryTheme, "King")}

	groups := SongTagSuggestions(songs, tags, nil)

	found := false
	for _, g := range groups {
		if g.Word == "king" {
			found = true
		}
	}
	if !found {
		t.Error(`expected the "king" group to still be suggested while song 2 isn't tagged yet`)
	}
}

// TestSongTitleWords locks in the tokenization rules SongTagSuggestions
// depends on: lowercase, split on punctuation (not just whitespace),
// short/stopword/all-digit tokens dropped, and a repeated word in one
// title only counted once.
func TestSongTitleWords(t *testing.T) {
	tests := []struct {
		title string
		want  []string
	}{
		// "Kings" (plural) is deliberately its own token, distinct from
		// "king" - this feature does literal matching only, no stemming.
		{"King of Kings", []string{"king", "kings"}},
		{"King's Kingdom", []string{"king", "kingdom"}},
		{"10,000 Reasons", []string{"reasons"}},
		{"The Lion and the Lamb", []string{"lion", "lamb"}},
	}

	for _, tt := range tests {
		got := SongTitleWords(tt.title)
		if len(got) != len(tt.want) {
			t.Errorf("SongTitleWords(%q) = %v, want %v", tt.title, got, tt.want)
			continue
		}
		for _, w := range tt.want {
			if !got[w] {
				t.Errorf("SongTitleWords(%q) missing %q, got %v", tt.title, w, got)
			}
		}
	}
}
