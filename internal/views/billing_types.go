package views

// BillingData is the Billing page's view model - either the plan picker
// (HasPlan false) or the current plan plus a portal link (HasPlan true),
// see BillingController.buildBillingData.
type BillingData struct {
	// HasPlan is true once the org has ever completed Checkout - even if
	// Status has since lapsed (past_due, canceled), the page still shows
	// "you're on the X plan" plus a portal link to fix it there, rather
	// than reverting to the picker.
	HasPlan bool
	Status  string
	Plan    string
	// TrialDaysLeft is only meaningful while Status is "trialing" - 0
	// once the trial has actually run out (not negative).
	TrialDaysLeft int
	// StandardPlan/MultiCampusPlan are the plan code constants
	// (billing.PlanStandard/PlanMultiCampus) - passed through rather
	// than imported directly here, so this package doesn't need to
	// depend on internal/billing just for two string literals.
	StandardPlan, MultiCampusPlan string
	// CheckoutResult reflects Stripe's own redirect back
	// (?checkout=success/canceled) - a one-time banner, not stored
	// anywhere.
	CheckoutResult string
	// SeatCount is how many distinct people have signed in for this org
	// recently (see auth.SeatCounts) - shown for both plans. Multi-Campus
	// actually bills for seats beyond its included 5 (a graduated Stripe
	// Price - see billing.SyncSeatQuantity), so this is just informational
	// there; for Standard it's what StandardOverLimit is based on.
	SeatCount int
	// StandardOverLimit is true when a Standard org has more distinct
	// seats than billing.SeatsIncludedStandard - Standard has no paid
	// overage at all, so this only ever drives a visible upsell nudge
	// toward Multi-Campus, never a block.
	StandardOverLimit bool
	ErrMsg            string
}
