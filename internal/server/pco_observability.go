package server

import (
	"net/http"

	pco "github.com/drkgrntt/go-pco"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/server/middleware"
)

// registerPCOResponseHook wires go-pco's opt-in observability hook
// (pco.SetResponseHook, go-pco v0.19.0+) to this app's own logger. go-pco
// deliberately never logs anything itself - a library shouldn't assume a
// consumer's logging setup - so without this, a PCO 429 leaves zero trace
// anywhere: most call sites treat a PCO error as best-effort (see
// docs/pco-api-risks.md) and silently degrade rather than surface it, and
// even the handful that do surface one (an inline ErrMsg on the rendered
// page, e.g. buildSetBuilderData's "loading plan from Planning Center:
// ...") never reach this app's own logs. Called once from server.New() -
// see SetResponseHook's own doc comment for why a second call would just
// replace, not add to, this one.
//
// Beyond logging, the 429 branch also feeds the page-level "some details
// may be stale" banner (docs/plans/in-progress/
// pco-rate-limit-user-visibility-plan.md's Phase 1) via
// middleware.RecordPCORateLimit - this is the only place in the app that
// sees every PCO 429 regardless of which read-path call site triggered
// it, so it's the natural place to mark "a 429 happened during this
// request" rather than threading that signal through every individual
// best-effort call site.
//
// Deliberately logs only failures, not every attempt: a single Build page
// render can legitimately make 100-200 PCO calls (see docs/
// pco-api-risks.md's "PCO API calls" section) - logging every success
// would flood the log for no diagnostic benefit. A 429 gets its own
// distinct, grep-able message with RetryAfter/Attempt/ThrottleWait, so a
// future occurrence shows whether the throttle/retry (see go-pco's
// README, both configured via PCO_THROTTLE_*/PCO_RETRY_ON_429_* env vars)
// actually engaged - that's the one this app has already been paged
// about (a real 429 got through despite the throttle being on, see the
// "priority-aware request throttle" addendum in docs/pco-api-risks.md).
// Any other failure still gets logged, just without the 429-specific
// detail.
//
// Uses logger.*Context, not the plain versions: info.Ctx (go-pco v0.20.0+)
// is the exact context every pco.* call site in this app was made with -
// middleware.RequestContext(c)'s own or one built via logger.WithRequestID
// for a background goroutine (see song_arrangement_cache.go/
// person_data_cache.go) - so it already carries a request_id
// logger.WarnContext folds in automatically. This is the one log site that
// needed go-pco itself to change to get a request_id at all: unlike every
// other call site in this app, a hook registered via SetResponseHook has
// no fiber.Ctx and no ctx param of its own to read one from.
func registerPCOResponseHook() {
	pco.SetResponseHook(func(info pco.ResponseInfo) {
		if info.Err == nil {
			return
		}

		if info.StatusCode == http.StatusTooManyRequests {
			logger.WarnContext(info.Ctx, "PCO rate limit hit",
				"method", info.Method,
				"url", info.URL,
				"attempt", info.Attempt,
				"retry_after", info.RetryAfter,
				"throttle_wait", info.ThrottleWait,
				"duration", info.Duration,
			)

			// Beyond the log line above, also mark this request's id (when
			// there is one) so controllers/render.go can surface a
			// page-level "some details may be stale" banner on this same
			// request's render - see middleware.RecordPCORateLimit's own
			// doc comment for the full read-once mechanism. info.Ctx is
			// the exact context the failing pco.* call was made with, so
			// this is the one place a request-less caller (a cron job, a
			// CLI tool) simply never hits this branch with an id to record.
			if id, ok := logger.RequestIDFromContext(info.Ctx); ok {
				middleware.RecordPCORateLimit(id)
			}
			return
		}

		logger.WarnContext(info.Ctx, "PCO request failed",
			"method", info.Method,
			"url", info.URL,
			"status_code", info.StatusCode,
			"attempt", info.Attempt,
			"error", info.Err,
		)
	})
}
