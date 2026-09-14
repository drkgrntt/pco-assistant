package middleware

import "github.com/gofiber/fiber/v2"

// SecurityHeaders sets a small, deliberately conservative set of
// response headers - the ones with essentially zero compatibility risk
// for this app's actual shape (server-rendered templ + htmx, no iframe
// embedding, no cross-origin API consumers). Register early, alongside
// RequestID/Recover, so every response gets these regardless of route.
//
// Deliberately NOT included here: a Content-Security-Policy. This app's
// own architecture (internal/views/layout.templ's inline
// onclick="..." handlers calling straight into the global-scope JS
// files under web/static/js/ - see CLAUDE.md's Architecture section) is
// incompatible with a strict CSP (no unsafe-inline); a real CSP pass
// needs its own review against every page rather than a default guessed
// here, so it's left as a follow-up rather than shipping something
// unverified. Cross-Origin-Embedder-Policy/Cross-Origin-Opener-Policy
// are also left out for the same reason - Fiber's helmet middleware
// defaults those to "require-corp"/"same-origin", which can silently
// break loading cross-origin resources (this app's layout pulls Google
// Fonts CSS/fonts) unless verified live first; not worth the risk for
// headers with real, but much smaller, security value than the three
// below.
func SecurityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
		c.Set(fiber.HeaderXFrameOptions, "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		return c.Next()
	}
}
