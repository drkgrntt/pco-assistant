package controllers

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"time"

	"pco-assistant/internal/auth"
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

	// Best-effort, unlike the org lookup above: this is account-level
	// bookkeeping (see auth.UpsertOrganization), not something a request
	// gets scoped by, so a failure here shouldn't block sign-in.
	if err := ac.store.UpsertOrganization(c.Context(), org.Data.ID, org.Data.Attributes.Name); err != nil {
		log.Println("upserting organization failed:", err.Error())
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

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
