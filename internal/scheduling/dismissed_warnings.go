package scheduling

import (
	"context"
	"time"
)

// Warning-kind codes for dismissed_warnings.warning_kind - see
// docs/plans/shipped/dismissable-warnings-plan.md for the full design and
// each kind's own dismissal scope:
//
//   - WarningKindAvoidPairing dismisses permanently (organization + song-pair
//     scoped, no plan component - see the controller's avoidPairingSubjectKey).
//   - WarningKindRecentRepeat and WarningKindVocalRangeMismatch both dismiss
//     per-occurrence (subject_key folds in the plan id, so the same song on a
//     genuinely different future plan still shows loud - see the
//     controller's recentRepeatSubjectKey/vocalRangeMismatchSubjectKey).
//
// Team-tab warnings (FrequencyWarning/BlockoutWarning/CrossTeamWarning,
// rosters_candidate_warnings.go) are deliberately out of scope for this
// first pass - no warning kind constant exists for them here.
const (
	WarningKindAvoidPairing       = "avoid_pairing"
	WarningKindRecentRepeat       = "recent_repeat"
	WarningKindVocalRangeMismatch = "vocal_range_mismatch"
)

// DismissedWarning is one dismissed-warning row - see the migration
// (00049_dismissed_warnings.sql) for the full schema/reasoning.
type DismissedWarning struct {
	ID                  int64     `json:"id"`
	OrganizationID      string    `json:"organization_id"`
	WarningKind         string    `json:"warning_kind"`
	SubjectKey          string    `json:"subject_key"`
	DismissedByPersonID string    `json:"dismissed_by_person_id"`
	DismissedAt         time.Time `json:"dismissed_at"`
}

// DismissWarning records organizationID/warningKind/subjectKey as dismissed,
// upserting so re-dismissing an already-dismissed warning (e.g. a second
// person clicking Dismiss on the same badge before the page refreshes) is
// idempotent rather than erroring - it just refreshes who dismissed it and
// when.
func (s *Store) DismissWarning(ctx context.Context, organizationID, dismissedByPersonID, warningKind, subjectKey string) (DismissedWarning, error) {
	var dw DismissedWarning
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO dismissed_warnings (organization_id, warning_kind, subject_key, dismissed_by_person_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id, warning_kind, subject_key)
		DO UPDATE SET dismissed_by_person_id = excluded.dismissed_by_person_id, dismissed_at = now()
		RETURNING id, organization_id, warning_kind, subject_key, dismissed_by_person_id, dismissed_at
	`, organizationID, warningKind, subjectKey, dismissedByPersonID).Scan(
		&dw.ID, &dw.OrganizationID, &dw.WarningKind, &dw.SubjectKey, &dw.DismissedByPersonID, &dw.DismissedAt,
	)
	if err != nil {
		return DismissedWarning{}, err
	}
	return dw, nil
}

// UndismissWarning removes a dismissal - "Re-enable warning" on an already-
// dismissed badge. Org-scoped like every write here; a no-op (not an error)
// when nothing matched, same convention as DeleteSongRelationship's
// sql.ErrNoRows callers that don't actually care whether a row existed.
func (s *Store) UndismissWarning(ctx context.Context, organizationID, warningKind, subjectKey string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM dismissed_warnings
		WHERE organization_id = $1 AND warning_kind = $2 AND subject_key = $3
	`, organizationID, warningKind, subjectKey)
	return err
}

// DismissedWarningKeys returns every dismissed subject_key for
// organizationID/warningKind as a cheap lookup set - fetched once per Build
// page render (buildSetBuilderData) and checked inline via a plain map
// lookup per warning instance, the same "fetch once here, check inline"
// shape this app's other per-render lookups already use (e.g.
// personNameMapForIDs) - just against this app's own local DB, so there's
// no PCO rate limit/caching concern to weigh here.
func (s *Store) DismissedWarningKeys(ctx context.Context, organizationID, warningKind string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT subject_key FROM dismissed_warnings
		WHERE organization_id = $1 AND warning_kind = $2
	`, organizationID, warningKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys[key] = true
	}
	return keys, rows.Err()
}
