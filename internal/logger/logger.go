// Package logger provides structured logging for the app, wrapping a single
// package-level slog.Logger so call sites don't each construct their own.
package logger

import (
	"context"
	"log/slog"
	"os"
)

var logger *slog.Logger

func init() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func Info(msg string, args ...any) {
	logger.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	logger.Warn(msg, args...)
}

func Debug(msg string, args ...any) {
	logger.Debug(msg, args...)
}

func Error(msg string, args ...any) {
	logger.Error(msg, args...)
}

// Fatal logs at error level and then exits the process. It has no
// equivalent in the source logger (slog has no process-exiting method),
// added here to replace this codebase's several log.Fatal/log.Fatalf
// startup call sites.
func Fatal(msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}

// requestIDKey is the context key WithRequestID stores under - unexported
// so nothing outside this package can set or spoof what a *Context log
// function below reports as request_id.
type requestIDKey struct{}

// WithRequestID returns a copy of ctx carrying id, for InfoContext/
// WarnContext/DebugContext/ErrorContext below to read back as a
// "request_id" field. middleware.RequestContext (internal/server/
// middleware/auth.go) already wraps this into the context it hands back
// for every request - call sites that already have that ctx in scope don't
// need this. It's only for building a fresh context.Background()-derived
// one for work that outlives the request itself (a goroutine a handler
// kicks off and returns without waiting on), mirroring how such call
// sites already carry the request's PCO access token forward via
// go-pco.WithAccessToken.
//
// A blank id is a no-op (returns ctx unchanged) rather than storing "" -
// keeps requestIDFrom's "not present" and "present but empty" cases from
// needing to be told apart.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// withRequestIDArg prepends a "request_id" key/value pair to args when ctx
// carries one (see WithRequestID) - factored out so every *Context
// function below picks it up the same way, first in the field list.
func withRequestIDArg(ctx context.Context, args []any) []any {
	id, _ := ctx.Value(requestIDKey{}).(string)
	if id == "" {
		return args
	}
	return append([]any{"request_id", id}, args...)
}

// InfoContext, WarnContext, DebugContext, and ErrorContext are the
// request-aware counterparts of Info/Warn/Debug/Error above: same
// signature plus a leading ctx, which - if it's one WithRequestID (or
// middleware.RequestContext) built - gets its request_id folded into the
// logged fields automatically. Prefer these over the plain versions
// whenever a ctx is already in scope; there's no *Context equivalent of
// Fatal since every existing Fatal call site is a startup/CLI failure with
// no request to correlate against.
func InfoContext(ctx context.Context, msg string, args ...any) {
	logger.Info(msg, withRequestIDArg(ctx, args)...)
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	logger.Warn(msg, withRequestIDArg(ctx, args)...)
}

func DebugContext(ctx context.Context, msg string, args ...any) {
	logger.Debug(msg, withRequestIDArg(ctx, args)...)
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	logger.Error(msg, withRequestIDArg(ctx, args)...)
}
