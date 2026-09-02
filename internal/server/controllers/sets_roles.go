package controllers

// Song roles (opener / response tagging) - setup page CRUD.

import (
	"database/sql"
	"errors"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// songRoleTypeLabelMap builds a role code -> label lookup from
// ListSongRoleTypes, for resolving a tag's stored role code back to its
// display label (see SongRolesTable).
func songRoleTypeLabelMap(types []scheduling.SongRoleType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

func (sc *SchedulingController) SongRolesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

	roleTypes, err := sc.store.ListSongRoleTypes(c.Context())
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, err.Error()))
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, nil, roleTypes, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongRoles(personName, orgName, songs, roles, roleTypes, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}

func (sc *SchedulingController) CreateSongRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.CreateSongRoleParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongRole(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongRole(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	roleTypes, _ := sc.store.ListSongRoleTypes(c.Context())

	return render(c, views.SongRolesTable(roles, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}

func (sc *SchedulingController) DeleteSongRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRole(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	roleTypes, _ := sc.store.ListSongRoleTypes(c.Context())

	return render(c, views.SongRolesTable(roles, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}
