package controllers

import (
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// StyleGuideController serves a single living reference page for the CSS
// design system (tokens.css/base.css/web/static/css/components/ - see
// their own header comments) - colors, type scale, spacing, and every
// reusable component,
// rendered from the actual classes rather than mocked up separately, so it
// can't silently drift out of sync with them. Not a real end-user feature
// (worship leaders/church admins have no reason to see it), so it's gated
// behind APP_ENV=local the same way PCOController's debug routes are,
// rather than being linked anywhere in the nav.
type StyleGuideController struct{}

func NewStyleGuideController() *StyleGuideController {
	return &StyleGuideController{}
}

func (sc *StyleGuideController) RegisterRoutes(router fiber.Router) {
	router.Get("/style-guide", middleware.DevOnly(), sc.Page)
}

func (sc *StyleGuideController) Page(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	return render(c, views.StyleGuide(personName, middleware.CurrentOrganization(c)))
}
