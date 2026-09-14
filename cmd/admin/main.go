// Command admin runs a small, separate app-operator web app - not part of
// the main multi-tenant app or its auth chain at all. See
// docs/plans/in-progress/admin-tooling-plan.md for the full design
// rationale (why this is a second binary instead of a role bolted onto
// RequireAuth/RequireActiveOrganization): cross-org visibility for Derek
// as the app's operator, not any one org's admin, doesn't fit the
// existing per-org identity model, and mixing an "is-operator" check into
// the request path every real tenant's traffic runs through is exactly
// the kind of thing that's easy to get subtly wrong later.
//
// Auth is a single shared secret (ADMIN_TOKEN), checked as the password
// half of HTTP Basic Auth via fiber's basicauth middleware (constant-time
// comparison under the hood) - no PCO OAuth, no session cookie, no user
// table, deliberately outside the main app's per-request auth chain
// entirely. Binds to 127.0.0.1 only; how a request actually reaches that
// (an SSH tunnel, or a real subdomain behind nginx with this same
// shared-secret check as defense in depth) is still an open decision -
// see the plan doc's "Open questions" section. This binary works
// unchanged either way, so building it doesn't have to wait on that
// decision.
//
// Run it with:
//
//	ADMIN_TOKEN=... go run ./cmd/admin
//
// (reads the same .env as the main app for DB_*, via godotenv/autoload -
// only ADMIN_TOKEN, and optionally ADMIN_PORT, are new.)
//
// First (and so far only) feature: GET /feedback, listing every
// /feedback submission across every organization - the plan doc's own
// "first real need," deliberately not built past that yet. Add the next
// feature (org list/billing status lookups, etc.) the same way: a read
// method on the relevant store, a handler here, a route below.
package main

import (
	"crypto/subtle"
	"html/template"
	"os"

	"pco-assistant/internal/database"
	"pco-assistant/internal/feedback"
	"pco-assistant/internal/logger"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		logger.Fatal("ADMIN_TOKEN must be set - refusing to start an unauthenticated admin app")
	}

	port := os.Getenv("ADMIN_PORT")
	if port == "" {
		port = "7824"
	}

	db, err := database.Open()
	if err != nil {
		logger.Fatal("connect to database", "error", err)
	}
	defer db.Close()

	feedbackStore := feedback.NewStore(db)

	app := fiber.New()
	app.Use(basicauth.New(basicauth.Config{
		Realm: "admin",
		// Username is ignored - this is a single shared secret, not a user
		// table (see the package doc comment). subtle.ConstantTimeCompare
		// avoids a timing side-channel on the token check.
		Authorizer: func(_ string, pass string) bool {
			return subtle.ConstantTimeCompare([]byte(pass), []byte(token)) == 1
		},
	}))

	app.Get("/", func(c *fiber.Ctx) error { return c.Redirect("/feedback") })
	app.Get("/feedback", feedbackPage(feedbackStore))

	addr := "127.0.0.1:" + port
	logger.Info("admin app listening", "addr", addr)
	if err := app.Listen(addr); err != nil {
		logger.Fatal("admin app crashed", "error", err)
	}
}

// feedbackTmpl is deliberately plain html/template, not a templ component -
// this app doesn't share internal/views' design system (fonts, CSS,
// htmx JS) at all, and pulling that in for one read-only table would be
// more machinery than a single-operator lookup tool needs. html/template
// auto-escapes every field below, which matters here specifically:
// SubmitterEmail and Message are free-text, submitted by whoever filled out
// the main app's /feedback form - untrusted input, even though only Derek
// ever views this page.
var feedbackTmpl = template.Must(template.New("feedback").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Feedback - admin</title>
	<style>
		body { font-family: system-ui, sans-serif; max-width: 60rem; margin: 2rem auto; padding: 0 1rem; color: #222; }
		table { width: 100%; border-collapse: collapse; }
		th, td { text-align: left; padding: 0.5rem; border-bottom: 1px solid #ddd; vertical-align: top; }
		th { font-size: 0.85rem; text-transform: uppercase; color: #666; }
		td.message { white-space: pre-wrap; }
		td.when { white-space: nowrap; color: #666; font-size: 0.9rem; }
	</style>
</head>
<body>
	<h1>Feedback ({{ len . }})</h1>
	{{ if not . }}
		<p>No feedback submitted yet.</p>
	{{ else }}
	<table>
		<thead>
			<tr><th>When</th><th>Organization</th><th>From</th><th>Message</th></tr>
		</thead>
		<tbody>
			{{ range . }}
			<tr>
				<td class="when">{{ .CreatedAt.Format "2006-01-02 15:04" }}</td>
				<td>{{ .OrganizationName }}</td>
				<td>{{ .PersonName }}{{ if .SubmitterEmail }}<br>{{ .SubmitterEmail }}{{ end }}</td>
				<td class="message">{{ .Message }}</td>
			</tr>
			{{ end }}
		</tbody>
	</table>
	{{ end }}
</body>
</html>`))

func feedbackPage(store *feedback.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		rows, err := store.List(c.Context())
		if err != nil {
			logger.Error("listing feedback for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load feedback")
		}

		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		if err := feedbackTmpl.Execute(c.Response().BodyWriter(), rows); err != nil {
			logger.Error("rendering feedback page for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to render feedback")
		}
		return nil
	}
}
