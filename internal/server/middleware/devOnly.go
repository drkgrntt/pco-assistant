package middleware

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

// DevOnly gates a route or route group so it only responds when APP_ENV is
// "local". Everywhere else it 404s, so the route's existence isn't leaked
// (as a 403 would) in non-local environments.
func DevOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if os.Getenv("APP_ENV") != "local" {
			return c.SendStatus(fiber.StatusNotFound)
		}

		return c.Next()
	}
}
