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
// entirely. Exposure is resolved as of the 2026-09-15 planning pass:
// Tailscale, not a public subdomain or SSH-tunnel-only. Binds to
// ADMIN_BIND_ADDR (default 127.0.0.1, for local dev) - in production this
// is set to the host's tailnet IP so the app is reachable directly from
// any device on Derek's tailnet and never touches a public port at all;
// ADMIN_TOKEN stays on as defense in depth even though the tailnet is the
// real access control. See the plan doc's "Admin app exposure & security"
// section for why Tailscale won over the other two options.
//
// Run it with:
//
//	ADMIN_TOKEN=... go run ./cmd/admin
//
// (reads the same .env as the main app for DB_*, via godotenv/autoload -
// only ADMIN_TOKEN, and optionally ADMIN_PORT/ADMIN_BIND_ADDR, are new.)
//
// Pages render via cmd/admin/views, real .templ components (same engine
// the main app uses, `templ generate` before building - see make admin/
// .air.admin.toml) rather than hand-rolled html/template strings - just
// its own small layout, not internal/views' design system (see that
// package's own doc comment for why this is a templating-engine choice,
// not an adoption of the main app's chrome/CSS/htmx).
//
// Features so far: GET /feedback (every /feedback submission across every
// organization - the plan doc's own "first real need"); GET /orgs (every
// organization's billing status, with a trial-extension action, plus
// people-table activity - person count/sign-ins/last active - and
// setup-page usage - how many of the 8 onboarding-checklist categories
// from the org's own home page have any data) and GET /people (everyone
// who's ever signed in, by org, with email and sign-in count - see the
// people table's own migration for why this exists separately from
// sessions). Add the next feature the same way: a read (and, if it
// writes anything, write) method on the relevant store, a views.templ
// component, a handler here, a route below.
package main

import (
	"crypto/subtle"
	"os"
	"strconv"
	"time"

	"pco-assistant/cmd/admin/views"
	"pco-assistant/internal/auth"
	"pco-assistant/internal/billing"
	"pco-assistant/internal/database"
	"pco-assistant/internal/feedback"
	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"

	"github.com/a-h/templ"
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

	bindAddr := os.Getenv("ADMIN_BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	db, err := database.Open()
	if err != nil {
		logger.Fatal("connect to database", "error", err)
	}
	defer db.Close()

	feedbackStore := feedback.NewStore(db)
	authStore := auth.NewStore(db, auth.LoadTokenEncryptionKeyFromEnv())
	schedulingStore := scheduling.NewStore(db)
	// Same env vars/constructor server.go uses for the main app's
	// billingClient - this binary just needs the one ExtendTrial method
	// off it, not the checkout/portal/webhook paths those envs also
	// cover.
	billingClient := billing.New(
		os.Getenv("STRIPE_SECRET_KEY"),
		os.Getenv("STRIPE_PRICE_STANDARD"),
		os.Getenv("STRIPE_PRICE_MULTI_CAMPUS"),
		os.Getenv("STRIPE_WEBHOOK_SECRET"),
		os.Getenv("APP_BASE_URL"),
	)

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

	app.Get("/", func(c *fiber.Ctx) error { return c.Redirect("/orgs") })
	app.Get("/feedback", feedbackPage(feedbackStore))
	app.Get("/orgs", orgsPage(authStore, schedulingStore))
	app.Post("/orgs/:id/extend-trial", extendTrial(authStore, billingClient))
	app.Get("/people", peoplePage(authStore))

	addr := bindAddr + ":" + port
	logger.Info("admin app listening", "addr", addr)
	if err := app.Listen(addr); err != nil {
		logger.Fatal("admin app crashed", "error", err)
	}
}

func feedbackPage(store *feedback.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		rows, err := store.List(c.Context())
		if err != nil {
			logger.Error("listing feedback for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load feedback")
		}
		return render(c, views.Feedback(rows))
	}
}

func orgsPage(store *auth.Store, schedulingStore *scheduling.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		orgs, err := store.ListOrganizations(c.Context())
		if err != nil {
			logger.Error("listing organizations for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load organizations")
		}
		activity, err := store.OrgActivityByOrganization(c.Context())
		if err != nil {
			logger.Error("loading org activity for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load organization activity")
		}
		setup, err := schedulingStore.SetupCompletionByOrganization(c.Context())
		if err != nil {
			logger.Error("loading setup completion for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load setup completion")
		}
		return render(c, views.Orgs(orgs, activity, setup))
	}
}

// extendTrial pushes an org's trial out by the posted number of days from
// now. Branches on whether the org has a live Stripe subscription yet:
//   - No subscription (still on the original signup trial): trial_ends_at
//     is the only source of truth (see RequireActiveOrganization), so this
//     writes it directly via auth.Store.SetOrganizationTrialEnd.
//   - Has a subscription (already paying): the local column is instead kept
//     in sync *from* Stripe (see UpdateOrganizationSubscription's
//     trialEndsAt param), so this calls billing.Client.ExtendTrial to move
//     Stripe's own trial_end - the webhook round-trips organizations.
//     trial_ends_at back automatically from there, same as any other
//     Stripe-side subscription change.
func extendTrial(store *auth.Store, billingClient *billing.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		days, err := strconv.Atoi(c.FormValue("days"))
		if err != nil || days < 1 {
			return c.Status(fiber.StatusBadRequest).SendString("days must be a positive integer")
		}

		org, err := store.GetOrganization(c.Context(), id)
		if err != nil {
			logger.Error("looking up organization to extend trial", "organization_id", id, "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to look up organization")
		}

		newTrialEnd := time.Now().Add(time.Duration(days) * 24 * time.Hour)

		if org.StripeSubscriptionID == "" {
			if err := store.SetOrganizationTrialEnd(c.Context(), id, newTrialEnd); err != nil {
				logger.Error("setting trial end for organization", "organization_id", id, "error", err)
				return c.Status(fiber.StatusInternalServerError).SendString("failed to extend trial")
			}
		} else if err := billingClient.ExtendTrial(c.Context(), org.StripeSubscriptionID, newTrialEnd); err != nil {
			logger.Error("extending stripe trial for organization", "organization_id", id, "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to extend trial in stripe")
		}

		return c.Redirect("/orgs")
	}
}

func peoplePage(store *auth.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		people, err := store.ListPeople(c.Context())
		if err != nil {
			logger.Error("listing people for admin app", "error", err)
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load people")
		}
		return render(c, views.People(people))
	}
}

// render writes a templ component as the response - same shape as the
// main app's own render.go, just local to this binary (not worth sharing
// a package for one helper function).
func render(c *fiber.Ctx, component templ.Component) error {
	c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	return component.Render(c.Context(), c.Response().BodyWriter())
}
