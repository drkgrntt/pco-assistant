package controllers

import (
	"testing"
	"time"

	"pco-assistant/internal/scheduling"
)

func TestPersonSyncKindFromStaleMapsAllThreeKinds(t *testing.T) {
	cases := []struct {
		in   scheduling.StalePersonSyncKind
		want personSyncKind
	}{
		{scheduling.StalePersonSyncKindBlockouts, personSyncKindBlockouts},
		{scheduling.StalePersonSyncKindCrossTeamHistory, personSyncKindCrossTeamHistory},
		{scheduling.StalePersonSyncKindTeamHistory, personSyncKindTeamHistory},
	}

	for _, c := range cases {
		if got := personSyncKindFromStale(c.in); got != c.want {
			t.Errorf("personSyncKindFromStale(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDiscoveryDue(t *testing.T) {
	const orgA, orgB = "org-a", "org-b"
	interval := 24 * time.Hour
	base := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	lastRunAt := map[string]time.Time{}

	if !discoveryDue(lastRunAt, orgA, base, interval) {
		t.Fatal("first call for a never-seen org should be due")
	}
	if got := lastRunAt[orgA]; !got.Equal(base) {
		t.Errorf("discoveryDue should record the run time, got %v want %v", got, base)
	}

	if discoveryDue(lastRunAt, orgA, base.Add(time.Hour), interval) {
		t.Error("a call one hour later, well inside the interval, should not be due")
	}
	if discoveryDue(lastRunAt, orgA, base.Add(interval-time.Minute), interval) {
		t.Error("a call one minute short of the interval should not be due")
	}

	dueAt := base.Add(interval)
	if !discoveryDue(lastRunAt, orgA, dueAt, interval) {
		t.Error("a call exactly at the interval boundary should be due")
	}
	if got := lastRunAt[orgA]; !got.Equal(dueAt) {
		t.Errorf("discoveryDue should advance the recorded run time on a due call, got %v want %v", got, dueAt)
	}

	// A second organization's clock is independent of the first's.
	if !discoveryDue(lastRunAt, orgB, base.Add(time.Minute), interval) {
		t.Error("a different organization's first call should be due regardless of orgA's state")
	}
	if discoveryDue(lastRunAt, orgB, base.Add(2*time.Minute), interval) {
		t.Error("orgB's second call, seconds later, should not be due")
	}
}
