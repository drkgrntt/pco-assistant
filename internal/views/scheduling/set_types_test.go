package scheduling

import "testing"

func TestCandidateSongViewHasMetaEmptyIsFalse(t *testing.T) {
	cand := CandidateSongView{SongID: "1", Title: "Nothing Cached"}
	if cand.HasMeta() {
		t.Error("expected an all-empty candidate to report no meta")
	}
}

func TestCandidateSongViewHasMetaEachFieldTrue(t *testing.T) {
	cases := []struct {
		name string
		cand CandidateSongView
	}{
		{"new song label", CandidateSongView{NewSongLabel: "Sung once"}},
		{"arrangement info", CandidateSongView{ArrangementInfo: "Key of G, 4:32"}},
		{"roles", CandidateSongView{Roles: []SongRoleView{{Code: "opener", Label: "Opener"}}}},
		{"tags", CandidateSongView{Tags: []SongTagView{{Category: "Theme", Value: "Hope"}}}},
		{"conflict titles", CandidateSongView{ConflictTitles: []string{"Other Song"}}},
		{"recently repeated", CandidateSongView{RecentlyRepeated: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.cand.HasMeta() {
				t.Errorf("expected HasMeta true when only %s is set", tc.name)
			}
		})
	}
}
