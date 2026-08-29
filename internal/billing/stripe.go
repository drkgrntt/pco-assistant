// Package billing wraps the Stripe-facing side of an organization's
// subscription: creating a Checkout Session to start paying, a Billing
// Portal session to manage an existing subscription, and mapping a
// Stripe Price id back to this app's own plan codes. Kept out of
// internal/auth (which owns reading/writing organizations.* locally)
// since this is a distinct, substantial concern of its own - the same
// reason internal/scheduling and internal/auth are already separate
// packages. See webhook.go for the other half: reacting to Stripe's own
// subscription lifecycle events.
package billing

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v86"
)

// PlanStandard/PlanMultiCampus are this app's own plan codes, stored in
// organizations.plan - see the README's Setup section for what each
// includes. Deliberately not Stripe Price ids themselves, so a Price id
// can be rotated (e.g. a price change) without a data migration - only
// Client's own priceToPlan map needs to change.
const (
	PlanStandard    = "standard"
	PlanMultiCampus = "multi_campus"
)

// SeatsIncludedStandard is how many admin seats (see auth.SeatCounts)
// the Standard plan includes before /billing starts nagging toward
// Multi-Campus - Standard has no paid overage at all (unlike
// Multi-Campus's graduated Price - see SyncSeatQuantity), so going over
// this never blocks anyone or bills anything, it's purely a visible
// upsell nudge.
const SeatsIncludedStandard = 2

// Client is this app's one Stripe-facing entry point - constructed once
// at startup (see server.New) from env vars, alongside authStore/
// schedulingStore.
type Client struct {
	sc            *stripe.Client
	webhookSecret string
	priceToPlan   map[string]string
	planToPrice   map[string]string
	// successURL/cancelURL/portalReturnURL are absolute URLs (Stripe
	// requires them, not relative paths) built once from APP_BASE_URL at
	// construction time.
	successURL      string
	cancelURL       string
	portalReturnURL string
}

// New constructs a Client. priceStandard/priceMultiCampus are Stripe
// Price ids (from the Stripe Dashboard - see README's Setup section);
// webhookSecret verifies incoming webhook requests really came from
// Stripe (see HandleWebhook); baseURL is this app's own
// externally-reachable origin (e.g. "http://localhost:8080" locally,
// "https://pco.derekgarnett.com" in production) used to build Stripe's
// required absolute redirect URLs.
func New(secretKey, priceStandard, priceMultiCampus, webhookSecret, baseURL string) *Client {
	return &Client{
		sc:            stripe.NewClient(secretKey),
		webhookSecret: webhookSecret,
		priceToPlan: map[string]string{
			priceStandard:    PlanStandard,
			priceMultiCampus: PlanMultiCampus,
		},
		planToPrice: map[string]string{
			PlanStandard:    priceStandard,
			PlanMultiCampus: priceMultiCampus,
		},
		successURL:      baseURL + "/billing?checkout=success",
		cancelURL:       baseURL + "/billing?checkout=canceled",
		portalReturnURL: baseURL + "/billing",
	}
}

// PlanForPriceID reverse-maps a Stripe Price id back to this app's own
// plan code - "" if it's neither of the two configured prices (a plan
// change made directly in the Stripe Dashboard without updating
// STRIPE_PRICE_STANDARD/STRIPE_PRICE_MULTI_CAMPUS, say).
func (c *Client) PlanForPriceID(priceID string) string {
	return c.priceToPlan[priceID]
}

// CreateCheckoutSession starts a subscription-mode Checkout for the
// given plan, scoped to organizationID two ways: ClientReferenceID (read
// back on the checkout.session.completed webhook) and
// SubscriptionData.Metadata (copied onto the resulting Subscription
// object itself, so customer.subscription.* webhooks - which reference
// the subscription, not the checkout session - carry it too, with no DB
// lookup-by-subscription-id needed). Returns the URL to redirect the
// browser to.
func (c *Client) CreateCheckoutSession(ctx context.Context, organizationID, plan string) (string, error) {
	priceID, ok := c.planToPrice[plan]
	if !ok {
		return "", fmt.Errorf("unknown plan %q", plan)
	}

	session, err := c.sc.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		ClientReferenceID: stripe.String(organizationID),
		SuccessURL:        stripe.String(c.successURL),
		CancelURL:         stripe.String(c.cancelURL),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"organization_id": organizationID},
		},
	})
	if err != nil {
		return "", err
	}

	return session.URL, nil
}

// SyncSeatQuantity pushes seatCount as stripeSubscriptionID's (single)
// line item quantity - Stripe computes the graduated Multi-Campus Price
// (5 seats bundled into the base fee, $5/seat above that) directly from
// this quantity, no tier math needed on this side. Only ever called for
// Multi-Campus subscriptions (see jobs.BillingJobs) - Standard's Price
// isn't tiered, and Standard never bills for extra seats at all (see
// SeatsIncludedStandard's doc comment). The subscription is re-fetched
// live for its current line item id rather than storing one locally -
// same tradeoff as handleCheckoutCompleted's live Subscription fetch,
// one extra call for something that only runs once a day, not once a
// request.
func (c *Client) SyncSeatQuantity(ctx context.Context, stripeSubscriptionID string, seatCount int) error {
	sub, err := c.sc.V1Subscriptions.Retrieve(ctx, stripeSubscriptionID, nil)
	if err != nil {
		return fmt.Errorf("retrieving subscription %s: %w", stripeSubscriptionID, err)
	}
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return fmt.Errorf("subscription %s has no line items", stripeSubscriptionID)
	}

	_, err = c.sc.V1SubscriptionItems.Update(ctx, sub.Items.Data[0].ID, &stripe.SubscriptionItemUpdateParams{
		Quantity: stripe.Int64(int64(seatCount)),
	})
	if err != nil {
		return fmt.Errorf("updating subscription item quantity: %w", err)
	}

	return nil
}

// CreatePortalSession opens Stripe's own hosted Billing Portal for an
// already-paying org to update payment details, change plan, or cancel -
// no custom UI needed for any of that. Returns the URL to redirect the
// browser to.
func (c *Client) CreatePortalSession(ctx context.Context, stripeCustomerID string) (string, error) {
	session, err := c.sc.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(c.portalReturnURL),
	})
	if err != nil {
		return "", err
	}

	return session.URL, nil
}
