package controllers

import (
	"testing"
	"time"

	"church/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
)

func neededPosition(teamID, positionName string, quantity int) pco.NeededPositionData {
	np := pco.NeededPositionData{Attributes: pco.NeededPositionAttributes{
		Quantity:         quantity,
		TeamPositionName: positionName,
	}}
	np.Relationships.Team.Data = &pco.General{Type: "Team", ID: teamID}
	return np
}

func rosterAssignment(id int64, personID, teamID, positionName string) scheduling.RosterAssignment {
	return scheduling.RosterAssignment{ID: id, PersonID: personID, TeamID: teamID, TeamPositionName: positionName}
}

func TestGroupNeededPositionsMergesQuantityAndAssignments(t *testing.T) {
	needed := []pco.NeededPositionData{
		neededPosition("t1", "Drums", 2),
		neededPosition("t1", "Vocals", 1),
	}
	assignments := []scheduling.RosterAssignment{
		rosterAssignment(1, "p1", "t1", "Drums"),
	}

	got := groupNeededPositions(needed, assignments)

	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(got), got)
	}
	if got[0].TeamPositionName != "Drums" || got[0].Quantity != 2 || len(got[0].Assignments) != 1 {
		t.Errorf("Drums group wrong: %+v", got[0])
	}
	if got[1].TeamPositionName != "Vocals" || got[1].Quantity != 1 || len(got[1].Assignments) != 0 {
		t.Errorf("Vocals group wrong: %+v", got[1])
	}
}

// TestGroupNeededPositionsKeepsAssignmentWithNoNeededEntry confirms an
// assignment for a position Planning Center never configured a "needed"
// count for still shows up (quantity 0), rather than being silently
// dropped - mirrors mergeServiceOrder's "never drop data" principle.
func TestGroupNeededPositionsKeepsAssignmentWithNoNeededEntry(t *testing.T) {
	assignments := []scheduling.RosterAssignment{
		rosterAssignment(1, "p1", "t1", "Lights"),
	}

	got := groupNeededPositions(nil, assignments)

	if len(got) != 1 || got[0].TeamPositionName != "Lights" || got[0].Quantity != 0 || len(got[0].Assignments) != 1 {
		t.Errorf("expected a 0-quantity Lights group with 1 assignment, got %+v", got)
	}
}

func TestFrequencyWarningUnavailableAlwaysWarns(t *testing.T) {
	got := frequencyWarning(pco.SchedulePreferenceUnavailable, time.Now(), nil)
	if got == "" {
		t.Error("expected a warning for Unavailable, got none")
	}
}

func TestFrequencyWarningEveryOtherWeekTooSoon(t *testing.T) {
	target := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	lastServed := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) // 1 week before

	got := frequencyWarning(pco.SchedulePreferenceEveryOtherWeek, target, []time.Time{lastServed})
	if got == "" {
		t.Error("expected a warning - only 1 week since last serving, prefers every other week")
	}
}

func TestFrequencyWarningEveryOtherWeekFineWithEnoughGap(t *testing.T) {
	target := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	lastServed := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) // 3 weeks before

	got := frequencyWarning(pco.SchedulePreferenceEveryOtherWeek, target, []time.Time{lastServed})
	if got != "" {
		t.Errorf("expected no warning with a 3-week gap for an every-other-week preference, got %q", got)
	}
}

func TestFrequencyWarningOnceAMonthAlreadyMet(t *testing.T) {
	target := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	sameMonth := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	got := frequencyWarning(pco.SchedulePreferenceOnceAMonth, target, []time.Time{sameMonth})
	if got == "" {
		t.Error("expected a warning - already served once this month, prefers once a month")
	}
}

func TestFrequencyWarningOnceAMonthDifferentMonth(t *testing.T) {
	target := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	lastMonth := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)

	got := frequencyWarning(pco.SchedulePreferenceOnceAMonth, target, []time.Time{lastMonth})
	if got != "" {
		t.Errorf("expected no warning - last serving was a different month, got %q", got)
	}
}

func TestFrequencyWarningNoOtherDatesNeverWarnsForWeeklyPresets(t *testing.T) {
	got := frequencyWarning(pco.SchedulePreferenceEveryWeek, time.Now(), nil)
	if got != "" {
		t.Errorf("expected no warning with no serving history, got %q", got)
	}
}

func TestFrequencyWarningChooseWeeksNeverWarns(t *testing.T) {
	target := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	got := frequencyWarning(pco.SchedulePreferenceChooseWeeks, target, []time.Time{target})
	if got != "" {
		t.Errorf("Choose Weeks isn't modeled and should never warn, got %q", got)
	}
}

func TestBlockoutWarningInsideWindow(t *testing.T) {
	target := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	blockouts := []pco.BlockoutData{
		{Attributes: pco.BlockoutAttributes{
			StartsAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
			EndsAt:   time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
			Reason:   "Vacation",
		}},
	}

	got := blockoutWarning(target, blockouts)
	if got == "" {
		t.Error("expected a warning - target date falls inside the blockout window")
	}
}

func TestBlockoutWarningOutsideWindow(t *testing.T) {
	target := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	blockouts := []pco.BlockoutData{
		{Attributes: pco.BlockoutAttributes{
			StartsAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
			EndsAt:   time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
		}},
	}

	got := blockoutWarning(target, blockouts)
	if got != "" {
		t.Errorf("expected no warning - target date is outside the blockout window, got %q", got)
	}
}

// TestBlockoutWarningSkipsRecurring confirms a recurring blockout
// (repeat_frequency set) is skipped rather than treated as a plain window -
// a deliberate v1 simplification, not a bug (see blockoutWarning's doc
// comment).
func TestBlockoutWarningSkipsRecurring(t *testing.T) {
	target := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	blockouts := []pco.BlockoutData{
		{Attributes: pco.BlockoutAttributes{
			StartsAt:        time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
			EndsAt:          time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
			RepeatFrequency: "weekly",
		}},
	}

	got := blockoutWarning(target, blockouts)
	if got != "" {
		t.Errorf("expected recurring blockouts to be skipped in v1, got %q", got)
	}
}

func TestPlanSortDatesFromIncludedParsesPlanEntries(t *testing.T) {
	included := []any{
		map[string]any{
			"type": "Plan",
			"id":   "p1",
			"attributes": map[string]any{
				"sort_date": "2026-09-20T00:00:00Z",
			},
		},
		map[string]any{
			"type": "Team",
			"id":   "t1",
		},
	}

	got := planSortDatesFromIncluded(included)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 plan date (the Team entry should be skipped), got %+v", got)
	}
	want := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if !got["p1"].Equal(want) {
		t.Errorf("expected p1 -> %v, got %v", want, got["p1"])
	}
}
