package controllers

import (
	"errors"

	pco "github.com/drkgrntt/go-pco"
)

// friendlyPCOErr turns a Planning Center permission-denied error (401/403)
// into an actionable message for the person who hit it, instead of PCO's
// raw JSON:API detail text - every other error type is returned unchanged
// (err.Error()). Used at the write-path (Publish) call sites in
// sets_publish.go/rosters_publish.go, where a real PCO permission wall is
// something a signed-in person (who otherwise has *some* PCO Services
// access - see auth.go's Callback sign-in gate) can genuinely hit if their
// PCO permission level (e.g. Services Viewer) doesn't allow writes - PCO's
// API doesn't expose that permission level directly, so the clearest signal
// available is the 401/403 the write call itself gets back.
func friendlyPCOErr(err error) string {
	if err == nil {
		return ""
	}
	var reqErr *pco.RequestError
	if errors.As(err, &reqErr) && (reqErr.StatusCode == 401 || reqErr.StatusCode == 403) {
		return "Your Planning Center account doesn't have permission to do that. Ask whoever manages your church's Planning Center account for Services Editor or Scheduler access."
	}
	return err.Error()
}
