package controllers

import (
	"context"
	"fmt"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// songTitleStopwords/songTitleWords are thin aliases for the shared,
// controllers-agnostic versions now in internal/scheduling - moved there
// so middleware.PopulateNavState's lightweight "is there anything
// pending" nav check can share the exact same word-matching rules this
// page's own suggestions use, rather than a second copy that could
// quietly drift from this one's behavior. Kept as these exact names here
// since the tests in this package already reference them directly.
var songTitleStopwords = scheduling.SongTitleStopwords

func songTitleWords(title string) map[string]bool {
	return scheduling.SongTitleWords(title)
}

// songTagSuggestions renders the shared scheduling.SongTagSuggestions
// result (see its own doc comment for the actual grouping/matching rules)
// into this page's view types - all the real logic now lives in
// internal/scheduling; this is just the view-shape translation.
func songTagSuggestions(songs []pco.SongData, existingTags []scheduling.SongTag, ignoredWords map[string]bool) []views.SongTagSuggestionGroup {
	groups := scheduling.SongTagSuggestions(songs, existingTags, ignoredWords)

	out := make([]views.SongTagSuggestionGroup, len(groups))
	for i, g := range groups {
		songViews := make([]views.SuggestedSongView, len(g.Songs))
		for j, s := range g.Songs {
			songViews[j] = views.SuggestedSongView{
				SongID:        s.SongID,
				Title:         s.Title,
				AlreadyTagged: s.AlreadyTagged,
			}
		}
		out[i] = views.SongTagSuggestionGroup{Word: g.Word, Songs: songViews}
	}
	return out
}

func (sc *SchedulingController) buildSongTagSuggestionsData(ctx context.Context, organizationID string) views.SongTagSuggestionsData {
	var data views.SongTagSuggestionsData

	songs, err := fetchActiveSongs(ctx, organizationID)
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
