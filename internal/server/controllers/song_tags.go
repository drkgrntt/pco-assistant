package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Song tags (theme / scripture / idea) ----------------------------------
//
// Deliberately separate from song_roles.go's opener/response tagging: roles
// feed the set builder's candidate filtering (see sets.go), tags are purely
// descriptive - for finding a song later by sermon theme, related scripture,
// or a loose idea, both here and (see buildSetBuilderData) as search text on
// the set builder's candidate list.
//
// The page itself (song_tags.templ) shows one persistent card per active
// song - see docs/plans/shipped/song-tags-setup-redesign-plan.md - so every
// handler here groups the flat tag list by song id (songTagsBySong) rather
// than resolving a song_id -> title lookup the way the old flat table did.
//
// This file also owns the page's own dismissible Suggestions panel (see
// docs/plans/shipped/song-tags-suggestions-merge-plan.md for why the former
// standalone /scheduling/song-tag-suggestions page was folded in here) -
// songTitleStopwords/songTitleWords/songTagSuggestionGroupViews/
// songTagSuggestions/songThemeSuggestions below moved from that page's own
// controller file, unchanged, since they're still needed here.

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

// songTagSuggestionGroupViews is the shared scheduling.SongTagSuggestionGroup
// -> views.SongTagSuggestionGroup translation both songTagSuggestions and
// songThemeSuggestions use - all the real logic lives in
// internal/scheduling; this is just the view-shape translation.
func songTagSuggestionGroupViews(groups []scheduling.SongTagSuggestionGroup) []views.SongTagSuggestionGroup {
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

// songTagSuggestions renders the shared scheduling.SongTagSuggestions
// result (see its own doc comment for the actual grouping/matching rules)
// into this page's view types.
func songTagSuggestions(songs []pco.SongData, existingTags []scheduling.SongTag, ignoredWords map[string]bool) []views.SongTagSuggestionGroup {
	return songTagSuggestionGroupViews(scheduling.SongTagSuggestions(songs, existingTags, ignoredWords))
}

// songThemeSuggestions renders the shared scheduling.SongThemeSuggestions
// result (see its own doc comment) into this page's view types - mirrors
// songTagSuggestions exactly, just fed from PCO's own Themes field instead
// of shared title words.
func songThemeSuggestions(songs []pco.SongData, existingTags []scheduling.SongTag, ignoredWords map[string]bool) []views.SongTagSuggestionGroup {
	return songTagSuggestionGroupViews(scheduling.SongThemeSuggestions(songs, existingTags, ignoredWords))
}

// songTagSuggestionsPanelData computes both suggestion sources (shared
// title words and shared PCO Themes values) for the Suggestions panel
// (SongTagSuggestionsPanel, song_tags.templ) - shared by SongTagsPage's
// initial render and every mutation handler's OOB refresh below (a tag
// change can shrink or remove a group), and by HomeController's own
// pending count (home.go, songTagSuggestionPendingCount) so all three stay
// in lockstep with the same underlying computation. Takes songs/tags as
// params rather than fetching them itself since every caller but Home
// already has both in scope from its own work.
func (sc *SchedulingController) songTagSuggestionsPanelData(ctx context.Context, organizationID string, songs []pco.SongData, tags []scheduling.SongTag) views.SongTagSuggestionsPanelData {
	ignored, err := sc.store.ListIgnoredSongTagSuggestionWords(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "listing ignored song tag suggestion words", "error", err)
	}
	return views.SongTagSuggestionsPanelData{
		TitleGroups: songTagSuggestions(songs, tags, ignored),
		ThemeGroups: songThemeSuggestions(songs, tags, ignored),
	}
}

// songTagSuggestionPendingCount is HomeController's own hook into the same
// suggestion computation above (home.go) - fetches its own songs/tags
// rather than reusing another handler's request-scoped data, and sums both
// groups so the home page's nudge/checklist count (previously
// buildSongTagSuggestionsData's own len(suggestions.Suggestions), which
// never counted theme groups) reflects everything the Suggestions panel
// itself would show.
func (sc *SchedulingController) songTagSuggestionPendingCount(ctx context.Context, organizationID string) int {
	songs, err := fetchActiveSongs(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "loading songs from Planning Center for song tag suggestion count", "error", err)
	}
	tags, err := sc.store.ListSongTags(ctx, organizationID, "", "")
	if err != nil {
		logger.ErrorContext(ctx, "listing song tags for song tag suggestion count", "error", err)
	}
	data := sc.songTagSuggestionsPanelData(ctx, organizationID, songs, tags)
	return len(data.TitleGroups) + len(data.ThemeGroups)
}

func (sc *SchedulingController) SongTagsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	songs, songsErr := fetchActiveSongs(ctx, orgID)

	categories, err := sc.store.ListSongTagCategories(c.Context())
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, nil, nil, views.SongTagSuggestionsPanelData{}, err.Error()))
	}

	tags, err := sc.store.ListSongTags(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, categories, songTagCategoryLabelMap(categories), views.SongTagSuggestionsPanelData{}, err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	panelData := sc.songTagSuggestionsPanelData(ctx, orgID, songs, tags)

	return render(c, views.SongTags(personName, orgName, songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), panelData, errMsg))
}

func (sc *SchedulingController) CreateSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	var body scheduling.CreateSongTagParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongTag(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongTag(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	tags, err := sc.store.ListSongTags(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	categories, _ := sc.store.ListSongTagCategories(c.Context())
	panelData := sc.songTagSuggestionsPanelData(ctx, orgID, songs, tags)

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), panelData, "", errMsg))
}

// BulkCreateSongTag applies one category/value pair to every selected song
// at once - the counterpart to CreateSongTag's per-song inline form, for
// "I already know this tag, I just need it on songs a/b/c/d" without
// opening each song's own card (or, since the Suggestions panel merge, the
// form applySongTagSuggestion drives after picking a suggestion - see
// song_tags.templ's own doc comment). Reuses the same store.CreateSongTag
// (its ON CONFLICT upsert makes a repeat selection of an already-tagged
// song a harmless no-op rather than an error) rather than a new bulk-insert
// store method.
func (sc *SchedulingController) BulkCreateSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	var body struct {
		Category string   `form:"category"`
		Value    string   `form:"value"`
		SongIDs  []string `form:"song_ids"`
	}

	errMsg := ""
	applied := 0
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if len(body.SongIDs) == 0 {
		errMsg = "select at least one song to add this tag to"
	} else {
		for _, songID := range body.SongIDs {
			if _, err := sc.store.CreateSongTag(ctx, orgID, scheduling.CreateSongTagParams{
				SongID: songID, Category: body.Category, Value: body.Value,
			}); err != nil {
				errMsg = appendErr(errMsg, err.Error())
				continue
			}
			applied++
		}
	}

	tags, err := sc.store.ListSongTags(ctx, orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	categories, _ := sc.store.ListSongTagCategories(ctx)
	panelData := sc.songTagSuggestionsPanelData(ctx, orgID, songs, tags)

	successMsg := ""
	if applied > 0 {
		successMsg = fmt.Sprintf("Added %q to %d song(s).", body.Value, applied)
	}

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), panelData, successMsg, errMsg))
}

func (sc *SchedulingController) DeleteSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongTag(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	tags, err := sc.store.ListSongTags(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	categories, _ := sc.store.ListSongTagCategories(c.Context())
	panelData := sc.songTagSuggestionsPanelData(ctx, orgID, songs, tags)

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), panelData, "", errMsg))
}

// IgnoreSongTagSuggestion permanently dismisses one suggestion group's word
// (writes ignored_song_tag_suggestions) and re-renders only the
// Suggestions panel itself, via SongTagSuggestionsPanel - unlike
// Create/Bulk/DeleteSongTag above, ignoring a suggestion never changes
// which tags exist, so there's nothing for #song-tags-list or the filter
// chips to refresh.
func (sc *SchedulingController) IgnoreSongTagSuggestion(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	word := c.FormValue("word")
	if word != "" {
		if err := sc.store.IgnoreSongTagSuggestion(ctx, orgID, word); err != nil {
			logger.ErrorContext(ctx, "ignore song tag suggestion", "error", err, "word", word)
		}
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	tags, _ := sc.store.ListSongTags(ctx, orgID, "", "")
	panelData := sc.songTagSuggestionsPanelData(ctx, orgID, songs, tags)

	return render(c, views.SongTagSuggestionsPanel(panelData))
}
