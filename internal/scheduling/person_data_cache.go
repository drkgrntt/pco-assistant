package scheduling

// Local Postgres caches for three per-person Planning Center reads the Team
// Builder page used to make live, once per relevant person, on every render
// (see rosters_warnings.go/rosters_builder.go in internal/server/
// controllers, and pco-api-risks.md's addendum on this change): blockouts,
// all-teams plan-people history (crossTeamAssignmentsThisWeek), and
// per-team plan-people history (otherAssignmentDates). Same "sync-marker
// row plus zero-or-more detail rows" shape as song_arrangement_cache
// (song_arrangement_cache.go/migration 00023), just without that table's
// "a row's mere existence means checked" shortcut - list-valued data can't
// reuse that trick, since zero blockouts and never-synced both look like
// zero rows. See migration 00033 for the schema these methods read/write.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PersonBlockout is one cached Planning Center blockout window - see
// pco.BlockoutData, which this mirrors just the fields blockoutWarning
// actually needs from.
type PersonBlockout struct {
	PCOBlockoutID   string
	StartsAt        time.Time
	EndsAt          time.Time
	Reason          string
	RepeatFrequency string
}

// PersonPlanHistoryEntry is one cached all-teams PlanPerson row, backing
// crossTeamAssignmentsThisWeek. TeamName is resolved once at sync time from
// the same GetPersonPlanPeople response's "team" include (see migration
// 00033's comment) so the read path can render the same "prefer the
// resolved team name, fall back to the position name" label the old live
// call produced, without a second lookup.
type PersonPlanHistoryEntry struct {
	PCOPlanPersonID  string
	TeamID           string
	TeamName         string
	TeamPositionName string
	PlanID           string
	SortDate         time.Time
}

// PersonTeamPlanHistoryEntry is one cached per-(person, team) PlanPerson
// row, backing otherAssignmentDates. TeamID isn't repeated here - it's
// already the cache's own key (see PersonTeamPlanHistoryCache/
// ReplacePersonTeamPlanHistory), unlike PersonPlanHistoryEntry, which spans
// every team at once.
type PersonTeamPlanHistoryEntry struct {
	PCOPlanPersonID  string
	TeamPositionName string
	PlanID           string
	SortDate         time.Time
}

// PersonBlockoutCache returns personID's cached blockouts plus when they
// were last synced. ok is false only when no sync has ever run for this
// person (a genuine cache miss) - a completed sync that found zero
// blockouts still returns ok=true with an empty slice, so the caller never
// re-treats "checked, nothing found" as "never checked."
func (s *Store) PersonBlockoutCache(ctx context.Context, organizationID, personID string) (blockouts []PersonBlockout, syncedAt time.Time, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT synced_at FROM person_blockout_sync WHERE organization_id = $1 AND person_id = $2
	`, organizationID, personID).Scan(&syncedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT pco_blockout_id, starts_at, ends_at, reason, repeat_frequency
		FROM person_blockouts WHERE organization_id = $1 AND person_id = $2
	`, organizationID, personID)
	if err != nil {
		return nil, syncedAt, true, err
	}
	defer rows.Close()

	for rows.Next() {
		var b PersonBlockout
		if err := rows.Scan(&b.PCOBlockoutID, &b.StartsAt, &b.EndsAt, &b.Reason, &b.RepeatFrequency); err != nil {
			return nil, syncedAt, true, err
		}
		// .UTC(): Postgres timestamptz round-trips through database/sql as a
		// time.Time in the driver/session's local zone, not the UTC location
		// every one of these values actually arrived in from Planning Center
		// (see PersonPlanHistoryCache's own comment on the same normalization
		// - blockoutWarning's own comparisons are location-independent
		// today, but this keeps "every time.Time handed to business logic is
		// UTC" a true invariant rather than one that happens to hold only
		// for callers that don't do calendar-day math).
		b.StartsAt = b.StartsAt.UTC()
		b.EndsAt = b.EndsAt.UTC()
		blockouts = append(blockouts, b)
	}
	return blockouts, syncedAt, true, rows.Err()
}

// ReplacePersonBlockouts wholesale-replaces personID's cached blockouts
// (delete + insert in one transaction, following ImportNewSongsFromPCO/
// RevertSet's idiom in sets.go) and stamps the sync marker with now(). This
// data is small per person, so a full replace on every sync is simplest
// correct option - no need to diff against what was there before.
func (s *Store) ReplacePersonBlockouts(ctx context.Context, organizationID, personID string, blockouts []PersonBlockout) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM person_blockouts WHERE organization_id = $1 AND person_id = $2`, organizationID, personID); err != nil {
		return err
	}
	for _, b := range blockouts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO person_blockouts (organization_id, person_id, pco_blockout_id, starts_at, ends_at, reason, repeat_frequency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, organizationID, personID, b.PCOBlockoutID, b.StartsAt, b.EndsAt, b.Reason, b.RepeatFrequency); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO person_blockout_sync (organization_id, person_id, synced_at)
		VALUES ($1, $2, now())
		ON CONFLICT (organization_id, person_id) DO UPDATE SET synced_at = now()
	`, organizationID, personID); err != nil {
		return err
	}
	return tx.Commit()
}

// PersonPlanHistoryCache is PersonBlockoutCache's counterpart for the
// all-teams plan-people history behind crossTeamAssignmentsThisWeek.
func (s *Store) PersonPlanHistoryCache(ctx context.Context, organizationID, personID string) (entries []PersonPlanHistoryEntry, syncedAt time.Time, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT synced_at FROM person_plan_history_sync WHERE organization_id = $1 AND person_id = $2
	`, organizationID, personID).Scan(&syncedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT pco_plan_person_id, team_id, team_name, team_position_name, plan_id, sort_date
		FROM person_plan_history WHERE organization_id = $1 AND person_id = $2
	`, organizationID, personID)
	if err != nil {
		return nil, syncedAt, true, err
	}
	defer rows.Close()

	for rows.Next() {
		var e PersonPlanHistoryEntry
		if err := rows.Scan(&e.PCOPlanPersonID, &e.TeamID, &e.TeamName, &e.TeamPositionName, &e.PlanID, &e.SortDate); err != nil {
			return nil, syncedAt, true, err
		}
		// .UTC(): SortDate feeds crossTeamAssignmentsThisWeek's sameWeek
		// check (rosters_warnings.go), which computes each side's own
		// Mon-Sun week boundary via t.Date()/t.Location() - genuinely
		// location-sensitive, unlike a plain .Before()/.After() comparison.
		// Postgres timestamptz round-trips through database/sql in the
		// driver/session's local zone, not the UTC location this value
		// actually arrived in from Planning Center (planSortDatesFromIncluded
		// parses an RFC3339 "Z" timestamp, always UTC) - comparing a
		// UTC-located targetDate (fresh from PCO, e.g. computeCandidateWarnings'
		// pco.GetPlan call) against a local-located SortDate from this cache
		// computes each one's calendar week in a different zone, silently
		// missing a real same-week match when the local zone's offset moves
		// a date across the Monday boundary relative to UTC. Normalizing
		// here, once, keeps every SortDate this cache ever hands out in the
		// same zone every other plan date in this app already is.
		e.SortDate = e.SortDate.UTC()
		entries = append(entries, e)
	}
	return entries, syncedAt, true, rows.Err()
}

// ReplacePersonPlanHistory wholesale-replaces personID's cached all-teams
// plan-people history - see ReplacePersonBlockouts for the pattern this
// follows.
func (s *Store) ReplacePersonPlanHistory(ctx context.Context, organizationID, personID string, entries []PersonPlanHistoryEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM person_plan_history WHERE organization_id = $1 AND person_id = $2`, organizationID, personID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO person_plan_history (organization_id, person_id, pco_plan_person_id, team_id, team_name, team_position_name, plan_id, sort_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, organizationID, personID, e.PCOPlanPersonID, e.TeamID, e.TeamName, e.TeamPositionName, e.PlanID, e.SortDate); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO person_plan_history_sync (organization_id, person_id, synced_at)
		VALUES ($1, $2, now())
		ON CONFLICT (organization_id, person_id) DO UPDATE SET synced_at = now()
	`, organizationID, personID); err != nil {
		return err
	}
	return tx.Commit()
}

// PersonTeamPlanHistoryCache is PersonBlockoutCache's counterpart for the
// per-team plan-people history behind otherAssignmentDates - additionally
// keyed by teamID, matching the sync-marker/detail tables' own extra key
// column (see migration 00033).
func (s *Store) PersonTeamPlanHistoryCache(ctx context.Context, organizationID, personID, teamID string) (entries []PersonTeamPlanHistoryEntry, syncedAt time.Time, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT synced_at FROM person_team_plan_history_sync WHERE organization_id = $1 AND person_id = $2 AND team_id = $3
	`, organizationID, personID, teamID).Scan(&syncedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT pco_plan_person_id, team_position_name, plan_id, sort_date
		FROM person_team_plan_history WHERE organization_id = $1 AND person_id = $2 AND team_id = $3
	`, organizationID, personID, teamID)
	if err != nil {
		return nil, syncedAt, true, err
	}
	defer rows.Close()

	for rows.Next() {
		var e PersonTeamPlanHistoryEntry
		if err := rows.Scan(&e.PCOPlanPersonID, &e.TeamPositionName, &e.PlanID, &e.SortDate); err != nil {
			return nil, syncedAt, true, err
		}
		// .UTC(): see PersonPlanHistoryCache's identical normalization above
		// - frequencyWarning's own math (.Sub()) is location-independent
		// today, but this keeps every SortDate this app hands to business
		// logic in the same zone regardless of which cache it came from.
		e.SortDate = e.SortDate.UTC()
		entries = append(entries, e)
	}
	return entries, syncedAt, true, rows.Err()
}

// ReplacePersonTeamPlanHistory wholesale-replaces personID's cached
// per-team plan-people history for teamID - see ReplacePersonBlockouts for
// the pattern this follows.
func (s *Store) ReplacePersonTeamPlanHistory(ctx context.Context, organizationID, personID, teamID string, entries []PersonTeamPlanHistoryEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM person_team_plan_history WHERE organization_id = $1 AND person_id = $2 AND team_id = $3`, organizationID, personID, teamID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO person_team_plan_history (organization_id, person_id, team_id, pco_plan_person_id, team_position_name, plan_id, sort_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, organizationID, personID, teamID, e.PCOPlanPersonID, e.TeamPositionName, e.PlanID, e.SortDate); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO person_team_plan_history_sync (organization_id, person_id, team_id, synced_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (organization_id, person_id, team_id) DO UPDATE SET synced_at = now()
	`, organizationID, personID, teamID); err != nil {
		return err
	}
	return tx.Commit()
}

// AnyPersonDataSynced reports whether at least one of personIDs has ever
// completed a sync pass in any of the three caches above, for organizationID.
// Backs the Team Builder's "still gathering data" notice (see
// buildRosterBuilderData/buildRosterLightData): a brand-new org's very
// first render has nothing synced yet for anyone, which would otherwise
// look identical to "checked, no warnings apply" - this is the cheap,
// render-time signal (mirroring PopulateNavState's songInfoNeedsSync badge
// in internal/server/middleware/nav.go - no new tracked state, recomputed
// live from the sync-marker tables that already exist) that tells the two
// cases apart. Self-clearing: once any relevant person has a synced_at row
// in any of the three tables, this returns true and the notice stops
// appearing on its own.
//
// Returns false, nil for an empty personIDs (nothing to check, nothing
// pending to report).
func (s *Store) AnyPersonDataSynced(ctx context.Context, organizationID string, personIDs []string) (bool, error) {
	if len(personIDs) == 0 {
		return false, nil
	}

	placeholders := make([]string, len(personIDs))
	args := make([]any, 0, len(personIDs)+1)
	args = append(args, organizationID)
	for i, id := range personIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1 FROM person_blockout_sync WHERE organization_id = $1 AND person_id IN (%s)
			UNION ALL
			SELECT 1 FROM person_plan_history_sync WHERE organization_id = $1 AND person_id IN (%s)
			UNION ALL
			SELECT 1 FROM person_team_plan_history_sync WHERE organization_id = $1 AND person_id IN (%s)
		)
	`, inClause, inClause, inClause)

	var any bool
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&any)
	return any, err
}

// ClearPersonDataCache deletes every row this package's person-data cache
// tables hold for organizationID - all three detail tables and their sync
// markers (migration 00033) - see AccountController.ClearPersonDataCache
// (internal/server/controllers/account.go), the Account page's "nuclear"
// cache-clear action. Non-destructive to real PCO data: the next roster
// render simply re-syncs blockouts/plan history from Planning Center as
// needed, the same as a brand-new org that's never synced yet (see
// AnyPersonDataSynced's own "never synced" case above). One transaction
// across all six tables, same delete-in-a-tx idiom as
// ReplacePersonBlockouts/ReplacePersonPlanHistory/
// ReplacePersonTeamPlanHistory above, just spanning every person/team for
// this org at once instead of one person(/team) at a time.
func (s *Store) ClearPersonDataCache(ctx context.Context, organizationID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range []string{
		"person_blockouts", "person_blockout_sync",
		"person_plan_history", "person_plan_history_sync",
		"person_team_plan_history", "person_team_plan_history_sync",
	} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE organization_id = $1`, table), organizationID); err != nil {
			return err
		}
	}

	return tx.Commit()
}
