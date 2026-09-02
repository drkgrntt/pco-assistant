package controllers

// Unit test for isServicesAccessDenied - the classification logic behind
// Callback's login-time Services-access gate (see auth.go). This is
// deliberately narrower than a full handler-level test: internal/auth's
// ExchangeCode/FetchIdentity hit hardcoded PCO URLs
// (tokenURL/userinfoURL in pco_oauth.go) via http.DefaultClient directly,
// with no base-URL override seam the way go-pco has
// (pco.SetBaseURLForTesting) - so there's no practical way to stand up an
// httptest.Server for the whole OAuth code-exchange/userinfo/org-lookup/
// probe chain Callback runs, the way build_httptest_test.go and friends do
// for pure PCO-data-fetching code. Testing the classification function
// directly still covers the actual decision this gate makes: given an
// error from the GetServiceTypes probe, does sign-in get blocked or not.

import (
	"errors"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

func TestIsServicesAccessDenied(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "403 blocks sign-in",
			err:  &pco.RequestError{StatusCode: 403},
			want: true,
		},
		{
			name: "401 blocks sign-in",
			err:  &pco.RequestError{StatusCode: 401},
			want: true,
		},
		{
			name: "500 fails open (does not block)",
			err:  &pco.RequestError{StatusCode: 500},
			want: false,
		},
		{
			name: "429 fails open (does not block)",
			err:  &pco.RequestError{StatusCode: 429},
			want: false,
		},
		{
			name: "non-RequestError (e.g. a network blip) fails open",
			err:  errors.New("connection reset by peer"),
			want: false,
		},
		{
			name: "nil error (successful probe) does not block",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isServicesAccessDenied(tt.err); got != tt.want {
				t.Errorf("isServicesAccessDenied(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
