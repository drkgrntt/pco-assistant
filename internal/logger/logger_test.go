package logger

import (
	"context"
	"testing"
)

func TestWithRequestIDRoundTrips(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	args := withRequestIDArg(ctx, []any{"key", "value"})

	want := []any{"request_id", "req-123", "key", "value"}
	if len(args) != len(want) {
		t.Fatalf("expected %#v, got %#v", want, args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("expected %#v, got %#v", want, args)
			break
		}
	}
}

func TestWithRequestIDBlankIsNoop(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	args := withRequestIDArg(ctx, []any{"key", "value"})

	want := []any{"key", "value"}
	if len(args) != len(want) || args[0] != want[0] || args[1] != want[1] {
		t.Errorf("expected args unchanged (%#v), got %#v", want, args)
	}
}

func TestWithRequestIDAbsentFromPlainContext(t *testing.T) {
	args := withRequestIDArg(context.Background(), []any{"key", "value"})

	want := []any{"key", "value"}
	if len(args) != len(want) || args[0] != want[0] || args[1] != want[1] {
		t.Errorf("expected args unchanged (%#v), got %#v", want, args)
	}
}

// TestContextFunctionsDoNotPanic is a smoke test, not a behavior
// assertion (slog's own handler formatting isn't this package's to test) -
// it just confirms every *Context function is wired up correctly and
// doesn't panic with or without a request id present.
func TestContextFunctionsDoNotPanic(t *testing.T) {
	withID := WithRequestID(context.Background(), "req-456")
	withoutID := context.Background()

	for _, ctx := range []context.Context{withID, withoutID} {
		InfoContext(ctx, "info msg", "k", "v")
		WarnContext(ctx, "warn msg", "k", "v")
		DebugContext(ctx, "debug msg", "k", "v")
		ErrorContext(ctx, "error msg", "k", "v")
	}
}
