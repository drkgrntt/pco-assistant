package middleware

import (
	"context"
	"strings"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/logger"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

const SessionCookieName = "church_session"

// refreshBuffer is how far ahead of a session's PCO token actually
// expiring RequireAuth proactively refreshes it - big enough that a
// request's own handling time can never land past the real expiry.
const refreshBuffer = 5 * time.Minute

// publicPrefixes never require a session: infra health checks, the sign-in
// flow itself, static assets, the PWA manifest/service worker (which
// must be reachable pre-login for install prompts and are root-served, not
// under /static/, so they need their own entries here), and Stripe's
// webhook (which authenticates itself via a signature - see
// internal/billing.HandleWebhook - not a session cookie, since Stripe's
// servers have neither).
var publicPrefixes = []string{"/health", "/login", "/auth/", "/static/", "/sw.js", "/manifest.json", "/billing/webhook"}

// RequireAuth gates every route not covered by publicPrefixes behind a
// valid, unexpired session. On success it stashes the signed-in person's id
// and name, and their organization's id and name, in fiber locals for
// handlers/views to use, and - the reason this runs on every request rather
// than just once at login - builds the context every downstream PCO API
// call for this request needs to authenticate as that person (see
// RequestContext) instead of a fixed Personal Access Token, refreshing
// their PCO access token first if it's close to expiring.
func RequireAuth(store *auth.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		for _, prefix := range publicPrefixes {
			if c.Path() == prefix || strings.HasPrefix(c.Path(), prefix) {
				return c.Next()
			}
		}

		// "/" is neither a hard-auth path nor a no-locals-at-all public one
		// like publicPrefixes above - it's soft-auth: a real session still
		// gets full locals and a refreshed token (same as any other route,
		// so the signed-in dashboard keeps working exactly as before), but
		// a missing/invalid one proceeds anyway instead of redirecting, so
		// the handler (HomeController.Index) can render the public landing
		// page for a signed-out visitor rather than bouncing them to
		// /login. Every failure path below checks this before falling
		// through to redirectToLogin.
		softAuth := c.Path() == "/"

		// Any lookup failure (expired, unknown token, DB error) just falls
		// through to the redirect below.
		if token := c.Cookies(SessionCookieName); token != "" {
			if session, err := store.GetSession(c.Context(), token); err == nil {
				// OrganizationID is what every scheduling.Store call this
				// request makes gets scoped to (see CurrentOrganizationID) -
				// a session without one predates that scoping (or hit a
				// sign-in bug) and letting it through would mean either an
				// unscoped query or, worse, every such session sharing the
				// same "" bucket. Treat it exactly like a failed token
				// refresh: sign out and make them sign in again properly.
				if session.OrganizationID == "" {
					logger.Error("session has no organization id, signing out", "person_id", session.PersonID)
					return signOutAndReject(c, store, token, softAuth)
				}

				accessToken := session.AccessToken

				if time.Now().Add(refreshBuffer).After(session.TokenExpiresAt) {
					result, err := auth.RefreshAccessToken(c.Context(), session.RefreshToken)
					if err != nil {
						// Their PCO grant may have been revoked, or the
						// refresh token itself expired - there's no PAT to
						// fall back to for a specific person, so this has
						// to be treated as session-invalid, not ignored.
						logger.Error("refreshing PCO token failed, signing out", "error", err)
						return signOutAndReject(c, store, token, softAuth)
					}

					if err := store.UpdateSessionTokens(c.Context(), token, result.AccessToken, result.RefreshToken, result.ExpiresAt); err != nil {
						logger.Error("saving refreshed PCO token failed", "error", err)
					}
					accessToken = result.AccessToken
				}

				c.Locals("person_id", session.PersonID)
				c.Locals("person_name", session.PersonName)
				c.Locals("organization_id", session.OrganizationID)
				c.Locals("organization_name", session.OrganizationName)
				c.Locals("pco_ctx", pco.WithAccessToken(c.Context(), accessToken))
				c.Locals("access_token", accessToken)
				return c.Next()
			}
		}

		if softAuth {
			return c.Next()
		}
		return redirectToLogin(c)
	}
}

// signOutAndReject is the second half of every "this session is no longer
// good" path in RequireAuth (no organization id, a failed access-token
// refresh): delete the session, then either let a soft-auth route through
// unauthenticated or redirect to /login like any other rejected session.
// Callers log their own reason first, since that message differs each time.
func signOutAndReject(c *fiber.Ctx, store *auth.Store, token string, softAuth bool) error {
	_ = store.DeleteSession(c.Context(), token)
	if softAuth {
		return c.Next()
	}
	return redirectToLogin(c)
}

func redirectToLogin(c *fiber.Ctx) error {
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}

	return c.Redirect("/login")
}

// CurrentPerson reads the signed-in person's id/name that RequireAuth
// stashed in locals. Only meaningful on routes RequireAuth let through.
func CurrentPerson(c *fiber.Ctx) (id, name string) {
	id, _ = c.Locals("person_id").(string)
	name, _ = c.Locals("person_name").(string)
	return id, name
}

// CurrentOrganization reads the signed-in person's PCO organization (church)
// name that RequireAuth stashed in locals - display only, see
// Session.OrganizationName. Only meaningful on routes RequireAuth let
// through.
func CurrentOrganization(c *fiber.Ctx) string {
	name, _ := c.Locals("organization_name").(string)
	return name
}

// CurrentOrganizationID reads the signed-in person's PCO organization id -
// unlike CurrentOrganization's name, this is load-bearing: pass it to every
// scheduling.Store call a handler makes, so this session's requests only
// ever read or write its own organization's data. RequireAuth already
// refuses to let a session through at all without one (see its own
// comment), so an empty return here should only ever happen on a route
// RequireAuth didn't run on - callers shouldn't treat "" as a real,
// queryable organization.
func CurrentOrganizationID(c *fiber.Ctx) string {
	id, _ := c.Locals("organization_id").(string)
	return id
}

// RequestContext returns the context RequireAuth built for this request -
// c.Context() wrapped with the signed-in person's PCO access token (see
// go-pco.WithAccessToken), so every pco.* call made with it authenticates
// as them. Use this instead of c.Context() anywhere a handler is about to
// call into go-pco. Falls back to plain c.Context() on routes RequireAuth
// didn't run a token into (the public routes - none of which call PCO
// today, but this keeps that safe by default rather than panicking).
func RequestContext(c *fiber.Ctx) context.Context {
	if ctx, ok := c.Locals("pco_ctx").(context.Context); ok {
		return ctx
	}
	return c.Context()
}

// AccessToken returns the signed-in person's raw PCO OAuth access token as
// a plain string, independent of the fiber request's own context/lifecycle.
// RequestContext's pco_ctx already carries this token, but go-pco's
// WithAccessToken stores it under an unexported context key precisely so
// nothing outside that package can read it back out - so a caller that
// needs the token itself, not just a context authenticated with it, reads
// it from here instead. The one real use case: a handler that launches a
// goroutine outdoing the request (e.g. SyncSongInfo in
// internal/server/controllers/song_arrangement_cache.go) needs the token
// to build a fresh context.Background()-derived context for that
// goroutine, since c.Context() (and anything derived from it, including
// pco_ctx) is cancelled the moment the handler returns. Only meaningful on
// routes RequireAuth let through.
func AccessToken(c *fiber.Ctx) string {
	token, _ := c.Locals("access_token").(string)
	return token
}
