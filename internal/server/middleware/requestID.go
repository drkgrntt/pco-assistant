package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/utils"
)

// requestIDContextKey is where RequestID (below) reads back what
// requestid.New() stashed in locals - matches that middleware's own
// ConfigDefault.ContextKey rather than inventing a second name for the
// same value.
const requestIDContextKey = "requestid"

// RequestID wraps fiber's own requestid middleware: one id per request,
// echoed back as the X-Request-ID response header and stashed in locals
// for RequestID (below) to hand to a log line. Register it first, ahead of
// everything else (including Recover) - Recover's own log line reads it
// back, and a request that never reaches this app's own handlers (a
// rejected CORS preflight, a 404) still gets an id to hand back to whoever
// reports the problem.
//
// Uses UUIDv4 rather than requestid's own default generator: that default
// is a fast atomic counter dressed up in UUID formatting, which - per its
// own doc comment - exposes roughly how many requests this server has
// handled to anyone comparing two ids. This app hands the id back in a
// response header on every request, authenticated or not, so a real random
// v4 UUID is the safer default here.
func RequestID() fiber.Handler {
	return requestid.New(requestid.Config{
		ContextKey: requestIDContextKey,
		Generator:  utils.UUIDv4,
	})
}

// CurrentRequestID reads the id RequestID generated for this request - safe
// to include in any log line as a "request_id" field so a report against
// one X-Request-ID response header can be traced back to the log lines it
// produced. Empty on a route RequestID didn't run ahead of (shouldn't
// happen - RequestID is registered first in routes.go - but callers outside
// the normal chain, e.g. a test, get "" rather than a panic).
func CurrentRequestID(c *fiber.Ctx) string {
	id, _ := c.Locals(requestIDContextKey).(string)
	return id
}
