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

	return render(c, views.SongTagsList(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), errMsg))
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

	return render(c, views.SongTagsList(songs, songTagsBySong(tags), categories, songTagCategoryLabelMap(categories), errMsg))
}
