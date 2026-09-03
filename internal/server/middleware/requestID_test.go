package middleware

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRequestIDSetsHeaderAndLocal(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(CurrentRequestID(c))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	header := resp.Header.Get("X-Request-ID")
	if header == "" {
		t.Fatal("expected X-Request-ID response header to be set")
	}
	if len(header) != 36 {
		t.Errorf("expected a UUID-shaped id (36 chars), got %q (%d chars)", header, len(header))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading body: %v", err)
	}
	if string(body) != header {
		t.Errorf("expected CurrentRequestID to match the response header - got body %q, header %q", body, header)
	}
}

func TestRequestIDDiffersPerRequest(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(CurrentRequestID(c))
	})

	first, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	firstID := first.Header.Get("X-Request-ID")
	secondID := second.Header.Get("X-Request-ID")
	if firstID == "" || secondID == "" {
		t.Fatal("expected both requests to get an id")
	}
	if firstID == secondID {
		t.Errorf("expected two requests to get different ids, both got %q", firstID)
	}
}

func TestCurrentRequestIDEmptyWithoutMiddleware(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(CurrentRequestID(c))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "" {
		t.Errorf("expected empty id on a route RequestID never ran on, got %q", body)
	}
}
