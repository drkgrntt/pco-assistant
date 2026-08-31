package controllers

import (
	"testing"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

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

func TestCrossTeamWarningEmptyWhenNoOtherCommitments(t *testing.T) {
	got := crossTeamWarning(nil)
	if got != "" {
		t.Errorf("expected no warning with no other commitments, got %q", got)
	}
}

func TestCrossTeamWarningListsOtherCommitments(t *testing.T) {
	got := crossTeamWarning([]string{"Photography (Sun AM)", "Greeting (Sun AM)"})
	want := "already serving 2 other position(s) this week: Photography (Sun AM), Greeting (Sun AM)"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCrossTeamWarningSingleCommitment(t *testing.T) {
	got := crossTeamWarning([]string{"Photography (Sun AM)"})
	want := "already serving 1 other position(s) this week: Photography (Sun AM)"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestSameWeekTrueWithinMondayToSunday(t *testing.T) {
	monday := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	if !sameWeek(monday, sunday) {
		t.Error("expected Monday and the following Sunday to be in the same calendar week")
	}
}

func TestSameWeekFalseAcrossWeekBoundary(t *testing.T) {
	sunday := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	nextMonday := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	if sameWeek(sunday, nextMonday) {
		t.Error("expected Sunday and the next day (Monday, a new calendar week) to differ")
	}
}

func TestWeekdayAMPM(t *testing.T) {
	morning := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	if got := weekdayAMPM(morning); got != "Sun AM" {
		t.Errorf("expected %q, got %q", "Sun AM", got)
	}
	if got := weekdayAMPM(evening); got != "Sun PM" {
		t.Errorf("expected %q, got %q", "Sun PM", got)
	}
}
