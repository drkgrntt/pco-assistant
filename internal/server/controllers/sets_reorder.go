package controllers

// Drag-and-drop reordering of set songs within the order of service.

import (
	"fmt"
	"strconv"
	"strings"

	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// RepositionSetSong is what the order-of-service drag-and-drop calls: the
// dragged row is placed immediately after whatever row it was dropped on
// (?after=, either "item:<pco_item_id>" for a foreign row or "row:<id>" for
// another set song - see parseAfterToken and RepositionSongInSet).
func (sc *SchedulingController) RepositionSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		After string `form:"after"`
	}
	errMsg := ""
	rowID, rowErr := c.ParamsInt("set_song_id")
	if rowErr != nil {
		errMsg = "set_song_id must be an integer"
	} else if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		anchorItemID, afterRowID, parseErr := parseAfterToken(body.After)
		if parseErr != nil {
			errMsg = parseErr.Error()
		} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
			errMsg = err.Error()
		} else if err := sc.store.RepositionSongInSet(c.Context(), set.ID, int64(rowID), anchorItemID, afterRowID); err != nil {
			errMsg = err.Error()
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// parseAfterToken decodes the drag-and-drop "after" token: "" (top of
// service), "item:<id>" (right after a foreign item - the start of its
// group), or "row:<id>" (right after another set song's row, inheriting
// its anchor).
func parseAfterToken(token string) (anchorItemID string, afterRowID int64, err error) {
	if after, ok := strings.CutPrefix(token, "item:"); ok {
		return after, 0, nil
	}
	if after, ok := strings.CutPrefix(token, "row:"); ok {
		id, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			return "", 0, fmt.Errorf("invalid row id in after token %q", token)
		}
		return "", id, nil
	}
	return "", 0, nil
}
