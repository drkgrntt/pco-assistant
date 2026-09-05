package controllers

// Pure, testable roster warning logic (frequency/blockout/cross-team) plus
// the cache-first read wrappers that feed it real assignment history - see
// person_data_cache.go for the background sync that actually fills that
// cache from Planning Center.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// everyXWeeksFor maps an "every X weeks" schedule_preference value to its X.
// "Once a quarter" is treated as a rough ~13 weeks, since PCO doesn't offer
// a dedicated per-month bucket for it.
func everyXWeeksFor(schedulePreference string) (weeks int, ok bool) {
	switch schedulePreference {
	case pco.SchedulePreferenceEveryWeek:
		return 1, true
	case pco.SchedulePreferenceEveryOtherWeek:
		return 2, true
	case pco.SchedulePreferenceEvery3rdWeek:
		return 3, true
	case pco.SchedulePreferenceEvery4thWeek:
		return 4, true
	case pco.SchedulePreferenceEvery5thWeek:
		return 5, true
	case pco.SchedulePreferenceEvery6thWeek:
		return 6, true
	case pco.SchedulePreferenceOnceAQuarter:
		return 13, true
	}
	return 0, false
}

// perMonthFor maps an "X times a month" schedule_preference value to its X.
func perMonthFor(schedulePreference string) (times int, ok bool) {
	switch schedulePreference {
	case pco.SchedulePreferenceOnceAMonth:
		return 1, true
	case pco.SchedulePreferenceTwiceAMonth:
		return 2, true
	case pco.SchedulePreferenceThreeTimesAMonth:
		return 3, true
	}
	return 0, false
}

// frequencyWarning checks a person's serving-frequency preference
// (PersonTeamPositionAssignment.schedule_preference) against their other
// known assignment dates for the same position, relative to targetDate (the
// plan being built).
//
//   - "Unavailable" always warns, regardless of other dates.
//   - The "every X weeks" presets warn when the *nearest* other assignment
//     (whichever side of targetDate it falls on) is closer than X weeks -
//     serving again too soon either before or after this plan both violate
//     the same preference.
//   - The "X times a month" presets warn when targetDate's calendar month
//     already has >= X other assignments (not counting targetDate itself,
//     which isn't in otherDates - it's the plan being considered, not yet a
//     confirmed serving date).
//   - "Choose Weeks" has no numeric rule to check against and never warns -
//     this app doesn't model PCO's custom per-week list (see
//     PersonTeamPositionAssignmentAttributes.PreferredWeeks in go-pco).
func frequencyWarning(schedulePreference string, targetDate time.Time, otherDates []time.Time) string {
	if schedulePreference == pco.SchedulePreferenceUnavailable {
		return "marked Unavailable for this position"
	}

	if weeks, ok := everyXWeeksFor(schedulePreference); ok {
		if len(otherDates) == 0 {
			return ""
		}
		nearestGap := math.Abs(targetDate.Sub(otherDates[0]).Hours())
		for _, d := range otherDates[1:] {
			if gap := math.Abs(targetDate.Sub(d).Hours()); gap < nearestGap {
				nearestGap = gap
			}
		}
		gapWeeks := nearestGap / (7 * 24)
		if gapWeeks < float64(weeks) {
			return fmt.Sprintf("prefers %q - another serving is only about %.0f week(s) away", schedulePreference, gapWeeks)
		}
		return ""
	}

	if times, ok := perMonthFor(schedulePreference); ok {
		count := 0
		for _, d := range otherDates {
			if d.Year() == targetDate.Year() && d.Month() == targetDate.Month() {
				count++
			}
		}
		if count >= times {
			return fmt.Sprintf("prefers %q - already serving %d time(s) this month", schedulePreference, count)
		}
		return ""
	}

	return ""
}

// blockoutWarning flags when targetDate falls inside one of the person's
// blockout windows. Scoped to non-recurring windows for v1 - a blockout
// with a repeat_frequency set is skipped rather than expanded into its
// recurring dates, a deliberate simplification (see BlockoutAttributes' doc
// comment in go-pco), not a silent bug.
func blockoutWarning(targetDate time.Time, blockouts []pco.BlockoutData) string {
	for _, b := range blockouts {
		if b.Attributes.RepeatFrequency != "" {
			continue
		}
		if targetDate.Before(b.Attributes.StartsAt) || targetDate.After(b.Attributes.EndsAt) {
			continue
		}
		if b.Attributes.Reason == "" {
			return "marked unavailable in Planning Center for this date"
		}
		return fmt.Sprintf("marked unavailable in Planning Center for this date (%s)", b.Attributes.Reason)
	}
	return ""
}

// personCacheTTL bounds how long a cached blockout/plan-history row is
// served before a render also queues a background refresh for it (see
// personCacheStale) - a few hours, not FetchActiveSongs'/navStateTTL's 60s:
// blockouts and serving history don't change minute-to-minute the way "is
// this song still active" does, so a short TTL would barely improve the
// cold-render case (see docs/plans/shipped/pco-roster-cold-start-plan.md's "Caching"
// section - this is the whole reason this cache is a persisted table
// with its own TTL rather than an in-process map). Chosen as a round,
// safely-long default; tune once real
// usage shows how often blockouts/commitments actually change.
const personCacheTTL = 4 * time.Hour

// personCacheStale reports whether syncedAt is old enough that a render
// should also queue a background refresh (see enqueuePersonSync) while
// still serving the row it has immediately - never blocks on the refresh.
func personCacheStale(syncedAt time.Time) bool {
	return time.Since(syncedAt) > personCacheTTL
}

// otherAssignmentDates returns personID's own cached serving dates for the
// exact (teamID, teamPositionName) slot, excluding excludePlanID (the plan
// currently being built, if they're already on it) - the thin cache-read
// wrapper frequencyWarning's pure check runs against.
//
// Pure read of the local person_team_plan_history cache (see
// scheduling.Store.PersonTeamPlanHistoryCache) - no PCO call happens here,
// cache hit or not (see personBlockouts' doc comment below for why). On a
// miss (no sync-marker row yet for this person/team) this returns nil and
// queues a background sync (see enqueuePersonSync/triggerPersonDataSync in
// person_data_cache.go); on a stale hit it serves the cached rows
// immediately and also queues a refresh. The cached rows span every
// position on teamID (PCO's plan_people endpoint filters by team_id
// server-side but not by position name, same as the live call this
// replaced), so the position match still happens here client-side.
func (sc *SchedulingController) otherAssignmentDates(ctx context.Context, organizationID, personID, teamID, teamPositionName, excludePlanID string) []time.Time {
	entries, syncedAt, ok, err := sc.store.PersonTeamPlanHistoryCache(ctx, organizationID, personID, teamID)
	if err != nil || !ok {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, teamID: teamID, kind: personSyncKindTeamHistory})
		return nil
	}
	if personCacheStale(syncedAt) {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, teamID: teamID, kind: personSyncKindTeamHistory})
	}

	var dates []time.Time
	for _, e := range entries {
		if e.TeamPositionName != teamPositionName {
			continue
		}
		if e.PlanID == excludePlanID {
			continue
		}
		dates = append(dates, e.SortDate)
	}
	return dates
}

// startOfWeek returns the Monday 00:00 (in t's own location) that begins
// t's Mon-Sun calendar week - the boundary crossTeamAssignmentsThisWeek uses
// to decide "same week as the plan being built."
func startOfWeek(t time.Time) time.Time {
	daysFromMonday := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).AddDate(0, 0, -daysFromMonday)
}

// sameWeek reports whether a and b fall in the same Mon-Sun calendar week.
func sameWeek(a, b time.Time) bool {
	return startOfWeek(a).Equal(startOfWeek(b))
}

// weekdayAMPM renders a plan's sort_date as a compact "Sun AM"/"Sun PM"
// label - enough to tell two same-week commitments apart without a full
// timestamp.
func weekdayAMPM(t time.Time) string {
	half := "AM"
	if t.Hour() >= 12 {
		half = "PM"
	}
	return fmt.Sprintf("%s %s", t.Format("Mon"), half)
}

// crossTeamAssignmentsThisWeek looks up personID's *other* serving
// commitments - across every team and position, not just the one
// (teamID, teamPositionName) slot otherAssignmentDates is scoped to - that
// fall in the same Mon-Sun calendar week as targetDate (the plan being
// built). excludePlanID (the plan currently being built) is left out: any
// assignment already on that exact plan is either this same position being
// considered (nothing new to report) or visible directly elsewhere on this
// same roster page, so it isn't a "hidden" commitment worth surfacing here.
// Returns one human-readable label per other commitment found (e.g.
// "Photography (Sun AM)"), preferring the resolved team name and falling
// back to the position name alone when the team can't be resolved.
//
// Pure read of the local person_plan_history cache (see
// scheduling.Store.PersonPlanHistoryCache) - see otherAssignmentDates' doc
// comment for the shared miss/stale-hit/never-block-on-PCO shape this
// follows exactly, just against the all-teams cache instead of the
// per-team one.
func (sc *SchedulingController) crossTeamAssignmentsThisWeek(ctx context.Context, organizationID, personID string, targetDate time.Time, excludePlanID string) []string {
	entries, syncedAt, ok, err := sc.store.PersonPlanHistoryCache(ctx, organizationID, personID)
	if err != nil || !ok {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindCrossTeamHistory})
		return nil
	}
	if personCacheStale(syncedAt) {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindCrossTeamHistory})
	}

	var labels []string
	for _, e := range entries {
		if e.PlanID == excludePlanID {
			continue
		}
		if !sameWeek(e.SortDate, targetDate) {
			continue
		}

		label := e.TeamPositionName
		if e.TeamName != "" {
			label = e.TeamName
		}
		if label == "" {
			label = "another position"
		}

		labels = append(labels, fmt.Sprintf("%s (%s)", label, weekdayAMPM(e.SortDate)))
	}
	return labels
}

// crossTeamWarning turns crossTeamAssignmentsThisWeek's labels into a
// display string, mirroring frequencyWarning/blockoutWarning's shape - a
// non-blocking "protect the team" nudge (see docs/ideas.md's "Cross-team
// burnout") for someone within preference on any single position who's
// nonetheless serving several positions/teams the same week.
func crossTeamWarning(otherCommitments []string) string {
	if len(otherCommitments) == 0 {
		return ""
	}
	return fmt.Sprintf("already serving %d other position(s) this week: %s", len(otherCommitments), strings.Join(otherCommitments, ", "))
}

// blockoutWarningIfKnown is blockoutWarning guarded for a not-yet-resolved
// plan date (e.g. the GetPlan call failed) - no date means nothing to check
// against, not a false "unavailable."
func blockoutWarningIfKnown(planDate time.Time, blockouts []pco.BlockoutData) string {
	if planDate.IsZero() {
		return ""
	}
	return blockoutWarning(planDate, blockouts)
}

// personBlockouts returns personID's cached future blockouts, in the same
// []pco.BlockoutData shape the old live pco.GetBlockouts call
// (rosters_builder.go/rosters_scoped.go) returned - so blockoutWarningIfKnown
// and every existing caller keep working unchanged.
//
// Pure read of the local person_blockouts cache (see
// scheduling.Store.PersonBlockoutCache) - no PCO call happens in this
// (or any) render path anymore, cache hit or not: a miss just means
// nothing to warn about *this render*, not a blocked page. On a miss (no
// sync-marker row yet) this returns nil and queues a background sync (see
// enqueuePersonSync/triggerPersonDataSync in person_data_cache.go); on a
// stale hit it serves the cached rows immediately and also queues a
// refresh - never blocks on PCO either way. See otherAssignmentDates'/
// crossTeamAssignmentsThisWeek's doc comments for the identical shape.
func (sc *SchedulingController) personBlockouts(ctx context.Context, organizationID, personID string) []pco.BlockoutData {
	blockouts, syncedAt, ok, err := sc.store.PersonBlockoutCache(ctx, organizationID, personID)
	if err != nil || !ok {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindBlockouts})
		return nil
	}
	if personCacheStale(syncedAt) {
		enqueuePersonSync(personSyncRequest{organizationID: organizationID, personID: personID, kind: personSyncKindBlockouts})
	}

	out := make([]pco.BlockoutData, 0, len(blockouts))
	for _, b := range blockouts {
		out = append(out, pco.BlockoutData{
			ID: b.PCOBlockoutID,
			Attributes: pco.BlockoutAttributes{
				Reason:          b.Reason,
				StartsAt:        b.StartsAt,
				EndsAt:          b.EndsAt,
				RepeatFrequency: b.RepeatFrequency,
			},
		})
	}
	return out
}
