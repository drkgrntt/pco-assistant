package middleware

import (
	"pco-assistant/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// RequireActiveOrganization runs after RequireAuth and checks the
// signed-in session's organization status (see auth.OrganizationStatus) -
// the actual paywall enforcement point, once there's a real subscription
// status to enforce. Fails open: no organization id in locals (a public
// route RequireAuth let straight through, or the "/" soft-auth path with
// no session - RequireAuth already handles both), no organization row
// yet, or a lookup error all just pass through unchanged. Only a row
// that explicitly says something other than auth.OrgStatusActive ever
// blocks - which nothing writes today, so this is a no-op in practice
// until a real billing flow starts writing non-active statuses.
func RequireActiveOrganization(store *auth.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgID := CurrentOrganizationID(c)
		if orgID == "" {
			return c.Next()
		}

		status, err := store.OrganizationStatus(c.Context(), orgID)
		if err != nil || status == "" || status == auth.OrgStatusActive {
			return c.Next()
		}

		// No dedicated billing/paused page exists yet - a plain message is
		// the honest placeholder until one does.
		return c.Status(fiber.StatusForbidden).SendString(
			"This organization's access is currently paused. Contact support to resolve it.",
		)
	}
}
