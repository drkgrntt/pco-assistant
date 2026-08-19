package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	authorizeURL = "https://api.planningcenteronline.com/oauth/authorize"
	tokenURL     = "https://api.planningcenteronline.com/oauth/token"
	userinfoURL  = "https://api.planningcenteronline.com/oauth/userinfo"

	// oauthScope only needs to identify who's signing in - "openid" gets us
	// the OIDC userinfo endpoint (clean sub/name/email claims); "people" is
	// requested alongside it since PCO documents scope as a per-product
	// grant and openid alone wasn't confirmed sufficient on its own.
	oauthScope = "openid people"
)

func oauthClientID() string     { return os.Getenv("PCO_OAUTH_CLIENT_ID") }
func oauthClientSecret() string { return os.Getenv("PCO_OAUTH_CLIENT_SECRET") }
func oauthRedirectURI() string  { return os.Getenv("PCO_OAUTH_REDIRECT_URI") }

// AuthorizeURL builds the URL to send the browser to start sign-in. state is
// an opaque value the caller generates and verifies on callback (CSRF
// protection for the redirect).
func AuthorizeURL(state string) string {
	q := url.Values{
		"client_id":     {oauthClientID()},
		"redirect_uri":  {oauthRedirectURI()},
		"response_type": {"code"},
		"scope":         {oauthScope},
		"state":         {state},
	}

	return authorizeURL + "?" + q.Encode()
}

// ExchangeCode trades the authorization code PCO redirected back with for an
// access token. The token is used once, by FetchIdentity, and is not
// persisted anywhere.
func ExchangeCode(ctx context.Context, code string) (accessToken string, err error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {oauthClientID()},
		"client_secret": {oauthClientSecret()},
		"redirect_uri":  {oauthRedirectURI()},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("pco oauth token exchange failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("pco oauth token response had no access_token: %s", respBody)
	}

	return parsed.AccessToken, nil
}

// FetchIdentity looks up who just signed in via PCO's OIDC userinfo
// endpoint, using the access token from ExchangeCode. Only the sub/name are
// kept (as the session's person id/name) - the access token itself is
// discarded after this call.
func FetchIdentity(ctx context.Context, accessToken string) (personID, personName string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("pco oauth userinfo failed with status %d: %s", resp.StatusCode, respBody)
	}

	var parsed struct {
		Sub   string `json:"sub"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", fmt.Errorf("parse userinfo response: %w", err)
	}
	if parsed.Sub == "" {
		return "", "", fmt.Errorf("pco oauth userinfo response had no sub: %s", respBody)
	}

	name := parsed.Name
	if name == "" {
		name = parsed.Email
	}

	return parsed.Sub, name, nil
}
