package controllers

import (
	"database/sql"
	"errors"

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

func (sc *SchedulingController) SongTagsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchAllSongs(middleware.RequestContext(c))

	categories, err := sc.store.ListSongTagCategories(c.Context())
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, err.Error()))
	}

	tags, err := sc.store.ListSongTags(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongTags(personName, orgName, songs, nil, categories, songTitleMap(songs), songTagCategoryLabelMap(categories), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongTags(personName, orgName, songs, tags, categories, songTitleMap(songs), songTagCategoryLabelMap(categories), errMsg))
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

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	categories, _ := sc.store.ListSongTagCategories(c.Context())

	return render(c, views.SongTagsTable(tags, songTitleMap(songs), songTagCategoryLabelMap(categories), errMsg))
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

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	categories, _ := sc.store.ListSongTagCategories(c.Context())

	return render(c, views.SongTagsTable(tags, songTitleMap(songs), songTagCategoryLabelMap(categories), errMsg))
}
