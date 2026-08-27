package billing

import (
	"context"
	"encoding/json"
	"fmt"

	"pco-assistant/internal/auth"

	"github.com/stripe/stripe-go/v86"
)

// HandleWebhook verifies payload/sigHeader against this Client's
// webhookSecret (see Stripe's own signing scheme - forging this without
// the secret isn't feasible) and reacts to the three subscription
// lifecycle events this app cares about, writing the result into
// organizations via store. Unrecognized event types are ignored, not
// errors - Stripe sends many event types this app has no opinion on.
func (c *Client) HandleWebhook(ctx context.Context, store *auth.Store, payload []byte, sigHeader string) error {
	// WithIgnoreAPIVersionMismatch: stripe-go pins an expected API version
	// and otherwise rejects a real event outright if the Stripe account's
	// configured webhook API version doesn't exactly match it - a
	// mismatch that's easy to hit in practice (SDK and Dashboard version
	// drift independently) and has nothing to do with whether the
	// signature itself is genuine. Confirmed live against a
	// self-constructed signed test payload during development - without
	// this, ConstructEvent rejected an otherwise-valid, correctly-signed
	// payload purely for lacking a matching api_version field.
	event, err := c.sc.ConstructEvent(payload, sigHeader, c.webhookSecret, stripe.WithIgnoreAPIVersionMismatch())
	if err != nil {
		return fmt.Errorf("verifying webhook signature: %w", err)
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		return c.handleCheckoutCompleted(ctx, store, event.Data.Raw)
	case stripe.EventTypeCustomerSubscriptionUpdated:
		return c.handleSubscriptionChanged(ctx, store, event.Data.Raw)
	case stripe.EventTypeCustomerSubscriptionDeleted:
		return c.handleSubscriptionDeleted(ctx, store, event.Data.Raw)
	}

	return nil
}

// handleCheckoutCompleted fires once, right after someone finishes
// paying for the first time. The checkout session itself only carries
// ClientReferenceID (the organization id) and bare Customer/Subscription
// ids - the subscription is re-fetched live for its real Status and
// Price, rather than trusting whatever the session object happens to
// have inlined, since Checkout doesn't expand those by default.
func (c *Client) handleCheckoutCompleted(ctx context.Context, store *auth.Store, raw []byte) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return fmt.Errorf("decoding checkout session: %w", err)
	}
	if session.ClientReferenceID == "" || session.Subscription == nil {
		return fmt.Errorf("checkout session %s missing client_reference_id or subscription", session.ID)
	}

	sub, err := c.sc.V1Subscriptions.Retrieve(ctx, session.Subscription.ID, nil)
	if err != nil {
		return fmt.Errorf("retrieving subscription %s: %w", session.Subscription.ID, err)
	}

	return store.UpdateOrganizationSubscription(
		ctx, session.ClientReferenceID,
		session.Customer.ID, sub.ID, string(sub.Status), c.planForSubscription(sub),
	)
}

// handleSubscriptionChanged covers any later change to an existing
// subscription (upgrade/downgrade, a payment failing into past_due,
// Stripe reporting the plan is unpaid, etc.) - the event's own payload
// already is the full Subscription object, no re-fetch needed.
// organization_id comes from the subscription's own metadata (set at
// Checkout time - see CreateCheckoutSession), not a DB lookup by
// subscription id.
func (c *Client) handleSubscriptionChanged(ctx context.Context, store *auth.Store, raw []byte) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(raw, &sub); err != nil {
		return fmt.Errorf("decoding subscription: %w", err)
	}

	orgID := sub.Metadata["organization_id"]
	if orgID == "" {
		return fmt.Errorf("subscription %s has no organization_id metadata", sub.ID)
	}

	return store.UpdateOrganizationSubscription(
		ctx, orgID,
		sub.Customer.ID, sub.ID, string(sub.Status), c.planForSubscription(&sub),
	)
}

// handleSubscriptionDeleted marks the org canceled - plan and Stripe ids
// are left as a historical record rather than cleared, since nothing
// currently reads them once status isn't active.
func (c *Client) handleSubscriptionDeleted(ctx context.Context, store *auth.Store, raw []byte) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(raw, &sub); err != nil {
		return fmt.Errorf("decoding subscription: %w", err)
	}

	orgID := sub.Metadata["organization_id"]
	if orgID == "" {
		return fmt.Errorf("subscription %s has no organization_id metadata", sub.ID)
	}

	return store.UpdateOrganizationSubscription(
		ctx, orgID,
		sub.Customer.ID, sub.ID, string(stripe.SubscriptionStatusCanceled), c.planForSubscription(&sub),
	)
}

// planForSubscription reads the plan off the subscription's first item's
// Price - a subscription created through CreateCheckoutSession only ever
// has the one line item, so there's nothing to reconcile across
// multiple items.
func (c *Client) planForSubscription(sub *stripe.Subscription) string {
	if sub.Items == nil || len(sub.Items.Data) == 0 || sub.Items.Data[0].Price == nil {
		return ""
	}
	return c.PlanForPriceID(sub.Items.Data[0].Price.ID)
}
