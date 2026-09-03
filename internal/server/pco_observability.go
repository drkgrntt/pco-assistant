package server

import (
	"net/http"

	pco "github.com/drkgrntt/go-pco"

	"pco-assistant/internal/logger"
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
func registerPCOResponseHook() {
	pco.SetResponseHook(func(info pco.ResponseInfo) {
		if info.Err == nil {
			return
		}

		if info.StatusCode == http.StatusTooManyRequests {
			logger.Warn("PCO rate limit hit",
				"method", info.Method,
				"url", info.URL,
				"attempt", info.Attempt,
				"retry_after", info.RetryAfter,
				"throttle_wait", info.ThrottleWait,
				"duration", info.Duration,
			)
			return
		}

		logger.Warn("PCO request failed",
			"method", info.Method,
			"url", info.URL,
			"status_code", info.StatusCode,
			"attempt", info.Attempt,
			"error", info.Err,
		)
	})
}
