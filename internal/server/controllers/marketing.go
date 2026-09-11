package controllers

import (
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// MarketingController serves the public marketing pages that sit alongside
// the homepage (Landing, served separately by HomeController.Index) -
// Pricing, HowItWorks, Privacy, and Terms, all pure static content with no
// auth/org dependency at all, unlike Landing which still branches on
// sign-in status. Registered in publicPrefixes (middleware/auth.go) so
// they're reachable with no session, same as /login.
type MarketingController struct{}

func NewMarketingController() *MarketingController {
	return &MarketingController{}
}

func (mc *MarketingController) RegisterRoutes(router fiber.Router) {
	router.Get("/pricing", mc.Pricing)
	router.Get("/how-it-works", mc.HowItWorks)
	router.Get("/privacy", mc.Privacy)
	router.Get("/terms", mc.Terms)
}

func (mc *MarketingController) Pricing(c *fiber.Ctx) error {
	return render(c, views.Pricing())
}

func (mc *MarketingController) HowItWorks(c *fiber.Ctx) error {
	return render(c, views.HowItWorks())
}

func (mc *MarketingController) Privacy(c *fiber.Ctx) error {
	return render(c, views.Privacy())
}

func (mc *MarketingController) Terms(c *fiber.Ctx) error {
	return render(c, views.Terms())
}
