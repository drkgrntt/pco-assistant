package server

import (
	"log"

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
	// register-sw.js.
	s.App.Get("/sw.js", func(c *fiber.Ctx) error {
		return c.SendFile("./web/static/sw.js")
	})
	s.App.Get("/manifest.json", func(c *fiber.Ctx) error {
		return c.SendFile("./web/static/manifest.json")
	})

	// RequireAuth allowlists /health, /login, /auth/*, and /static/*
	// internally, so it's safe to apply globally ahead of every route.
	s.App.Use(middleware.RequireAuth(s.authStore))

	controllers.NewHealthController(s.db).RegisterRoutes(s.App)
	controllers.NewAuthController(s.authStore).RegisterRoutes(s.App)
	controllers.NewHomeController().RegisterRoutes(s.App)
	controllers.NewPCOController().RegisterRoutes(s.App)
	controllers.NewSchedulingAPIController(s.schedulingStore).RegisterRoutes(s.App)
	controllers.NewSchedulingController(s.schedulingStore).RegisterRoutes(s.App)

	log.Println("Routes registered")
}
