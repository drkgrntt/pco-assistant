package controllers

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

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

	accessToken, err := auth.ExchangeCode(c.Context(), code)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString(err.Error())
	}

	personID, personName, err := auth.FetchIdentity(c.Context(), accessToken)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString(err.Error())
	}

	token, err := ac.store.CreateSession(c.Context(), personID, personName)
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
