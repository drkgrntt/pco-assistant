package controllers

import (
	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"

	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"
)

// render writes a templ component as the response body. *fiber.Ctx
// satisfies io.Writer, so templ can render directly to it.
//
// Also stashes whether this request's own PCO calls hit a 429
// (middleware.HadPCORateLimit, consumed here) as views.PCORateLimitedKey,
// so Layout can show its page-level "some details may be stale" banner -
// this has to happen here rather than in a pre-handler middleware (unlike
// every other Layout-driving flag, e.g. NavSuggestionsPendingKey) because
// whether a 429 happened is only knowable after the handler's own PCO
// calls have already run.
func render(c *fiber.Ctx, component templ.Component) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Locals(views.PCORateLimitedKey, middleware.HadPCORateLimit(middleware.CurrentRequestID(c)))
	return component.Render(c.Context(), c)
}
