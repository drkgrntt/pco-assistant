package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// songTitleStopwords is the fixed list of common English words excluded
// from song-tag suggestions - without it, nearly every title would share
// "the"/"of"/"my" and produce meaningless groups. Deliberately a plain,
// hand-picked list rather than anything smarter (stemming, part-of-speech
// tagging, AI) - this whole feature is scoped to literal, explainable
// word-matching, per the "no AI" decision it was designed against.
var songTitleStopwords = map[string]bool{
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

// songTitleWords splits a title into its deduped, filtered significant
// words - lowercased, split on anything that isn't a letter or digit (so
// "King's" becomes "king" - the trailing "s" is dropped by the length
// filter below), excluding stopwords, anything shorter than 3 runes, and
// purely-numeric tokens. Deduped per title so "King of Kings" only
// contributes "king" once, not twice.
func songTitleWords(title string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) < 3 || songTitleStopwords[w] || isAllDigits(w) {
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

// songTagSuggestions groups songs by significant words their titles share
// (see songTitleWords), keeping only groups with 2+ distinct songs,
// skipping any word in ignoredWords (see IgnoreSongTagSuggestion), and
// flagging a song AlreadyTagged when it already has a tag whose value
// exactly matches the word (case-insensitively) - an intentionally exact
// match: a tag already renamed to "Kingship" won't suppress a "king"
// suggestion on its own, since there's no reliable non-AI way to know
// they're the same idea. The Ignore action is the escape hatch for that
// case. Sorted by group size descending, then word ascending, so the
// biggest/most useful groups surface first.
func songTagSuggestions(songs []pco.SongData, existingTags []scheduling.SongTag, ignoredWords map[string]bool) []views.SongTagSuggestionGroup {
	taggedWordsBySong := map[string]map[string]bool{}
	for _, tag := range existingTags {
		if taggedWordsBySong[tag.SongID] == nil {
			taggedWordsBySong[tag.SongID] = map[string]bool{}
		}
		taggedWordsBySong[tag.SongID][strings.ToLower(strings.TrimSpace(tag.Value))] = true
	}

	songsByWord := map[string][]pco.SongData{}
	for _, song := range songs {
		for word := range songTitleWords(song.Attributes.Title) {
			songsByWord[word] = append(songsByWord[word], song)
		}
	}

	var groups []views.SongTagSuggestionGroup
	for word, groupSongs := range songsByWord {
		if len(groupSongs) < 2 || ignoredWords[word] {
			continue
		}

		sort.Slice(groupSongs, func(i, j int) bool {
			return groupSongs[i].Attributes.Title < groupSongs[j].Attributes.Title
		})

		views_ := make([]views.SuggestedSongView, 0, len(groupSongs))
		for _, song := range groupSongs {
			views_ = append(views_, views.SuggestedSongView{
				SongID:        song.ID,
				Title:         song.Attributes.Title,
				AlreadyTagged: taggedWordsBySong[song.ID][word],
			})
		}

		groups = append(groups, views.SongTagSuggestionGroup{Word: word, Songs: views_})
	}

	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Songs) != len(groups[j].Songs) {
			return len(groups[i].Songs) > len(groups[j].Songs)
		}
		return groups[i].Word < groups[j].Word
	})

	return groups
}

func (sc *SchedulingController) buildSongTagSuggestionsData(ctx context.Context, organizationID string) views.SongTagSuggestionsData {
	var data views.SongTagSuggestionsData

	songs, err := fetchAllSongs(ctx)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading songs from Planning Center: "+err.Error())
	}

	categories, err := sc.store.ListSongTagCategories(ctx)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	data.TagCategories = categories

	tags, err := sc.store.ListSongTags(ctx, organizationID, "", "")
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}

	ignored, err := sc.store.ListIgnoredSongTagSuggestionWords(ctx, organizationID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}

	data.Suggestions = songTagSuggestions(songs, tags, ignored)

	return data
}

func (sc *SchedulingController) SongTagSuggestionsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	data := sc.buildSongTagSuggestionsData(middleware.RequestContext(c), orgID)

	return render(c, views.SongTagSuggestions(personName, orgName, data))
}

func (sc *SchedulingController) ApplySongTagSuggestion(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	var body struct {
		Category string   `form:"category"`
		Value    string   `form:"value"`
		SongIDs  []string `form:"song_ids"`
	}

	data := sc.buildSongTagSuggestionsData(ctx, orgID)
	if err := c.BodyParser(&body); err != nil {
		data.ErrMsg = appendErr(err.Error(), data.ErrMsg)
		return render(c, views.SongTagSuggestionsList(data))
	}
	if len(body.SongIDs) == 0 {
		data.ErrMsg = appendErr("select at least one song", data.ErrMsg)
		return render(c, views.SongTagSuggestionsList(data))
	}

	applied := 0
	errMsg := ""
	for _, songID := range body.SongIDs {
		if _, err := sc.store.CreateSongTag(ctx, orgID, scheduling.CreateSongTagParams{
			SongID: songID, Category: body.Category, Value: body.Value,
		}); err != nil {
			errMsg = appendErr(errMsg, err.Error())
			continue
		}
		applied++
	}

	data = sc.buildSongTagSuggestionsData(ctx, orgID)
	data.ErrMsg = errMsg
	if applied > 0 {
		data.SuccessMsg = fmt.Sprintf("Tagged %d song(s) with %q.", applied, body.Value)
	}

	return render(c, views.SongTagSuggestionsList(data))
}

func (sc *SchedulingController) IgnoreSongTagSuggestion(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	word := c.FormValue("word")
	errMsg := ""
	if word == "" {
		errMsg = "word is required"
	} else if err := sc.store.IgnoreSongTagSuggestion(ctx, orgID, word); err != nil {
		errMsg = err.Error()
	}

	data := sc.buildSongTagSuggestionsData(ctx, orgID)
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	} else {
		data.SuccessMsg = fmt.Sprintf("Won't suggest %q again.", word)
	}

	return render(c, views.SongTagSuggestionsList(data))
}
