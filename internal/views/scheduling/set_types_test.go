package scheduling

import "testing"

func TestCandidateSongViewHasExtraMetaEmptyIsFalse(t *testing.T) {
	cand := CandidateSongView{SongID: "1", Title: "Nothing Cached"}
	if cand.HasExtraMeta() {
		t.Error("expected an all-empty candidate to report no extra meta")
	}
}

// TestCandidateSongViewHasExtraMetaIgnoresBrowseRowFields confirms the
// three fields songBrowseMeta already shows inline (set_builder.templ)
// don't by themselves make HasExtraMeta true - CandidateDetails' "nothing
// else on record" message depends on this staying false when that's all a
// candidate has.
func TestCandidateSongViewHasExtraMetaIgnoresBrowseRowFields(t *testing.T) {
	cand := CandidateSongView{
		NotSungInLabel: "Not sung recently",
		Roles:          []SongRoleView{{Code: "opener", Label: "Opener"}},
		Tags:           []SongTagView{{Category: "Theme", Value: "Hope"}},
	}
	if cand.HasExtraMeta() {
		t.Error("expected NotSungInLabel/Roles/Tags alone not to count as extra meta")
	}
}

func TestCandidateSongViewHasExtraMetaEachFieldTrue(t *testing.T) {
	cases := []struct {
		name string
		cand CandidateSongView
	}{
		{"new song label", CandidateSongView{NewSongLabel: "Sung once"}},
		{"arrangement info", CandidateSongView{ArrangementInfo: "Key of G, 4:32"}},
		{"contexts", CandidateSongView{Contexts: []SongContextView{{ID: 1, Label: "Big church"}}}},
		{"conflicts", CandidateSongView{Conflicts: []ConflictBadgeView{{Title: "Other Song", SubjectKey: "a:b"}}}},
		{"recently repeated", CandidateSongView{RecentlyRepeated: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.cand.HasExtraMeta() {
				t.Errorf("expected HasExtraMeta true when only %s is set", tc.name)
			}
		})
	}
}
