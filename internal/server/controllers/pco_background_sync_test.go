package controllers

import (
	"testing"

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
