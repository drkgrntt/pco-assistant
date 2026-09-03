package middleware

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRecoverCatchesPanicAndReturns500(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Use(Recover())
	app.Get("/", func(c *fiber.Ctx) error {
		panic("boom")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test itself errored (panic escaped Recover): %v", err)
	}

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Internal Server Error" {
		t.Errorf("expected a generic body, not the panic value's own text - got %q", body)
	}
}

func TestRecoverDoesNotLeakPanicValueToResponse(t *testing.T) {
	app := fiber.New()
	app.Use(Recover())
	app.Get("/", func(c *fiber.Ctx) error {
		panic("sensitive internal detail: connection string user=admin")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Internal Server Error" {
		t.Errorf("expected the panic value to be logged, not echoed to the client - got body %q", body)
	}
}

func TestRecoverLeavesNonPanickingRequestsUntouched(t *testing.T) {
	app := fiber.New()
	app.Use(Recover())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusTeapot).SendString("no panic here")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StatusCode != fiber.StatusTeapot {
		t.Errorf("expected status %d untouched, got %d", fiber.StatusTeapot, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "no panic here" {
		t.Errorf("expected passthrough body, got %q", body)
	}
}
