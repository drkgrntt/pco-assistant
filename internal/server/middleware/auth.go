package middleware

import (
	"strings"

	"pco-assistant/internal/auth"

	"github.com/gofiber/fiber/v2"
)

const SessionCookieName = "church_session"

// publicPrefixes never require a session: infra health checks, the sign-in
// flow itself, static assets, and the PWA manifest/service worker (which
// must be reachable pre-login for install prompts and are root-served, not
// under /static/, so they need their own entries here).
var publicPrefixes = []string{"/health", "/login", "/auth/", "/static/", "/sw.js", "/manifest.json"}

// RequireAuth gates every route not covered by publicPrefixes behind a
// valid, unexpired session. On success it stashes the signed-in person's id
// and name in fiber locals for handlers/views to use.
func RequireAuth(store *auth.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		for _, prefix := range publicPrefixes {
			if c.Path() == prefix || strings.HasPrefix(c.Path(), prefix) {
				return c.Next()
			}
		}

		// Any lookup failure (expired, unknown token, DB error) just falls
		// through to the redirect below.
		if token := c.Cookies(SessionCookieName); token != "" {
			if session, err := store.GetSession(c.Context(), token); err == nil {
				c.Locals("person_id", session.PersonID)
				c.Locals("person_name", session.PersonName)
				return c.Next()
			}
		}

		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", "/login")
			return c.SendStatus(fiber.StatusOK)
		}

		return c.Redirect("/login")
	}
}

// CurrentPerson reads the signed-in person's id/name that RequireAuth
// stashed in locals. Only meaningful on routes RequireAuth let through.
func CurrentPerson(c *fiber.Ctx) (id, name string) {
	id, _ = c.Locals("person_id").(string)
	name, _ = c.Locals("person_name").(string)
	return id, name
}
