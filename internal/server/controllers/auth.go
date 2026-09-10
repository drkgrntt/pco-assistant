package controllers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/logger"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

const oauthStateCookieName = "church_oauth_state"

type AuthController struct {
	store *auth.Store
}

func NewAuthController(store *auth.Store) *AuthController {
	return &AuthController{store: store}
}

func (ac *AuthController) RegisterRoutes(router fiber.Router) {
	router.Get("/login", ac.LoginPage)
	router.Get("/auth/pco/start", ac.Start)
	router.Get("/auth/pco/callback", ac.Callback)
	router.Post("/auth/logout", ac.Logout)
}

func (ac *AuthController) LoginPage(c *fiber.Ctx) error {
	return render(c, views.Login())
}

// Start kicks off the PCO OAuth dance: stash a nonce in a short-lived
// cookie, redirect to PCO's authorize screen with the same nonce as `state`
// so Callback can confirm the response actually came from a request we
// started (CSRF protection for the redirect flow).
func (ac *AuthController) Start(c *fiber.Ctx) error {
	state, err := randomState()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	c.Cookie(&fiber.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Expires:  time.Now().Add(10 * time.Minute),
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})

	return c.Redirect(auth.AuthorizeURL(state))
}

func (ac *AuthController) Callback(c *fiber.Ctx) error {
	state := c.Query("state")
	if state == "" || state != c.Cookies(oauthStateCookieName) {
		return c.Status(fiber.StatusBadRequest).SendString("invalid or missing oauth state")
	}
	c.ClearCookie(oauthStateCookieName)

	code := c.Query("code")
	if code == "" {
		return c.Status(fiber.StatusBadRequest).SendString("missing code")
	}

	tokenResult, err := auth.ExchangeCode(c.Context(), code)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString(err.Error())
	}

	personID, personName, err := auth.FetchIdentity(c.Context(), tokenResult.AccessToken)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString(err.Error())
	}

	// Unlike the org name (a header nicety), the org id is load-bearing:
	// every scheduling.Store call this session makes is scoped to it (see
	// middleware.CurrentOrganizationID), so a second Planning Center
	// organization signed into this same instance never sees this one's
	// data. A lookup failure here can't just fall back to "" the way the
	// name used to - every session that ever failed this lookup would
	// collapse into the same "" bucket and start seeing each other's data,
	// which is exactly the bug this is trying to prevent. Fail sign-in
	// instead and let them retry.
	org, err := pco.GetOrganization(pco.WithAccessToken(c.Context(), tokenResult.AccessToken))
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString("looking up your Planning Center organization: " + err.Error())
	}

	// Gate sign-in on the person actually having some access to the
	// Services product in PCO - without this, anyone who can complete
	// PCO's OAuth consent screen for the org gets a session (and a billed
	// seat, see auth.SeatCounts) even with zero Services access.
	//
	// This is deliberately coarse: go-pco (and PCO's API underneath it)
	// exposes no clean "this person's Services permission level" field,
	// so the only reliable, buildable signal is whether a live call into
	// a Services endpoint succeeds or 401/403s. That can't distinguish
	// PCO's own Viewer/Scheduler/Editor/Administrator levels from each
	// other - a known, accepted limitation, not a bug - it only filters
	// out zero-access logins. GetServiceTypes with PerPage: 1 is the
	// cheapest such probe: one request, smallest page, we only care about
	// success vs. 401/403, never the actual data.
	//
	// Deliberately calls pco.GetServiceTypes directly here rather than
	// the org-scoped, cached scheduling.FetchServiceTypes wrapper used
	// elsewhere (build.go/home.go/people.go): that cache is keyed only by
	// organization id, not by which person's token made the call, so a
	// cache hit from one admin's earlier session could wrongly let a
	// different, access-less person's login through this gate.
	if _, err := pco.GetServiceTypes(pco.WithAccessToken(c.Context(), tokenResult.AccessToken), &pco.ServiceTypesParams{PerPage: 1}); isServicesAccessDenied(err) {
		return c.Status(fiber.StatusForbidden).SendString("Your Planning Center account doesn't have access to Services, so it can't be used with this app. Ask whoever manages your church's Planning Center account to grant you Services access, then try signing in again.")
	} else if err != nil {
		// Any other error (network blip, PCO 5xx, etc.) fails OPEN - log
		// and let sign-in continue, same tolerance this handler already
		// gives UpsertOrganization a few lines below. Only a confirmed
		// 401/403 (definitely no access) should ever block sign-in; an
		// ambiguous failure shouldn't lock out a legitimate admin.
		logger.ErrorContext(middleware.RequestContext(c), "checking Services access failed, allowing sign-in", "error", err)
	}

	// Best-effort, unlike the org lookup above: this is account-level
	// bookkeeping (see auth.UpsertOrganization), not something a request
	// gets scoped by, so a failure here shouldn't block sign-in.
	if err := ac.store.UpsertOrganization(c.Context(), org.Data.ID, org.Data.Attributes.Name); err != nil {
		logger.ErrorContext(middleware.RequestContext(c), "upserting organization failed", "error", err)
	}

	// End whatever session this browser already had before creating its
	// new one - switching accounts via Start/AuthorizeURL's prompt=
	// select_account (rather than an explicit Logout first) would
	// otherwise leave the old session row live until its 30-day TTL sweep
	// (DeleteExpiredSessions), during which it keeps counting as a billed
	// seat for its organization (auth.SeatCounts dedupes by person_id
	// among all unexpired sessions, not just the one this browser is
	// actually using) and keeps showing up in that org's Account page
	// "who's signed in" list. Best-effort and post-token-exchange, same
	// tolerance/ordering as UpsertOrganization above and Logout's own
	// DeleteSession call - doing this before the exchange could strand
	// someone fully logged out over an abandoned/cancelled PCO consent
	// screen, for a session that was otherwise still perfectly good.
	if oldToken := c.Cookies(middleware.SessionCookieName); oldToken != "" {
		if err := ac.store.DeleteSession(c.Context(), oldToken); err != nil {
			logger.ErrorContext(middleware.RequestContext(c), "deleting previous session on re-sign-in", "error", err)
		}
	}

	token, err := ac.store.CreateSession(c.Context(), personID, personName, org.Data.ID, org.Data.Attributes.Name, tokenResult.AccessToken, tokenResult.RefreshToken, tokenResult.ExpiresAt)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Expires:  time.Now().Add(auth.SessionTTL),
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})

	return c.Redirect("/")
}

func (ac *AuthController) Logout(c *fiber.Ctx) error {
	if token := c.Cookies(middleware.SessionCookieName); token != "" {
		_ = ac.store.DeleteSession(c.Context(), token)
	}
	c.ClearCookie(middleware.SessionCookieName)

	return c.Redirect("/login")
}

// isServicesAccessDenied reports whether err is a *pco.RequestError with a
// 401 or 403 status - i.e. PCO confirmed this person has no access, as
// opposed to an ambiguous failure (network error, PCO 5xx, timeout) that
// shouldn't block sign-in. Factored out of Callback so the classification
// itself is unit-testable without exercising the whole OAuth handshake.
func isServicesAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *pco.RequestError
	return errors.As(err, &reqErr) && (reqErr.StatusCode == 401 || reqErr.StatusCode == 403)
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
