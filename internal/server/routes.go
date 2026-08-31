package server

import (
	"pco-assistant/internal/logger"
	"pco-assistant/internal/server/controllers"
	"pco-assistant/internal/server/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func (s *FiberServer) RegisterFiberRoutes() {
	// Apply CORS middleware
	s.App.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Accept,Authorization,Content-Type",
		AllowCredentials: false, // credentials require explicit origins
		MaxAge:           300,
	}))

	s.App.Static("/static", "./web/static")

	// Served from the root, not /static/, so the service worker's default
	// scope covers the whole app rather than just /static/* - see
	// register-sw.js. The URL is still root-level /sw.js regardless of
	// where the file itself lives on disk.
	s.App.Get("/sw.js", func(c *fiber.Ctx) error {
		return c.SendFile("./web/static/js/sw.js")
	})
	s.App.Get("/manifest.json", func(c *fiber.Ctx) error {
		return c.SendFile("./web/static/manifest.json")
	})

	// RequireAuth allowlists /health, /login, /auth/*, and /static/*
	// internally, so it's safe to apply globally ahead of every route.
	s.App.Use(middleware.RequireAuth(s.authStore))
	// RequireActiveOrganization runs after RequireAuth - it reads the
	// organization id RequireAuth just resolved into locals, so it has to
	// come second.
	s.App.Use(middleware.RequireActiveOrganization(s.authStore))
	// PopulateNavState also needs the organization id RequireAuth
	// resolved - runs on every page view since it drives Layout's nav,
	// not just one page (see its own doc comment for the real per-page
	// PCO cost this accepts).
	s.App.Use(middleware.PopulateNavState(s.schedulingStore))

	// Constructed once and shared with HomeController below (rather than a
	// second instance) so Home can reuse its private page-builder methods
	// (planStatus, rosterStatus, buildSongTagSuggestionsData,
	// buildSongInfoData) - see HomeController.buildHomeData.
	schedulingController := controllers.NewSchedulingController(s.schedulingStore)

	controllers.NewHealthController(s.db).RegisterRoutes(s.App)
	controllers.NewAuthController(s.authStore).RegisterRoutes(s.App)
	controllers.NewBillingController(s.authStore, s.billingClient).RegisterRoutes(s.App)
	controllers.NewFeedbackController(s.notifyClient).RegisterRoutes(s.App)
	controllers.NewHomeController(schedulingController, s.authStore).RegisterRoutes(s.App)
	controllers.NewPCOController().RegisterRoutes(s.App)
	controllers.NewStyleGuideController().RegisterRoutes(s.App)
	controllers.NewSchedulingAPIController(s.schedulingStore).RegisterRoutes(s.App)
	schedulingController.RegisterRoutes(s.App)

	logger.Info("routes registered")
}
