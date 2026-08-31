package controllers

import (
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/billing"
	"pco-assistant/internal/logger"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

type BillingController struct {
	authStore     *auth.Store
	billingClient *billing.Client
}

func NewBillingController(authStore *auth.Store, billingClient *billing.Client) *BillingController {
	return &BillingController{authStore: authStore, billingClient: billingClient}
}

func (bc *BillingController) RegisterRoutes(router fiber.Router) {
	group := router.Group("/billing")

	group.Get("/", bc.BillingPage)
	group.Post("/checkout", bc.CreateCheckout)
	group.Post("/portal", bc.Portal)
	// No RequireAuth/RequireActiveOrganization on this one - see
	// publicPrefixes (middleware/auth.go) - Stripe calls it directly,
	// authenticating with a signature, not a session.
	group.Post("/webhook", bc.Webhook)
}

// BillingPage shows either the plan picker (no plan chosen yet) or the
// current plan plus a "Manage billing" link into Stripe's own portal,
// depending on views.BillingData.HasPlan - see buildBillingData.
func (bc *BillingController) BillingPage(c *fiber.Ctx) error {
	data := bc.buildBillingData(c)
	data.CheckoutResult = c.Query("checkout")

	return render(c, views.Billing(bc.renderBilling(c, data)))
}

// buildBillingData resolves the signed-in org's current billing/trial
// state - shared by every handler below so each one's error/success
// re-render reflects the same real state, not a stale copy.
func (bc *BillingController) buildBillingData(c *fiber.Ctx) views.BillingData {
	orgID := middleware.CurrentOrganizationID(c)

	data := views.BillingData{
		StandardPlan:    billing.PlanStandard,
		MultiCampusPlan: billing.PlanMultiCampus,
	}

	org, err := bc.authStore.GetOrganization(c.Context(), orgID)
	if err != nil {
		data.ErrMsg = "loading your organization: " + err.Error()
		return data
	}

	data.Status = org.Status
	data.Plan = org.Plan
	data.HasPlan = org.Plan != ""
	if org.Status == auth.OrgStatusTrialing {
		data.TrialDaysLeft = int(time.Until(org.TrialEndsAt).Hours() / 24)
		if data.TrialDaysLeft < 0 {
			data.TrialDaysLeft = 0
		}
	}

	// Best-effort, same tolerance as every other lookup on this page - a
	// failure here just means the seat count/nag doesn't show, not a
	// broken page.
	if counts, err := bc.authStore.SeatCounts(c.Context()); err == nil {
		data.SeatCount = counts[orgID]
	}
	data.StandardOverLimit = org.Plan == billing.PlanStandard && data.SeatCount > billing.SeatsIncludedStandard

	return data
}

// renderBilling is the one place that resolves personName/orgName for
// views.Billing, so every handler's re-render (success or error) uses
// the same call rather than repeating both locals lookups each time.
func (bc *BillingController) renderBilling(c *fiber.Ctx, data views.BillingData) (string, string, views.BillingData) {
	_, personName := middleware.CurrentPerson(c)
	return personName, middleware.CurrentOrganization(c), data
}

func (bc *BillingController) CreateCheckout(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body struct {
		Plan string `form:"plan"`
	}
	if err := c.BodyParser(&body); err != nil {
		data := bc.buildBillingData(c)
		data.ErrMsg = err.Error()
		return render(c, views.Billing(bc.renderBilling(c, data)))
	}

	url, err := bc.billingClient.CreateCheckoutSession(middleware.RequestContext(c), orgID, body.Plan)
	if err != nil {
		data := bc.buildBillingData(c)
		data.ErrMsg = "starting checkout: " + err.Error()
		return render(c, views.Billing(bc.renderBilling(c, data)))
	}

	return c.Redirect(url)
}

func (bc *BillingController) Portal(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	org, err := bc.authStore.GetOrganization(c.Context(), orgID)
	if err != nil || org.StripeCustomerID == "" {
		data := bc.buildBillingData(c)
		data.ErrMsg = "no billing account on file yet - choose a plan first"
		return render(c, views.Billing(bc.renderBilling(c, data)))
	}

	url, err := bc.billingClient.CreatePortalSession(middleware.RequestContext(c), org.StripeCustomerID)
	if err != nil {
		data := bc.buildBillingData(c)
		data.ErrMsg = "opening billing portal: " + err.Error()
		return render(c, views.Billing(bc.renderBilling(c, data)))
	}

	return c.Redirect(url)
}

// Webhook has no session/organization context at all - Stripe calls it
// directly (see RegisterRoutes/publicPrefixes) - so it authenticates the
// request itself via the signature header, not middleware.
func (bc *BillingController) Webhook(c *fiber.Ctx) error {
	sig := c.Get("Stripe-Signature")
	if err := bc.billingClient.HandleWebhook(c.Context(), bc.authStore, c.Body(), sig); err != nil {
		logger.Error("stripe webhook error", "error", err)
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}

	return c.SendStatus(fiber.StatusOK)
}
