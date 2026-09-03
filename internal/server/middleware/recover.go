package middleware

import (
	"runtime/debug"

	"pco-assistant/internal/logger"

	"github.com/gofiber/fiber/v2"
)

// Recover catches a panic anywhere downstream (a nil map write, a bad type
// assertion, an out-of-range index - the kind of bug a `return err` can't
// cover because nothing saw it coming) and turns it into a logged 500
// instead of taking down the whole process. Fiber/fasthttp already recovers
// a panic enough to keep other in-flight requests alive, but without this
// it never reaches this app's own structured logger - it just prints its
// own raw trace to stderr, invisible to anything grepping the usual
// level=... log lines.
//
// Register this right after RequestID (routes.go) - early enough to wrap
// every handler below it, and after RequestID so the log line below can
// still tag the request that panicked.
//
// Deliberately responds with a generic message rather than the panic
// value's own text, unlike this app's usual `c.Status(500).
// SendString(err.Error())` convention for a handled error return - a
// `return err` site is something a developer chose to surface, but a
// panic's value is whatever an unaudited failure happened to produce
// (which can include a fragment of a query, a struct dump, anything) - not
// safe to hand to whoever's making the request.
func Recover() fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}

			logger.Error("panic recovered",
				"request_id", CurrentRequestID(c),
				"method", c.Method(),
				"path", c.Path(),
				"panic", r,
				"stack", string(debug.Stack()),
			)

			err = c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
		}()

		return c.Next()
	}
}
