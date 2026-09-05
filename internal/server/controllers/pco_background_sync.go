package controllers

// The background-sync cron's per-org pass (steps 4-6 of
// docs/pco-background-sync-plan.md, Phase 3 of
// docs/pco-call-efficiency-rollout-plan.md) - "for an org nobody's actively
// rendering pages for, do the discovery + kick off the same paced sync that
// already exists." RunOrgBackgroundSync (below) is the one exported entry
// point jobs.PCOJobs calls, once per org per tick, with a ctx already
// carrying that org's refreshed, low-priority access token and a fresh
// request id (see jobs.PCOJobs.runOrgPass for steps 1-3: picking a session,
// refreshing its token, building that ctx). Everything here deliberately
// reuses existing sync mechanics unchanged - no new pacing, batching, or
// in-progress guard - it only computes *what* needs syncing for an idle org
// and feeds it into the same enqueue/trigger functions an ordinary render
// already uses.

import (
	"context"
	"sync"
	"time"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

// RunOrgBackgroundSync runs the Song Info sync (step 4), the person-data
// refresh sync (step 5), and the person-data recent-history discovery sync
// (step 6) for organizationID - see docs/pco-background-sync-plan.md's
// "Per-org pass" section for the full design. ctx must already carry a
// valid PCO access token (pco.WithAccessToken) - both
// triggerSongInfoQueueSync and triggerPersonDataSyncCtx are no-ops without
// one, matching every other best-effort PCO pattern in this codebase.
func RunOrgBackgroundSync(ctx context.Context, store *scheduling.Store, organizationID string) {
	triggerSongInfoBackgroundSync(ctx, store, organizationID)
	triggerPersonDataBackgroundSync(ctx, store, organizationID)
}

// triggerSongInfoBackgroundSync is step 4: compute which of organizationID's
// active songs have no cached arrangement info yet - the same
// scheduling.FetchActiveSongs-vs-store.SongArrangementInfo diff
// buildSongInfoData used to compute for the old Song Info page before Phase
// 2 removed it - enqueue each missing one exactly like a render's own
// cache-miss path does (enqueueSongSync), then call the existing
// triggerSongInfoQueueSync to drain and run under its existing
// songSyncInProgress guard. An already-fully-cached library (the common
// case for an org with nothing new since its last sync) enqueues nothing,
// so this call is a cheap no-op: one cached FetchActiveSongs call plus one
// local DB read, no PCO write traffic at all.
func triggerSongInfoBackgroundSync(ctx context.Context, store *scheduling.Store, organizationID string) {
	songs, err := scheduling.FetchActiveSongs(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "background sync: fetching active songs failed", "organization_id", organizationID, "error", err)
		return
	}

	cached, err := store.SongArrangementInfo(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "background sync: reading song arrangement cache failed", "organization_id", organizationID, "error", err)
		return
	}

	for _, song := range songs {
		if _, ok := cached[song.ID]; ok {
			continue
		}
		enqueueSongSync(songSyncRequest{organizationID: organizationID, songID: song.ID})
	}

	triggerSongInfoQueueSync(ctx, store, organizationID)
}

// backgroundSyncRecentPlansPerServiceType bounds step 6's "recent past
// plans" window per service type - see docs/pco-background-sync-plan.md's
// "Open questions" section on why this needs a bound at all. 5 is a
// deliberate middle ground: wide enough to likely include a
// monthly-rotation volunteer's most recent appearance (roughly a month of
// weekly services), narrow enough that even an org with several service
// types costs only a handful of extra GetPlans/GetTeamMembers calls per
// hourly tick - bounded by (this constant) x (service type count),
// independent of how many total volunteers the org has (see that doc's
// "Scope" section).
//
// Deliberately no new "discovery already ran up through plan X" marker
// table (the plan doc's own alternative) to narrow this further: this
// window is re-walked on every tick regardless of whether anything's
// actually new, but personSyncMarkerStatus's existence check (not staleness
// check) already skips every person/team this org has ever synced before,
// so the only recurring cost of re-walking is the GetPlans/GetTeamMembers
// calls themselves, not reprocessing already-known people. That's a small,
// constant, already-bounded PCO cost, not one that grows with org size or
// history length - accepted as simpler than adding schema for a case this
// plan's own author flagged as "not urgent, revisit only if it proves to
// matter" (see that doc's opening section). Revisit this constant or add a
// marker table if the call volume this produces ever actually shows up as
// a problem.
const backgroundSyncRecentPlansPerServiceType = 5

// backgroundSyncDiscoveryInterval bounds how often step 6's recent-history
// discovery (discoverRecentPersonTeamPairs) actually runs its
// GetPlans/GetTeamMembers walk, independent of the hourly tick everything
// else in this file runs on. Unlike Song Info/person-data refresh (both
// genuinely time-sensitive staleness, and already self-limiting via the
// existing paced-batch sync they trigger), discovery's job is finding
// people/teams this org has *never* seen before - a condition that doesn't
// change hour to hour for a church whose plans update roughly weekly.
// Running the walk every hour regardless would just be 24 near-identical
// PCO calls for every one that could actually find something new. Daily is
// a deliberate middle ground: still far more often than any real team's
// membership changes, cheap enough that under-shooting this constant costs
// nothing but a slightly later discovery.
const backgroundSyncDiscoveryInterval = 24 * time.Hour

// discoveryLastRunMu/discoveryLastRunAt track this process's last
// discovery-walk attempt per organization, purely in-memory (a restart
// simply means the next tick after startup runs discovery once more than
// strictly necessary - the existing PersonSyncMarkerStatus check already
// makes a redundant walk a cheap no-op for anyone already known, so this
// never needs to survive a restart). A dedicated map/mutex rather than
// reusing pcoBackgroundSyncMu/pcoBackgroundSyncInProgress above -
// deliberately: that guard's job is "don't overlap this org's own pass,"
// this one's job is "don't bother re-walking too often," and conflating
// the two would make either concern harder to reason about on its own.
var (
	discoveryLastRunMu sync.Mutex
	discoveryLastRunAt = map[string]time.Time{}
)

// discoveryDue is the pure check-and-set at the core of the gate above -
// split out so it's unit-testable against a plain map and a fake clock,
// same reasoning as pcoBackgroundSyncTryStart (jobs/pco.go). Returns true
// (and records now as the new last-run time) the first time it's ever
// called for organizationID, or once interval has elapsed since the
// previous true - false otherwise, leaving the recorded time untouched.
func discoveryDue(lastRunAt map[string]time.Time, organizationID string, now time.Time, interval time.Duration) bool {
	if last, ok := lastRunAt[organizationID]; ok && now.Sub(last) < interval {
		return false
	}
	lastRunAt[organizationID] = now
	return true
}

// triggerPersonDataBackgroundSync is steps 5-6: refresh every stale
// person/team sync-marker row for organizationID (step 5,
// scheduling.Store.StalePersonSyncTargets), discover anyone who served on a
// bounded window of this org's recent past plans but has no sync-marker row
// at all yet (step 6, discoverRecentPersonTeamPairs below - gated to
// backgroundSyncDiscoveryInterval via discoveryDue, not run every tick),
// then trigger the existing drain-and-run pass once for whatever either
// step enqueued - mirrors triggerPersonDataSync's own render-triggered call
// exactly, just fed by this org's own idle-session pass instead of a live
// request.
func triggerPersonDataBackgroundSync(ctx context.Context, store *scheduling.Store, organizationID string) {
	stale, err := store.StalePersonSyncTargets(ctx, organizationID, personCacheTTL)
	if err != nil {
		logger.ErrorContext(ctx, "background sync: querying stale person sync targets failed", "organization_id", organizationID, "error", err)
	}
	for _, target := range stale {
		enqueuePersonSync(personSyncRequest{
			organizationID: organizationID,
			personID:       target.PersonID,
			teamID:         target.TeamID,
			kind:           personSyncKindFromStale(target.Kind),
		})
	}

	discoveryLastRunMu.Lock()
	due := discoveryDue(discoveryLastRunAt, organizationID, time.Now(), backgroundSyncDiscoveryInterval)
	discoveryLastRunMu.Unlock()
	if due {
		discoverRecentPersonTeamPairs(ctx, store, organizationID)
	}

	triggerPersonDataSyncCtx(ctx, store, organizationID)
}

// personSyncKindFromStale converts scheduling.StalePersonSyncKind (that
// package's own copy of this enum - see its doc comment for why it can't
// just import this package's personSyncKind) into this package's
// personSyncKind via an explicit switch rather than a raw int conversion,
// so neither enum's iota order can silently drift out of sync with the
// other without this switch's default case catching it.
func personSyncKindFromStale(kind scheduling.StalePersonSyncKind) personSyncKind {
	switch kind {
	case scheduling.StalePersonSyncKindBlockouts:
		return personSyncKindBlockouts
	case scheduling.StalePersonSyncKindCrossTeamHistory:
		return personSyncKindCrossTeamHistory
	case scheduling.StalePersonSyncKindTeamHistory:
		return personSyncKindTeamHistory
	default:
		// Shouldn't happen - scheduling.StalePersonSyncTargets only ever
		// produces the three kinds above. Blockouts is the safest fallback
		// (a person-only kind, same as the two most common cases) rather
		// than silently dropping the target.
		return personSyncKindBlockouts
	}
}

// discoverRecentPersonTeamPairs is step 6: walk a bounded window of
// organizationID's recent past plans, per service type
// (backgroundSyncRecentPlansPerServiceType), and for every (person, team)
// pair GetTeamMembers returns with no sync-marker row at all yet (checked
// via scheduling.Store.PersonSyncMarkerStatus - a person/team with any
// marker, fresh or stale, is already covered by the refresh sweep above and
// never re-enqueued here), enqueue the matching personSyncRequest(s) the
// same way the refresh sweep does. This is the "recent history of people"
// seed for a just-connected org or one whose team membership has shifted -
// see docs/pco-background-sync-plan.md's "Scope" section for why this is
// bounded by (recent plan count) x (service type count), not total
// volunteer count, and therefore safe independent of org size.
func discoverRecentPersonTeamPairs(ctx context.Context, store *scheduling.Store, organizationID string) {
	serviceTypes, err := scheduling.FetchServiceTypes(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "background sync: fetching service types failed", "organization_id", organizationID, "error", err)
		return
	}

	for _, serviceType := range serviceTypes {
		plansResp, err := pco.GetPlans(ctx, serviceType.ID, &pco.PlansParams{
			Filter:  "past",
			OrderBy: "-sort_date",
			PerPage: backgroundSyncRecentPlansPerServiceType,
		})
		if err != nil {
			logger.ErrorContext(ctx, "background sync: fetching recent past plans failed", "organization_id", organizationID, "service_type_id", serviceType.ID, "error", err)
			continue
		}

		for _, plan := range plansResp.Data {
			membersResp, err := pco.GetTeamMembers(ctx, serviceType.ID, plan.ID, nil)
			if err != nil {
				logger.ErrorContext(ctx, "background sync: fetching team members failed", "organization_id", organizationID, "service_type_id", serviceType.ID, "plan_id", plan.ID, "error", err)
				continue
			}

			for _, member := range membersResp.Data {
				enqueueDiscoveredPersonTeamPair(ctx, store, organizationID, member)
			}
		}
	}
}

// enqueueDiscoveredPersonTeamPair is discoverRecentPersonTeamPairs' per-row
// work, split out so its own error handling (best-effort, one bad lookup
// never aborts the rest of the walk) reads clearly against the triple loop
// above.
func enqueueDiscoveredPersonTeamPair(ctx context.Context, store *scheduling.Store, organizationID string, member pco.PlanPersonData) {
	if member.Relationships.Person.Data == nil {
		return
	}
	personID := member.Relationships.Person.Data.ID

	teamID := ""
	if member.Relationships.Team.Data != nil {
		teamID = member.Relationships.Team.Data.ID
	}

	personKnown, teamKnown, err := store.PersonSyncMarkerStatus(ctx, organizationID, personID, teamID)
	if err != nil {
		logger.ErrorContext(ctx, "background sync: checking person sync marker status failed", "organization_id", organizationID, "person_id", personID, "error", err)
		return
	}

	if !personKnown {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindBlockouts})
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindCrossTeamHistory})
	}
	if teamID != "" && !teamKnown {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, teamID: teamID, kind: personSyncKindTeamHistory})
	}
}
