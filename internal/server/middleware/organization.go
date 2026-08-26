package middleware

import (
	"strings"
	"time"

	"pco-assistant/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// billingPrefix is exempt from RequireActiveOrganization entirely - the
// page/actions this gate would otherwise redirect a blocked org to, plus
// the Stripe webhook (which authenticates itself, not via a session, and
// runs before RequireAuth ever populates an organization id at all - see
// RequireAuth's publicPrefixes). Without this exemption, a blocked org
// visiting /billing to actually pay would just get redirected right back
// to /billing.
const billingPrefix = "/billing"

// RequireActiveOrganization runs after RequireAuth and checks the
// signed-in session's organization status/trial (see auth.GetOrganization) -
// the actual paywall enforcement point. Fails open: no organization id in
// locals (a public route, or the "/" soft-auth path with no session -
// RequireAuth already handles both), the /billing prefix itself, no
// organization row yet, or a lookup error all just pass through
// unchanged. A trialing org is allowed until its TrialEndsAt passes;
// after that (or for any other non-active status - past_due, canceled,
// ...) it's redirected to /billing to actually resolve it, rather than a
// dead-end error.
func RequireActiveOrganization(store *auth.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == billingPrefix || strings.HasPrefix(c.Path(), billingPrefix+"/") {
			return c.Next()
		}

		orgID := CurrentOrganizationID(c)
		if orgID == "" {
			return c.Next()
		}

		org, err := store.GetOrganization(c.Context(), orgID)
		if err != nil {
			return c.Next()
		}

		switch {
		case org.Status == auth.OrgStatusActive:
			return c.Next()
		case org.Status == auth.OrgStatusTrialing && time.Now().Before(org.TrialEndsAt):
			return c.Next()
		}

		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", billingPrefix)
			return c.SendStatus(fiber.StatusOK)
		}
		return c.Redirect(billingPrefix)
	}
}
