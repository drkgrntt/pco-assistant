package controllers

import (
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

type HomeController struct{}

func NewHomeController() *HomeController {
	return &HomeController{}
}

func (hc *HomeController) RegisterRoutes(router fiber.Router) {
	router.Get("/", hc.Index)
}

// Index serves "/" - the one route RequireAuth treats as soft-auth (see
// its own doc comment) rather than hard-gated, so it's reachable signed
// out. personID is only ever populated by a real session, never faked, so
// checking it is the correct signal for "should this be the public landing
// page or the signed-in dashboard" - same signal CurrentPerson's own doc
// comment already describes.
func (hc *HomeController) Index(c *fiber.Ctx) error {
	personID, personName := middleware.CurrentPerson(c)
	if personID == "" {
		return render(c, views.Landing())
	}
	return render(c, views.Home(personName, middleware.CurrentOrganization(c)))
}
