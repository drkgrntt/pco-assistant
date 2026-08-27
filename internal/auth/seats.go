package auth

import "context"

// SeatCounts returns, for every organization with at least one live
// session, how many distinct people have signed in within the current
// session window - see sessions.expires_at / SessionTTL. This is what
// "seat" means everywhere in billing (see internal/billing): a person
// naturally stops counting once their session ages out and gets purged
// (jobs.AuthJobs' daily DeleteExpiredSessions), so there's no explicit
// seat-removal feature to build - someone who signed in once and never
// returned simply falls out of the count on its own.
func (s *Store) SeatCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT organization_id, COUNT(DISTINCT person_id)
		FROM sessions
		WHERE organization_id != '' AND expires_at > now()
		GROUP BY organization_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var orgID string
		var count int
		if err := rows.Scan(&orgID, &count); err != nil {
			return nil, err
		}
		counts[orgID] = count
	}

	return counts, rows.Err()
}
