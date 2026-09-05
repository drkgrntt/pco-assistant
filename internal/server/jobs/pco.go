package jobs

// The background-sync cron - Phase 3 of
// docs/pco-call-efficiency-rollout-plan.md, full design in
// docs/pco-background-sync-plan.md. On each tick, for every organization
// with at least one live session, borrow that session's token (refreshing
// it first), and kick off the same paced Song Info / person-data syncs an
// ordinary render already triggers - so an org nobody's actively visiting
// still gets warm caches instead of only ever catching up the first time
// someone opens a page again.

import (
	"context"
	"sync"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/controllers"

	pco "github.com/drkgrntt/go-pco"
	"github.com/go-co-op/gocron/v2"
	"github.com/gofiber/fiber/v2/utils"
)

// backgroundSyncInterval - see docs/pco-background-sync-plan.md's "Cadence"
// section: this job spends a real chunk of an org's own PCO budget every
// time it actually finds work (unlike AuthJobs'/BillingJobs' 24h pure-
// housekeeping cadence), so it runs more often, but is a cheap no-op for
// the vast majority of orgs on the vast majority of ticks - one cached
// FetchActiveSongs call, a couple of local DB reads, done. Chosen as a
// reasonable starting point per that doc's own framing ("revisit against
// real prod behavior once shipped rather than tuning this number
// speculatively now"), not a measurement.
var backgroundSyncInterval = time.Hour

// backgroundSyncSessionStore is the subset of *auth.Store this job needs -
// narrowed to an interface so runOrgPass's branching (refresh
// succeeds/fails) is unit-testable against a fake in-memory implementation,
// without a real Postgres connection - see pco_test.go. *auth.Store
// satisfies this automatically.
type backgroundSyncSessionStore interface {
	SessionForBackgroundSync(ctx context.Context, organizationID string) (auth.Session, bool, error)
	UpdateSessionTokensByID(ctx context.Context, id, accessToken, refreshToken string, tokenExpiresAt time.Time) error
	DeleteSessionByID(ctx context.Context, id string) error
}

// refreshAccessTokenFn abstracts auth.RefreshAccessToken for testability -
// see TestRunOrgPassDeletesSessionOnRefreshFailure/
// TestRunOrgPassPersistsRefreshedTokenAndSyncsOnSuccess (pco_test.go),
// which inject a fake instead of making a real PCO OAuth call.
type refreshAccessTokenFn func(ctx context.Context, refreshToken string) (auth.TokenResult, error)

// syncOrgFn abstracts controllers.RunOrgBackgroundSync for testability -
// same reasoning as refreshAccessTokenFn.
type syncOrgFn func(ctx context.Context, store *scheduling.Store, organizationID string)

// pcoBackgroundSyncMu/pcoBackgroundSyncInProgress guard against this same
// org's per-org pass still running (a slow tick - PCO calls, not just local
// DB reads, in discoverRecentPersonTeamPairs) when the next hourly tick
// fires, mirroring songSyncInProgress/personSyncInProgress's exact
// check-and-set shape in internal/server/controllers - a defensive,
// process-lifetime guard, not expected to matter on a healthy hourly
// cadence, but cheap and worth having rather than assuming it never
// overlaps.
var (
	pcoBackgroundSyncMu         sync.Mutex
	pcoBackgroundSyncInProgress = map[string]bool{}
)

// pcoBackgroundSyncTryStart is the pure check-and-set at the core of the
// guard above - split out so it's unit-testable against a plain map,
// without a goroutine or a real mutex, same as
// controllers.songSyncTryStart/personSyncTryStart. Callers hold
// pcoBackgroundSyncMu for the duration of this call.
func pcoBackgroundSyncTryStart(inProgress map[string]bool, organizationID string) bool {
	if inProgress[organizationID] {
		return false
	}
	inProgress[organizationID] = true
	return true
}

type PCOJobs struct {
	authStore       *auth.Store
	schedulingStore *scheduling.Store
}

func NewPCOJobs(authStore *auth.Store, schedulingStore *scheduling.Store) *PCOJobs {
	return &PCOJobs{authStore: authStore, schedulingStore: schedulingStore}
}

// Register runs the background sync hourly - see backgroundSyncInterval.
func (j *PCOJobs) Register(scheduler gocron.Scheduler) {
	_, err := scheduler.NewJob(
		gocron.DurationJob(backgroundSyncInterval),
		gocron.NewTask(j.runBackgroundSync),
	)
	if err != nil {
		logger.Error("registering PCO background sync cron failed", "error", err)
	}
}

// runBackgroundSync is one tick: every organization gets its own pass,
// each independent of the others (one org's failure - no session, a dead
// refresh token, a PCO error - never blocks the rest).
func (j *PCOJobs) runBackgroundSync() {
	ctx := context.Background()

	orgs, err := j.authStore.ListOrganizations(ctx)
	if err != nil {
		logger.Error("PCO background sync: listing organizations failed", "error", err)
		return
	}

	for _, org := range orgs {
		j.runOrgPass(ctx, org.ID)
	}
}

// runOrgPass wraps runOrgPassWith with this job's real dependencies -
// see that function for the actual per-org pass logic (steps 1-3 of
// docs/pco-background-sync-plan.md's "Per-org pass" section; steps 4-6 are
// controllers.RunOrgBackgroundSync).
func (j *PCOJobs) runOrgPass(ctx context.Context, organizationID string) {
	runOrgPassWith(ctx, j.authStore, auth.RefreshAccessToken, j.schedulingStore, controllers.RunOrgBackgroundSync, organizationID)
}

// runOrgPassWith is runOrgPass's actual logic, parameterized over the
// session store, token refresher, and sync function so it's unit-testable
// with fakes for all three (pco_test.go) - no real Postgres or PCO OAuth
// endpoint required.
//
// Guarded by pcoBackgroundSyncTryStart/pcoBackgroundSyncInProgress for the
// whole pass (not just the sync triggers inside sync, which already have
// their own independent guards) - see that guard's own doc comment.
func runOrgPassWith(ctx context.Context, sessionStore backgroundSyncSessionStore, refresh refreshAccessTokenFn, schedulingStore *scheduling.Store, sync syncOrgFn, organizationID string) {
	pcoBackgroundSyncMu.Lock()
	if !pcoBackgroundSyncTryStart(pcoBackgroundSyncInProgress, organizationID) {
		pcoBackgroundSyncMu.Unlock()
		return
	}
	pcoBackgroundSyncMu.Unlock()
	defer func() {
		pcoBackgroundSyncMu.Lock()
		delete(pcoBackgroundSyncInProgress, organizationID)
		pcoBackgroundSyncMu.Unlock()
	}()

	session, ok, err := sessionStore.SessionForBackgroundSync(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "PCO background sync: selecting session failed", "organization_id", organizationID, "error", err)
		return
	}
	if !ok {
		// No live session at all for this org - nothing to borrow a token
		// from. No behavior change from today: this org is simply skipped,
		// same as every tick before this cron existed.
		return
	}

	result, err := refresh(ctx, session.RefreshToken)
	if err != nil {
		// This session's refresh token is no longer honored by PCO (revoked
		// access, disconnected app, or an idle refresh token PCO has
		// stopped accepting - see docs/pco-background-sync-plan.md's "Open
		// questions"). Delete it now and skip this org for this tick - a
		// strictly earlier, stronger cleanup trigger than
		// deleteExpiredSessions' 30-day TTL sweep (auth.go), since a dead
		// refresh token is a much stronger "this session is actually
		// useless" signal than merely being old.
		logger.WarnContext(ctx, "PCO background sync: refreshing access token failed, deleting session", "organization_id", organizationID, "error", err)
		if delErr := sessionStore.DeleteSessionByID(ctx, session.ID); delErr != nil {
			logger.ErrorContext(ctx, "PCO background sync: deleting session with dead refresh token failed", "organization_id", organizationID, "error", delErr)
		}
		return
	}

	if err := sessionStore.UpdateSessionTokensByID(ctx, session.ID, result.AccessToken, result.RefreshToken, result.ExpiresAt); err != nil {
		// Best-effort: the freshly refreshed token is still usable for this
		// pass even if persisting it failed - continue rather than skip
		// (a real visit using this same session later will just trigger
		// its own refresh, same as any other stale-token session).
		logger.ErrorContext(ctx, "PCO background sync: persisting refreshed token failed", "organization_id", organizationID, "error", err)
	}

	// A fresh request id for this cron pass (not a real request's) so this
	// pass's own logging (and controllers.RunOrgBackgroundSync's) can still
	// be correlated end to end - same reasoning as every other
	// context.Background()-derived background context in this codebase
	// (see logger.WithRequestID's doc comment). Priority 20: this borrowed
	// token's traffic should never delay the same token's own interactive
	// use if go-pco's throttle is enabled (PCO_THROTTLE_ENABLED) - see
	// triggerSongInfoQueueSync's identical reasoning.
	requestID := utils.UUIDv4()
	syncCtx := pco.WithPriority(pco.WithAccessToken(logger.WithRequestID(context.Background(), requestID), result.AccessToken), 20)

	sync(syncCtx, schedulingStore, organizationID)
}
