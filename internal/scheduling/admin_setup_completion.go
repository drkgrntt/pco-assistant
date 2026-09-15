package scheduling

import "context"

// SetupCompletion mirrors HomeController.buildOnboardingChecklist's
// trackable items (internal/server/controllers/home.go) - whether an org
// has *any* local data in each of the setup categories the home page's
// own onboarding checklist tracks, i.e. whether they've actually used the
// setup pages, not just signed in. Position Preferences is excluded, same
// as that checklist, since it lives in PCO itself, not this app's
// database - there's nothing local to check.
type SetupCompletion struct {
	SongRelationships, SongRoles, SongContexts, SongTags, SongVocalRanges bool
	SongPersonPreferences, PersonRelationships, PersonVocalRanges         bool
}

// SetupTotal is len(SetupCompletion)'s trackable fields - the denominator
// for a "5 of 8" summary. A plain constant, not reflection, since the
// field list only changes when a new setup category is added, which
// already means touching this file's SQL by hand anyway.
const SetupTotal = 8

// Done counts how many of the 8 categories have any data.
func (c SetupCompletion) Done() int {
	n := 0
	for _, v := range [SetupTotal]bool{
		c.SongRelationships, c.SongRoles, c.SongContexts, c.SongTags, c.SongVocalRanges,
		c.SongPersonPreferences, c.PersonRelationships, c.PersonVocalRanges,
	} {
		if v {
			n++
		}
	}
	return n
}

// SetupCompletionByOrganization reports, for every organization with at
// least one setup category touched, which of the 8 categories have any
// data - the admin app's (cmd/admin) read path for "how much are they
// actually using the setup pages," not just "are they signed in" (see
// auth.Store.OrgActivityByOrganization for that half). Deliberately
// unscoped - every other scheduling.Store method takes an organization id
// and filters on it (see CLAUDE.md's Organization scoping section); this
// is the one cross-org exception, same shape/reasoning as
// feedback.Store.List and auth.Store.ListOrganizations/ListPeople: this
// app has no "app operator" access-control concept to safely gate a
// cross-org view with on the main multi-tenant app itself, so this exists
// purely for the separate, shared-secret-gated admin binary. See
// docs/plans/in-progress/admin-tooling-plan.md.
//
// One UNION query rather than 8 separate ones - each branch is already a
// cheap indexed lookup (same tables HasAnyX's per-org EXISTS checks use),
// and UNION's own dedup gives exactly one (organization_id, category) row
// per org per touched category for free, no GROUP BY needed.
func (s *Store) SetupCompletionByOrganization(ctx context.Context) (map[string]SetupCompletion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT organization_id, 'song_relationships' FROM song_relationships
		UNION
		SELECT organization_id, 'song_roles' FROM song_roles
		UNION
		SELECT organization_id, 'song_contexts' FROM song_contexts
		UNION
		SELECT organization_id, 'song_tags' FROM song_tags
		UNION
		SELECT organization_id, 'song_vocal_ranges' FROM song_vocal_ranges
		UNION
		SELECT organization_id, 'song_person_preferences' FROM song_person_preferences
		UNION
		SELECT organization_id, 'person_relationships' FROM person_relationships
		UNION
		SELECT organization_id, 'person_vocal_ranges' FROM person_vocal_ranges
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]SetupCompletion{}
	for rows.Next() {
		var orgID, category string
		if err := rows.Scan(&orgID, &category); err != nil {
			return nil, err
		}
		c := out[orgID]
		switch category {
		case "song_relationships":
			c.SongRelationships = true
		case "song_roles":
			c.SongRoles = true
		case "song_contexts":
			c.SongContexts = true
		case "song_tags":
			c.SongTags = true
		case "song_vocal_ranges":
			c.SongVocalRanges = true
		case "song_person_preferences":
			c.SongPersonPreferences = true
		case "person_relationships":
			c.PersonRelationships = true
		case "person_vocal_ranges":
			c.PersonVocalRanges = true
		}
		out[orgID] = c
	}
	return out, rows.Err()
}
