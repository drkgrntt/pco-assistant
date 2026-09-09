package controllers

import (
	"database/sql"
	"errors"
	"fmt"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

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

func (sc *SchedulingController) SongTagsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

	categories, err := sc.store.ListSongTagCategories(c.Context())
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, nil, nil, err.Error()))
	}

	tags, err := sc.store.ListSongTags(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, categories, songTagCategoryLabelMap(categories), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongTags(personName, orgName, songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), errMsg))
}

func (sc *SchedulingController) CreateSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

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

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	categories, _ := sc.store.ListSongTagCategories(c.Context())

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), "", errMsg))
}

// BulkCreateSongTag applies one category/value pair to every selected song
// at once - the counterpart to CreateSongTag's per-song inline form, for
// "I already know this tag, I just need it on songs a/b/c/d" without
// opening each song's own card. Mirrors ApplySongTagSuggestion's own
// per-song loop (song_tag_suggestions.go), reusing the same
// store.CreateSongTag (its ON CONFLICT upsert makes a repeat selection of
// an already-tagged song a harmless no-op rather than an error) rather
// than a new bulk-insert store method.
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

	successMsg := ""
	if applied > 0 {
		successMsg = fmt.Sprintf("Added %q to %d song(s).", body.Value, applied)
	}

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), successMsg, errMsg))
}

func (sc *SchedulingController) DeleteSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

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

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	categories, _ := sc.store.ListSongTagCategories(c.Context())

	return render(c, views.SongTagsMutationResponse(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), "", errMsg))
}
