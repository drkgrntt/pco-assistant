package controllers

import (
	"pco-assistant/internal/server/middleware"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// OpenSongAttachment is the "Files" list's actual link target (songDetailCard,
// song_detail_card.templ) - a plain GET a browser follows directly (a real
// link, opened in a new tab, not an htmx fragment swap), never the cached
// row's own data. See scheduling.SongAttachmentInfo's own doc comment for
// why: go-pco's AttachmentAttributes.URL is not confirmed to be a long-lived
// link rather than an expiring pre-signed one, so this app never persists
// it - every click live-fetches the current attachment list from Planning
// Center and redirects to whatever URL it has *right now*, the same
// "gated behind an explicit click, not a render-path cost" posture as
// RefreshSetSongArrangementInfo's "Update from Planning Center" button.
//
// Re-fetches the whole list (pco.GetAttachments) rather than a single-
// attachment GET, since go-pco deliberately only wraps the list endpoint
// (see attachments.go's own doc comment) - this app never needs more than
// one call's worth of attachments per song regardless.
//
// Errors render as a small plain-text response rather than the full
// SetBuilderBody an htmx handler would use - this route is a direct link
// navigation (likely into a new tab), not part of the Build page's own
// htmx swap cycle, so there is no Build page DOM here to re-render into.
func (sc *SchedulingController) OpenSongAttachment(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	attachmentID := c.Params("attachment_id")

	rowID, err := c.ParamsInt("set_song_id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("set_song_id must be an integer")
	}

	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songID := ""
	for _, ss := range setSongs {
		if ss.ID == int64(rowID) {
			songID = ss.SongID
			break
		}
	}
	if songID == "" {
		return c.Status(fiber.StatusNotFound).SendString("song is no longer in this set")
	}

	resp, err := pco.GetAttachments(ctx, songID, nil)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString("fetching attachment from Planning Center: " + err.Error())
	}

	for _, a := range resp.Data {
		if a.ID == attachmentID {
			if a.Attributes.URL == "" {
				return c.Status(fiber.StatusNotFound).SendString("this attachment has no file to open")
			}
			return c.Redirect(a.Attributes.URL, fiber.StatusFound)
		}
	}

	return c.Status(fiber.StatusNotFound).SendString("attachment not found - it may have been removed in Planning Center")
}
