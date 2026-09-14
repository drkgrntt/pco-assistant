package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// RateLimit returns a per-IP request limiter for a single route - meant
// for the small set of routes that are either genuinely public/
// unauthenticated (the OAuth entry points, reachable before RequireAuth
// ever runs) or trigger a real per-hit side effect (feedback submission's
// outbound email) worth guarding against scripted abuse or a runaway
// client retry loop. Deliberately not applied app-wide: this app's normal
// interactive traffic (htmx fragment reloads, the Build page's 30s
// poll-while-open check) is far busier per legitimate user than any route
// this actually guards, so a blanket limiter would have to be so generous
// it wouldn't stop real abuse anyway - see each call site's own chosen
// max/window.
//
// In-memory, per-process store (limiter's own default) - fine for this
// app's single-instance deploy (see docker-compose.yml); would need a
// shared limiter.Config.Storage backend if this ever ran as more than one
// replica.
func RateLimit(max int, window time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: window,
	})
}
