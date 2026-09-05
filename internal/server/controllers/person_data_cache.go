package controllers

// Background sync for the Team Builder's per-person PCO caches (blockouts,
// cross-team plan-people history, per-team plan-people history) - see
// rosters_warnings.go for the cache-first read path this backfills, and
// triggerSongInfoQueueSync/runSongInfoQueueSync (song_arrangement_cache.go)
// for the architecture this deliberately clones rather than reinventing: a
// queue of pending sync requests, drained by a background goroutine in
// paced batches, guarded against a duplicate concurrent pass per
// organization.
//
// There's no dedicated page/button that kicks this off (nor, since Phase 2
// of docs/pco-call-efficiency-rollout-plan.md, is there one for Song Info
// any more either) - requests are collected as a side effect of
// otherAssignmentDates/crossTeamAssignmentsThisWeek/personBlockouts finding
// a miss or a stale hit during an ordinary Team Builder render
// (rosters_warnings.go), and triggerPersonDataSync (called from each
// roster-rendering handler right after building its response data) starts
// a background pass whenever there's something pending and nothing already
// running for this org. Song Info's own trigger (triggerSongInfoQueueSync)
// differs only in *where* it's called from - once, inside the shared
// buildSetBuilderData builder function itself, rather than from each of
// this package's ~6 roster handlers the way this one is - see that
// function's own doc comment for why. Both are deliberately the "render-
// triggered" design docs/pco-roster-cold-start-plan.md settled on, not an
// unattended/nightly sync - see that plan's "unattended-cron question,
// revisited" section for why: both only ever run using a live request's
// own PCO access token (via middleware.AccessToken/pco.AccessTokenFromContext),
// so neither needs a new token-management mechanism (borrowing/refreshing
// some other session's token for unattended use) at all.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// personSyncKind distinguishes the three independent caches a
// personSyncRequest can target - see rosters_warnings.go's
// otherAssignmentDates/crossTeamAssignmentsThisWeek/personBlockouts, the
// three read paths that enqueue these.
type personSyncKind int

const (
	personSyncKindBlockouts personSyncKind = iota
	personSyncKindCrossTeamHistory
	personSyncKindTeamHistory
)

// personSyncRequest is one pending (organizationID, personID[, teamID])
// sync need, deduplicated in personSyncPending by being used as a map key
// (all fields are plain strings/an int, so it's comparable). teamID is only
// meaningful for personSyncKindTeamHistory - left "" for the other two
// kinds, which aren't team-scoped.
type personSyncRequest struct {
	organizationID string
	personID       string
	teamID         string
	kind           personSyncKind
}

// personSyncMu guards both maps below - the pending queue and the
// per-organization in-progress guard - so triggerPersonDataSync's
// check-in-progress-then-drain-pending sequence happens atomically (see its
// own comment for why that matters).
var (
	personSyncMu         sync.Mutex
	personSyncPending    = map[personSyncRequest]bool{}
	personSyncInProgress = map[string]bool{}
)

// enqueuePersonSync records that req still needs a sync pass - called from
// the cache-first read path (rosters_warnings.go) on a miss or a stale hit.
// Safe to call repeatedly for the same request before it's picked up (the
// map dedupes) and safe to call while a sync for this organization is
// already running (it'll just be picked up by the next pass).
func enqueuePersonSync(req personSyncRequest) {
	personSyncMu.Lock()
	personSyncPending[req] = true
	personSyncMu.Unlock()
}

// personDataSyncBatchSize/personDataSyncBatchPause are personSyncRequest's
// counterparts to songInfoSyncBatchSize/songInfoSyncBatchPause
// (song_arrangement_cache.go) - same reasoning, adjusted for this sync
// costing exactly one PCO call per request (a single GetBlockouts or
// GetPersonPlanPeople call - see syncOnePersonRequest), unlike Song Info's
// two calls per song. A personDataSyncBatchSize-sized batch (10 requests)
// therefore costs up to 10 calls fired together, not up to 20 - so the same
// personDataSyncBatchPause pause between batches leaves even more headroom
// under PCO's 100-requests/20-seconds-per-user-token budget than Song
// Info's own pacing does (at most 3 batch-starts land in any 20-second
// window at this pause, so at most 30 calls/20s - 30% of budget - rather
// than Song Info's already-conservative 60%).
const personDataSyncBatchSize = scheduling.PCOFanOutLimit
const personDataSyncBatchPause = 10 * time.Second

// personSyncBatches splits reqs into consecutive batches of at most size,
// preserving order - personSyncRequest's counterpart to songSyncBatches
// (song_arrangement_cache.go). Kept as its own small pure function rather
// than generalizing songSyncBatches to share one implementation, so this
// change doesn't touch that function's already-tested signature for a
// generalization it doesn't need.
func personSyncBatches(reqs []personSyncRequest, size int) [][]personSyncRequest {
	if len(reqs) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(reqs)
	}

	batches := make([][]personSyncRequest, 0, (len(reqs)+size-1)/size)
	for i := 0; i < len(reqs); i += size {
		end := i + size
		if end > len(reqs) {
			end = len(reqs)
		}
		batches = append(batches, reqs[i:end])
	}
	return batches
}

// personSyncTryStart is songSyncTryStart's counterpart for
// personSyncInProgress - see that function's doc comment (
// song_arrangement_cache.go) for why this check-and-set is split out as a
// pure function against a plain map: unit-testable without a goroutine or a
// real mutex. Callers hold personSyncMu for the duration of this call.
func personSyncTryStart(inProgress map[string]bool, organizationID string) bool {
	if inProgress[organizationID] {
		return false
	}
	inProgress[organizationID] = true
	return true
}

// triggerPersonDataSync starts a background sync pass for whatever's
// currently pending for this request's organization, if anything is and no
// pass is already running for it - called from each roster-rendering
// handler (BuildPlanPage/BuildTeamTabLazy, RunSheetPage, PublishRoster/
// RevertRoster, AddRosterAssignment/RemoveRosterAssignment/
// QuickAddPositionPreference) right after building its response data, which
// is what populates personSyncPending in the first place (see
// otherAssignmentDates/crossTeamAssignmentsThisWeek/personBlockouts in
// rosters_warnings.go).
//
// The in-progress check and the pending drain happen under one lock
// (personSyncMu) so two requests racing this call can't both see "nothing
// running yet" and both start a pass, or both drain the same pending
// entries into two separate passes.
func (sc *SchedulingController) triggerPersonDataSync(c *fiber.Ctx) {
	orgID := middleware.CurrentOrganizationID(c)
	if orgID == "" {
		return
	}

	personSyncMu.Lock()
	if !personSyncTryStart(personSyncInProgress, orgID) {
		personSyncMu.Unlock()
		return
	}
	var reqs []personSyncRequest
	for req := range personSyncPending {
		if req.organizationID == orgID {
			reqs = append(reqs, req)
			delete(personSyncPending, req)
		}
	}
	if len(reqs) == 0 {
		// Nothing actually pending for this org - release the in-progress
		// guard immediately rather than leaving it held with nothing to do,
		// which would block a genuine future request from ever starting a
		// pass.
		delete(personSyncInProgress, orgID)
		personSyncMu.Unlock()
		return
	}
	personSyncMu.Unlock()

	// accessToken, not ctx: this goroutine outlives the request (see
	// triggerSongInfoQueueSync's identical reasoning in
	// song_arrangement_cache.go) - middleware.RequestContext(c)'s context
	// is cancelled the moment this handler returns, so a fresh
	// context.Background()-derived context carrying the same token is
	// built instead - logger.WithRequestID
	// carries this request's id along the same way, so runPersonDataSync's
	// own logging can still be traced back to whichever request triggered
	// it.
	accessToken := middleware.AccessToken(c)
	requestID := middleware.CurrentRequestID(c)
	store := sc.store
	// Priority 20: an unattended background sync should never delay this
	// same token's interactive traffic if go-pco's throttle is enabled
	// (PCO_THROTTLE_ENABLED - off by default, see go-pco's README) - see
	// docs/pco-api-risks.md's throttle-adoption addendum.
	syncCtx := pco.WithPriority(pco.WithAccessToken(logger.WithRequestID(context.Background(), requestID), accessToken), 20)
	go runPersonDataSync(syncCtx, store, orgID, reqs)
}

// runPersonDataSync is triggerPersonDataSync's actual work, run in a
// background goroutine - see runSongInfoQueueSync (song_arrangement_cache.go)
// for the identical pacing rationale this mirrors: reqs is split into
// personDataSyncBatchSize batches, each fanned out concurrently via
// runPacedBatches with a personDataSyncBatchPause pause between batches,
// trading wall-clock time for staying comfortably under PCO's rate limit
// regardless of how many people/teams are pending. Same best-effort error
// handling as every other sync in this codebase: one request's failure is
// logged and skipped, never aborting the batch or the rest of the pass.
func runPersonDataSync(ctx context.Context, store *scheduling.Store, organizationID string, reqs []personSyncRequest) {
	defer func() {
		personSyncMu.Lock()
		delete(personSyncInProgress, organizationID)
		personSyncMu.Unlock()
	}()

	batches := personSyncBatches(reqs, personDataSyncBatchSize)
	var applied, failed atomic.Int64
	runPacedBatches(batches, personDataSyncBatchPause, func(req personSyncRequest) {
		if err := syncOnePersonRequest(ctx, store, req); err != nil {
			logger.ErrorContext(ctx, "background person data sync: request failed",
				"organization_id", organizationID, "person_id", req.personID,
				"team_id", req.teamID, "kind", req.kind, "error", err)
			failed.Add(1)
			return
		}
		applied.Add(1)
	})

	logger.InfoContext(ctx, "background person data sync complete",
		"organization_id", organizationID,
		"requests", len(reqs),
		"applied", applied.Load(),
		"failed", failed.Load(),
	)
}

// syncOnePersonRequest dispatches req to whichever of the three PCO calls/
// cache replacements its kind needs.
func syncOnePersonRequest(ctx context.Context, store *scheduling.Store, req personSyncRequest) error {
	switch req.kind {
	case personSyncKindBlockouts:
		return syncPersonBlockouts(ctx, store, req.organizationID, req.personID)
	case personSyncKindCrossTeamHistory:
		return syncPersonPlanHistory(ctx, store, req.organizationID, req.personID)
	case personSyncKindTeamHistory:
		return syncPersonTeamPlanHistory(ctx, store, req.organizationID, req.personID, req.teamID)
	default:
		return fmt.Errorf("unknown person sync kind %d", req.kind)
	}
}

// syncPersonBlockouts fetches personID's future blockouts (the same call
// rosters_builder.go/rosters_scoped.go used to make live on every render -
// see personBlockouts in rosters_warnings.go for the read path this feeds)
// and wholesale-replaces the local cache with the result.
func syncPersonBlockouts(ctx context.Context, store *scheduling.Store, organizationID, personID string) error {
	resp, err := pco.GetBlockouts(ctx, personID, &pco.BlockoutsParams{Filter: "future"})
	if err != nil {
		return err
	}

	blockouts := make([]scheduling.PersonBlockout, 0, len(resp.Data))
	for _, b := range resp.Data {
		blockouts = append(blockouts, scheduling.PersonBlockout{
			PCOBlockoutID:   b.ID,
			StartsAt:        b.Attributes.StartsAt,
			EndsAt:          b.Attributes.EndsAt,
			Reason:          b.Attributes.Reason,
			RepeatFrequency: b.Attributes.RepeatFrequency,
		})
	}
	return store.ReplacePersonBlockouts(ctx, organizationID, personID, blockouts)
}

// syncPersonPlanHistory fetches personID's all-teams PlanPerson history
// (feeding crossTeamAssignmentsThisWeek's cache) and wholesale-replaces the
// local cache with the result. Plan-people rows whose plan date can't be
// resolved (a malformed or missing "plan" include entry) are dropped rather
// than cached - a row with no sort_date is never usable by
// crossTeamAssignmentsThisWeek's own same-week filtering anyway, matching
// exactly what the live call this replaces used to do.
func syncPersonPlanHistory(ctx context.Context, store *scheduling.Store, organizationID, personID string) error {
	resp, err := pco.GetPersonPlanPeople(ctx, personID, &pco.PersonPlanPeopleParams{
		Include: []string{"plan", "team"},
		PerPage: 25,
	})
	if err != nil {
		return err
	}

	planDates := planSortDatesFromIncluded(resp.Included)
	teamNames := teamNamesFromIncluded(resp.Included)

	entries := make([]scheduling.PersonPlanHistoryEntry, 0, len(resp.Data))
	for _, pp := range resp.Data {
		if pp.Relationships.Plan.Data == nil {
			continue
		}
		planID := pp.Relationships.Plan.Data.ID
		date, ok := planDates[planID]
		if !ok {
			continue
		}

		teamID, teamName := "", ""
		if pp.Relationships.Team.Data != nil {
			teamID = pp.Relationships.Team.Data.ID
			teamName = teamNames[teamID]
		}

		entries = append(entries, scheduling.PersonPlanHistoryEntry{
			PCOPlanPersonID:  pp.ID,
			TeamID:           teamID,
			TeamName:         teamName,
			TeamPositionName: pp.Attributes.TeamPositionName,
			PlanID:           planID,
			SortDate:         date,
		})
	}
	return store.ReplacePersonPlanHistory(ctx, organizationID, personID, entries)
}

// syncPersonTeamPlanHistory is syncPersonPlanHistory's per-team counterpart,
// feeding otherAssignmentDates' cache - same PCO endpoint, filtered
// server-side to one team via TeamID, matching exactly what the live call
// this replaces used to do.
func syncPersonTeamPlanHistory(ctx context.Context, store *scheduling.Store, organizationID, personID, teamID string) error {
	resp, err := pco.GetPersonPlanPeople(ctx, personID, &pco.PersonPlanPeopleParams{
		TeamID:  teamID,
		Include: []string{"plan"},
		PerPage: 25,
	})
	if err != nil {
		return err
	}

	planDates := planSortDatesFromIncluded(resp.Included)

	entries := make([]scheduling.PersonTeamPlanHistoryEntry, 0, len(resp.Data))
	for _, pp := range resp.Data {
		if pp.Relationships.Plan.Data == nil {
			continue
		}
		planID := pp.Relationships.Plan.Data.ID
		date, ok := planDates[planID]
		if !ok {
			continue
		}

		entries = append(entries, scheduling.PersonTeamPlanHistoryEntry{
			PCOPlanPersonID:  pp.ID,
			TeamPositionName: pp.Attributes.TeamPositionName,
			PlanID:           planID,
			SortDate:         date,
		})
	}
	return store.ReplacePersonTeamPlanHistory(ctx, organizationID, personID, teamID, entries)
}
