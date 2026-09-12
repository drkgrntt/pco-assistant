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
// ListSongRoleTypes, for resolving a stored role code back to its display
// label - still used by the Build page (sets_builder.go), which shows a
// song's roles inline rather than via this page's own bucket cards.
func songRoleTypeLabelMap(types []scheduling.SongRoleType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

// songRolesByRoleCode groups a flat song-role list by role code, so
// SongRoleBuckets' per-bucket render (song_roles.templ - see
// docs/plans/shipped/song-roles-bucket-redesign-plan.md) is a map read
// instead of a query - the role equivalent of songTagsBySong
// (scheduling.go), just keyed by role code instead of song id since this
// page groups by role, not song.
func songRolesByRoleCode(roles []scheduling.SongRole) map[string][]scheduling.SongRole {
	byRole := make(map[string][]scheduling.SongRole, len(roles))
	for _, r := range roles {
		byRole[r.Role] = append(byRole[r.Role], r)
	}
	return byRole
}

func (sc *SchedulingController) SongRolesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

	roleTypes, err := sc.store.ListSongRoleTypes(c.Context())
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, nil, nil, songTitleMap(songs), err.Error()))
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, roleTypes, nil, songTitleMap(songs), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongRoles(personName, orgName, songs, roleTypes, songRolesByRoleCode(roles), songTitleMap(songs), errMsg))
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

	return render(c, views.SongRoleBuckets(songs, roleTypes, songRolesByRoleCode(roles), songTitleMap(songs), errMsg))
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

	return render(c, views.SongRoleBuckets(songs, roleTypes, songRolesByRoleCode(roles), songTitleMap(songs), errMsg))
}
