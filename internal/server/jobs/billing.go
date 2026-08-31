package jobs

import (
	"context"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/billing"
	"pco-assistant/internal/logger"

	"github.com/go-co-op/gocron/v2"
)

// seatReconcileInterval matches sessionCleanupInterval (auth.go) - both
// jobs care about the same "how many distinct people signed in
// recently" fact, no reason to check it on a different cadence.
var seatReconcileInterval = 24 * time.Hour

type BillingJobs struct {
	authStore     *auth.Store
	billingClient *billing.Client
}

func NewBillingJobs(authStore *auth.Store, billingClient *billing.Client) *BillingJobs {
	return &BillingJobs{authStore: authStore, billingClient: billingClient}
}

// Register reconciles every active Multi-Campus org's seat count into
// Stripe daily - see reconcileSeats. Deliberately not on the sign-in
// path (see auth.SeatCounts' doc comment on what "seat" means): a
// background job means one Stripe call per Multi-Campus org per day,
// not one per login.
func (j *BillingJobs) Register(scheduler gocron.Scheduler) {
	_, err := scheduler.NewJob(
		gocron.DurationJob(seatReconcileInterval),
		gocron.NewTask(j.reconcileSeats),
	)
	if err != nil {
		logger.Error("registering seat reconciliation cron failed", "error", err)
	}
}

func (j *BillingJobs) reconcileSeats() {
	ctx := context.Background()

	orgs, err := j.authStore.ListOrganizations(ctx)
	if err != nil {
		logger.Error("listing organizations for seat reconciliation failed", "error", err)
		return
	}

	counts, err := j.authStore.SeatCounts(ctx)
	if err != nil {
		logger.Error("computing seat counts failed", "error", err)
		return
	}

	for _, org := range orgs {
		// Only a paid, active Multi-Campus subscription has a graduated
		// Stripe Price to push a quantity into - Standard never bills for
		// extra seats (see billing.SeatsIncludedStandard), and a
		// trialing/canceled/past_due org has no subscription item worth
		// updating either way.
		if org.Status != auth.OrgStatusActive || org.Plan != billing.PlanMultiCampus {
			continue
		}

		if err := j.billingClient.SyncSeatQuantity(ctx, org.StripeSubscriptionID, counts[org.ID]); err != nil {
			logger.Error("syncing seat quantity for org failed", "organization_id", org.ID, "error", err)
		}
	}
}
