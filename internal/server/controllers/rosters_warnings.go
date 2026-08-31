package controllers

// Pure, testable roster warning logic (frequency/blockout/cross-team) plus
// the thin PCO-calling wrappers that feed it real assignment history.

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

// otherAssignmentDates fetches personID's own serving dates for the exact
// (teamID, teamPositionName) slot, excluding excludePlanID (the plan
// currently being built, if they're already on it) - the thin PCO-calling
// wrapper frequencyWarning's pure check runs against. PCO's plan_people
// endpoint filters by team_id server-side but not by position name, so the
// position match happens here client-side.
func otherAssignmentDates(ctx context.Context, personID, teamID, teamPositionName, excludePlanID string) []time.Time {
	history, err := pco.GetPersonPlanPeople(ctx, personID, &pco.PersonPlanPeopleParams{
		TeamID:  teamID,
		Include: []string{"plan"},
		PerPage: 25,
	})
	if err != nil {
		return nil
	}

	planDates := planSortDatesFromIncluded(history.Included)

	var dates []time.Time
	for _, pp := range history.Data {
		if pp.Attributes.TeamPositionName != teamPositionName {
			continue
		}
		if pp.Relationships.Plan.Data == nil {
			continue
		}
		planID := pp.Relationships.Plan.Data.ID
		if planID == excludePlanID {
			continue
		}
		if d, ok := planDates[planID]; ok {
			dates = append(dates, d)
		}
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
func crossTeamAssignmentsThisWeek(ctx context.Context, personID string, targetDate time.Time, excludePlanID string) []string {
	history, err := pco.GetPersonPlanPeople(ctx, personID, &pco.PersonPlanPeopleParams{
		Include: []string{"plan", "team"},
		PerPage: 25,
	})
	if err != nil {
		return nil
	}

	planDates := planSortDatesFromIncluded(history.Included)
	teamNames := teamNamesFromIncluded(history.Included)

	var labels []string
	for _, pp := range history.Data {
		if pp.Relationships.Plan.Data == nil {
			continue
		}
		planID := pp.Relationships.Plan.Data.ID
		if planID == excludePlanID {
			continue
		}
		date, ok := planDates[planID]
		if !ok || !sameWeek(date, targetDate) {
			continue
		}

		label := pp.Attributes.TeamPositionName
		if pp.Relationships.Team.Data != nil {
			if name, ok := teamNames[pp.Relationships.Team.Data.ID]; ok && name != "" {
				label = name
			}
		}
		if label == "" {
			label = "another position"
		}

		labels = append(labels, fmt.Sprintf("%s (%s)", label, weekdayAMPM(date)))
	}
	return labels
}

// crossTeamWarning turns crossTeamAssignmentsThisWeek's labels into a
// display string, mirroring frequencyWarning/blockoutWarning's shape - a
// non-blocking "protect the team" nudge (see ideas.md's "Cross-team
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
