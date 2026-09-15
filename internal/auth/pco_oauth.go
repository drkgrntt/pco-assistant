package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	authorizeURL = "https://api.planningcenteronline.com/oauth/authorize"
	tokenURL     = "https://api.planningcenteronline.com/oauth/token"
	userinfoURL  = "https://api.planningcenteronline.com/oauth/userinfo"

	// oauthScope needs to cover both who's signing in and what they can
	// then do: "openid" gets the OIDC userinfo endpoint (clean
	// sub/name/email claims); PCO scopes everything else per-product, so
	// "people" and "services" are requested alongside it - this app's own
	// API calls now authenticate as the signed-in user (see
	// go-pco.WithAccessToken), not a fixed Personal Access Token, so their
	// token needs to actually be allowed to call those products.
	oauthScope = "openid people services"
)

func oauthClientID() string     { return os.Getenv("PCO_OAUTH_CLIENT_ID") }
func oauthClientSecret() string { return os.Getenv("PCO_OAUTH_CLIENT_SECRET") }
func oauthRedirectURI() string  { return os.Getenv("PCO_OAUTH_REDIRECT_URI") }

// AuthorizeURL builds the URL to send the browser to start sign-in. state is
// an opaque value the caller generates and verifies on callback (CSRF
// protection for the redirect).
//
// prompt=select_account asks PCO's own OAuth server to show its account
// picker instead of silently re-approving off whatever PCO session cookie
// the browser already carries (PCO's own session lives in its own cookie
// jar, entirely separate from this app's - clearing this app's session on
// logout was never able to touch it). Without this, clicking "Sign in"
// right after logging out of this app - or wanting to switch which PCO
// account this app is connected to - just silently signed the same person
// back in with no login screen at all, since PCO had no reason to think
// anything needed re-approving.
//
// PCO's docs (api.planningcenteronline.com/docs/overview/authentication)
// document exactly two supported values here: select_account ("allow users
// to verify which account they will be signing in as and allow them to
// switch accounts" - PCO's own recommendation for this exact use case, and
// what powers PCO's native "Select Your Account" picker across linked
// accounts) and login (forces a full PCO logout - kills any other PCO
// product tab/session the person has open too, not just this app's
// connection - so more disruptive than switching actually calls for). This
// app sends select_account rather than login for that reason - see
// docs/plans/shipped/pco-account-switching-plan.md for the fuller
// writeup. Confirm live against a real sign-out/switch round trip rather
// than assuming docs are the whole story, per CLAUDE.md's own instruction
// for PCO-behavior claims.
func AuthorizeURL(state string) string {
	q := url.Values{
		"client_id":     {oauthClientID()},
		"redirect_uri":  {oauthRedirectURI()},
		"response_type": {"code"},
		"scope":         {oauthScope},
		"state":         {state},
		"prompt":        {"select_account"},
	}

	return authorizeURL + "?" + q.Encode()
}

// TokenResult is what PCO's token endpoint hands back - both ExchangeCode
// (the initial sign-in) and RefreshAccessToken (renewing an expiring one)
// return this same shape, since both are calls to the same endpoint with a
// different grant_type.
type TokenResult struct {
	AccessToken string
	// RefreshToken - PCO issues a *new* one on every refresh, not just at
	// initial sign-in, and the old one stops working once it has - always
	// store whatever comes back here, not just the first one.
	RefreshToken string
	ExpiresAt    time.Time
}

// ExchangeCode trades the authorization code PCO redirected back with for an
// access token (plus refresh token/expiry) - see requestToken.
func ExchangeCode(ctx context.Context, code string) (TokenResult, error) {
	return requestToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {oauthClientID()},
		"client_secret": {oauthClientSecret()},
		"redirect_uri":  {oauthRedirectURI()},
	})
}

// RefreshAccessToken trades a still-valid refresh token for a new access
// token (and a new refresh token - see TokenResult.RefreshToken) once the
// current one is at or near expiry. A failure here (e.g. the user revoked
// this app's access in PCO) should be treated like an invalid session, not
// silently ignored - there's no PAT to fall back to for a specific user.
func RefreshAccessToken(ctx context.Context, refreshToken string) (TokenResult, error) {
	return requestToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {oauthClientID()},
		"client_secret": {oauthClientSecret()},
	})
}

func requestToken(ctx context.Context, body url.Values) (TokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return TokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return TokenResult{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenResult{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResult{}, fmt.Errorf("pco oauth token request failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		// ExpiresIn is documented in seconds (PCO's token responses have
		// been observed sending it as either a JSON number or a numeric
		// string, hence json.Number rather than plain int).
		ExpiresIn    json.Number `json:"expires_in"`
		RefreshToken string      `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return TokenResult{}, fmt.Errorf("parse token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return TokenResult{}, fmt.Errorf("pco oauth token response had no access_token: %s", respBody)
	}

	expiresInSeconds, err := strconv.Atoi(parsed.ExpiresIn.String())
	if err != nil {
		return TokenResult{}, fmt.Errorf("pco oauth token response had a non-numeric expires_in %q: %w", parsed.ExpiresIn, err)
	}

	return TokenResult{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresInSeconds) * time.Second),
	}, nil
}

// FetchIdentity looks up who just signed in via PCO's OIDC userinfo
// endpoint, using the access token from ExchangeCode. sub/name/email are
// all returned - personEmail is persisted by the caller into the people
// table (see Callback, people.go), not touched here; the token itself is
// also the caller's concern (see sessions.go, which persists it for later
// API calls).
func FetchIdentity(ctx context.Context, accessToken string) (personID, personName, personEmail string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("pco oauth userinfo failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed struct {
		Sub   string `json:"sub"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", "", fmt.Errorf("parse userinfo response: %w", err)
	}
	if parsed.Sub == "" {
		return "", "", "", fmt.Errorf("pco oauth userinfo response had no sub: %s", respBody)
	}

	name := parsed.Name
	if name == "" {
		name = parsed.Email
	}

	return parsed.Sub, name, parsed.Email, nil
}
