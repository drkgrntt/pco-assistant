package scheduling

import "context"

// SongAttachmentInfo is one of a song's cached Planning Center Attachments
// (lead sheet/chord chart PDF, scan, audio - any arbitrary uploaded file) -
// see song_attachment_cache (migration 00035). Unlike
// song_arrangement_cache's one-row-per-song shape, a song can have zero,
// one, or many of these, and there's no reliable cross-org way to know
// which one (if any) is "the" lead sheet - the attachment-type label
// Planning Center shows is org-defined, not a fixed enum - so every cached
// attachment is surfaced, not just a single guessed one. See
// docs/plans/shipped/song-lead-sheet-attachments-plan.md.
//
// Deliberately carries no URL: even though go-pco's AttachmentAttributes.URL
// is confirmed live to be a stable link (Planning Center's own attachment
// page, not an expiring pre-signed one - see attachments.go's doc comment),
// this app still doesn't persist it, so a click always reflects whatever's
// actually there in Planning Center right now (renamed/removed since this
// song's row was last synced) rather than a possibly-stale cached value - a
// click-triggered live-fetch is cheap enough to just always do (see the
// attachment-open route in controllers).
type SongAttachmentInfo struct {
	// PCOAttachmentID is the attachment's own Planning Center id - needed
	// to re-fetch a fresh URL for this exact attachment on click, since the
	// URL itself is never cached (see this struct's own doc comment).
	PCOAttachmentID string
	Filename        string
	ContentType     string
}

// UpsertSongAttachments replaces songID's entire cached attachment list for
// this org with attachments - a full replace-all, not a merge, in one
// transaction (delete-then-insert, same pattern as
// PersonBlockoutCache/PersonPlanHistoryCache in person_data_cache.go) so an
// attachment removed in Planning Center since the last sync doesn't linger
// here forever. Safe to call with an empty attachments slice (a song with
// no attachments at all just ends up with no cached rows, not an error).
func (s *Store) UpsertSongAttachments(ctx context.Context, organizationID, songID string, attachments []SongAttachmentInfo) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM song_attachment_cache WHERE organization_id = $1 AND song_id = $2`, organizationID, songID); err != nil {
		return err
	}
	for _, a := range attachments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO song_attachment_cache (organization_id, song_id, pco_attachment_id, filename, content_type, cached_at)
			VALUES ($1, $2, $3, $4, $5, now())
		`, organizationID, songID, a.PCOAttachmentID, a.Filename, a.ContentType); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SongAttachments returns every cached attachment for every song in this
// org, keyed by song id - a single bulk read for a whole page render
// (buildSetBuilderData), mirroring SongArrangementInfo's own bulk-map
// shape. A song with no cached attachments simply has no entry in the
// returned map (not an empty slice) - callers should treat a missing key
// the same as a present-but-empty one.
func (s *Store) SongAttachments(ctx context.Context, organizationID string) (map[string][]SongAttachmentInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT song_id, pco_attachment_id, filename, content_type
		FROM song_attachment_cache
		WHERE organization_id = $1
		ORDER BY filename
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cache := map[string][]SongAttachmentInfo{}
	for rows.Next() {
		var songID string
		var info SongAttachmentInfo
		if err := rows.Scan(&songID, &info.PCOAttachmentID, &info.Filename, &info.ContentType); err != nil {
			return nil, err
		}
		cache[songID] = append(cache[songID], info)
	}

	return cache, rows.Err()
}
