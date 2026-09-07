package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// A Set is a draft song lineup for one PCO plan - local to this app only,
// nothing here is written back to Planning Center (see the package doc).
// There's at most one per (service_type_id, plan_id) pair.
type Set struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	ServiceTypeID  string    `json:"service_type_id"`
	PlanID         string    `json:"plan_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SetSong struct {
	ID        int64     `json:"id"`
	SetID     int64     `json:"set_id"`
	SongID    string    `json:"song_id"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	// PCOItemID is set once this song has been pushed to Planning Center as
	// a real plan item (see PublishSet) - empty means not published yet.
	PCOItemID string `json:"pco_item_id"`
	// PendingDelete marks a published song that was removed/replaced
	// locally - its PCO item still needs deleting on the next publish.
	// ListSetSongs never returns these; ListPendingDeletions does.
	PendingDelete bool `json:"pending_delete"`
	// AnchorItemID is the PCO item id this song should sit immediately
	// after in the full order of service - "" means "top of service,
	// before everything else." Position (above) only orders songs sharing
	// the same anchor; see MoveSongInSet and the controller's
	// buildServiceOrder for how anchor groups merge with the plan's other
	// items into one displayed/published order.
	AnchorItemID string `json:"anchor_item_id"`
	// Moved is true once RepositionSongInSet has placed this row somewhere
	// new without that move being published yet - unlike PCOItemID/
	// PendingDelete (which track creation/deletion), a drag-and-drop move
	// leaves both of those untouched, so without this a reordered-but-
	// already-published song looked identical to one that was never
	// touched at all. Cleared by ClearMovedFlags after a successful
	// publish; never set on rows RevertSet rebuilds fresh from Planning
	// Center, since those start in sync by definition.
	Moved bool `json:"moved"`
	// Description is a local, editable per-occurrence note for this one
	// song in this one set ("cut the bridge this week," "new keys player,
	// go slow") - distinct from the song's persistent arrangement notes
	// (song_arrangement_cache.Notes). Empty means "nothing set yet," not
	// "cleared to blank on purpose" - PublishSet/RefreshSongInfo fall back
	// to the arrangement's own notes as the item's Description default
	// when this is empty, same as before this field existed.
	Description string `json:"description"`
	// KeyOverride is a local, editable per-occurrence key ("put this in Bb
	// this week"), distinct from the song's arrangement's own default key
	// (song_arrangement_cache.Key). Empty means "use the arrangement's
	// default," same fallback convention as Description -
	// PublishSet/RefreshSongInfo resolve this to a real PCO Key id (see
	// resolveKeyID, scheduling.go) without ever touching the arrangement's
	// own default key.
	KeyOverride string `json:"key_override"`
	// InfoPending is true once Description, KeyOverride, or LeaderPersonID
	// has been edited on an already-published song without that edit
	// reaching Planning Center yet - the item-content equivalent of Moved
	// above, for exactly the same reason: without it, an
	// edited-but-not-yet-refreshed song looked identical to one that was
	// already in sync. Set by UpdateSetSongDescription/
	// UpdateSetSongKeyOverride/UpdateSetSongLeader; cleared by
	// ClearInfoPending once that content is actually pushed (a successful
	// create at Publish time, or a successful update via Refresh song
	// info).
	InfoPending bool `json:"info_pending"`
	// LeaderPersonID is this one occurrence's local per-occurrence song
	// leader ("who's actually leading this song this week") - a PCO
	// person id, not free text, so it stays a real person reference
	// rather than a name typed in two places that can drift apart. Empty
	// means "no leader set," same convention as Description/KeyOverride.
	// Editing this only updates the local draft (UpdateSetSongLeader); it
	// reaches Planning Center as a reserved first line inside the org's
	// configured "vocal notes" Item Note category on the next Publish or
	// Refresh song info (see syncLeaderNote, sets_publish.go, and
	// scheduling.Store.VocalNoteCategoryID) - never a category of its
	// own. See docs/plans/shipped/song-leader-plan.md.
	LeaderPersonID string `json:"leader_person_id"`
}

// FindSet looks up an existing set without creating one - unlike
// GetOrCreateSet, it reports found=false rather than materializing a new
// empty row when there's no draft yet for this plan. Used for status
// checks (e.g. the plans list) that shouldn't create clutter just by
// looking. Scoped to organizationID: every other set_songs/sets method in
// this file takes only a bare setID/rowID, trusting it's already
// org-scoped, because callers always re-derive it fresh from this (or
// GetOrCreateSet) on every request rather than trusting a client-supplied
// set id directly - see the scheduling controller's route handlers, which
// only ever take service_type_id/plan_id from the URL, never a set id.
func (s *Store) FindSet(ctx context.Context, organizationID, serviceTypeID, planID string) (set Set, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT id, organization_id, service_type_id, plan_id, created_at, updated_at
		FROM sets WHERE organization_id = $1 AND service_type_id = $2 AND plan_id = $3
	`, organizationID, serviceTypeID, planID).Scan(&set.ID, &set.OrganizationID, &set.ServiceTypeID, &set.PlanID, &set.CreatedAt, &set.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Set{}, false, nil
	}
	if err != nil {
		return Set{}, false, err
	}
	return set, true, nil
}

// GetOrCreateSet returns organizationID's draft set for a plan, creating an
// empty one on first use. Building a set always starts from picking a plan,
// so callers don't need to distinguish "new" from "already had one going" -
// see FindSet's doc comment for why every other method in this file trusts
// a bare setID without also taking organizationID.
func (s *Store) GetOrCreateSet(ctx context.Context, organizationID, serviceTypeID, planID string) (Set, error) {
	if serviceTypeID == "" || planID == "" {
		return Set{}, errors.New("service_type_id and plan_id are required")
	}

	var set Set
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO sets (organization_id, service_type_id, plan_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, service_type_id, plan_id) DO UPDATE SET updated_at = sets.updated_at
		RETURNING id, organization_id, service_type_id, plan_id, created_at, updated_at
	`, organizationID, serviceTypeID, planID).Scan(&set.ID, &set.OrganizationID, &set.ServiceTypeID, &set.PlanID, &set.CreatedAt, &set.UpdatedAt)
	if err != nil {
		return Set{}, err
	}

	return set, nil
}

// ListSetSongs returns the set's active (non-tombstoned) songs, in order.
// This is what the builder page/reorder/add logic treats as "the set" -
// see ListPendingDeletions for songs awaiting removal from Planning Center.
func (s *Store) ListSetSongs(ctx context.Context, setID int64) ([]SetSong, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, set_id, song_id, position, created_at, pco_item_id, anchor_item_id, moved, description, key_override, info_pending, leader_person_id
		FROM set_songs
		WHERE set_id = $1 AND pending_delete = false
		ORDER BY position ASC
	`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	songs := []SetSong{}
	for rows.Next() {
		var ss SetSong
		var pcoItemID sql.NullString
		if err := rows.Scan(&ss.ID, &ss.SetID, &ss.SongID, &ss.Position, &ss.CreatedAt, &pcoItemID, &ss.AnchorItemID, &ss.Moved, &ss.Description, &ss.KeyOverride, &ss.InfoPending, &ss.LeaderPersonID); err != nil {
			return nil, err
		}
		ss.PCOItemID = pcoItemID.String
		songs = append(songs, ss)
	}

	return songs, rows.Err()
}

// UpdateSetSongDescription sets rowID's local per-occurrence note (see
// SetSong.Description) - a plain field update, not part of the publish
// cycle: it takes effect in Planning Center on the next Publish or Refresh
// song info, same as every other local draft edit on this page (title
// isn't editable here, but tags/relationships/roles all work the same way).
// Always marks info_pending true, even if the new value happens to match
// what's already live - same "don't bother checking, just flag it"
// simplicity RepositionSongInSet already uses for moved.
func (s *Store) UpdateSetSongDescription(ctx context.Context, setID, rowID int64, description string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET description = $1, info_pending = true WHERE id = $2 AND set_id = $3 AND pending_delete = false
	`, description, rowID, setID)
	return err
}

// UpdateSetSongKeyOverride sets rowID's local per-occurrence key (see
// SetSong.KeyOverride) - same local-draft-only semantics (including
// info_pending) as UpdateSetSongDescription above.
func (s *Store) UpdateSetSongKeyOverride(ctx context.Context, setID, rowID int64, keyOverride string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET key_override = $1, info_pending = true WHERE id = $2 AND set_id = $3 AND pending_delete = false
	`, keyOverride, rowID, setID)
	return err
}

// UpdateSetSongLeader sets rowID's local per-occurrence song leader (see
// SetSong.LeaderPersonID) - same local-draft-only semantics (including
// info_pending) as UpdateSetSongKeyOverride/UpdateSetSongDescription
// above. personID is a PCO person id, "" meaning "no leader" (clears it).
func (s *Store) UpdateSetSongLeader(ctx context.Context, setID, rowID int64, personID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET leader_person_id = $1, info_pending = true WHERE id = $2 AND set_id = $3 AND pending_delete = false
	`, personID, rowID, setID)
	return err
}

// ClearInfoPending marks rowID's Description/KeyOverride as in sync with
// Planning Center - called once that content is actually pushed (a
// successful create at Publish time, or a successful update via Refresh
// song info), not before. Deliberately per-row, unlike ClearMovedFlags'
// whole-set sweep: RefreshSongInfo pushes one song at a time and can fail
// partway through, so only the rows that actually succeeded should stop
// showing as pending.
func (s *Store) ClearInfoPending(ctx context.Context, setID, rowID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET info_pending = false WHERE id = $1 AND set_id = $2
	`, rowID, setID)
	return err
}

// SyncSetSongInfoFromPCO overwrites rowID's local Description/KeyOverride
// with PCO's own current values - MergeSet's already-tracked-item
// reconciliation (see reconcileTrackedItems, sets_publish.go), for a row
// whose InfoPending is already false (no competing unpushed local edit -
// see the live-sync-reflection plan's conflict-policy decision). Unlike
// UpdateSetSongDescription/UpdateSetSongKeyOverride above, this
// deliberately does NOT set info_pending true: these values are being
// pulled in because they already match what's live in Planning Center,
// not because of a new local edit that still needs pushing.
func (s *Store) SyncSetSongInfoFromPCO(ctx context.Context, setID, rowID int64, description, keyOverride string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET description = $1, key_override = $2 WHERE id = $3 AND set_id = $4 AND pending_delete = false
	`, description, keyOverride, rowID, setID)
	return err
}

// ListPendingDeletions returns songs that were removed/replaced locally
// after already being published - their pco_item_id still needs deleting
// in Planning Center on the next PublishSet run.
func (s *Store) ListPendingDeletions(ctx context.Context, setID int64) ([]SetSong, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, set_id, song_id, position, created_at, pco_item_id
		FROM set_songs
		WHERE set_id = $1 AND pending_delete = true
	`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	songs := []SetSong{}
	for rows.Next() {
		var ss SetSong
		var pcoItemID sql.NullString
		if err := rows.Scan(&ss.ID, &ss.SetID, &ss.SongID, &ss.Position, &ss.CreatedAt, &pcoItemID); err != nil {
			return nil, err
		}
		ss.PCOItemID = pcoItemID.String
		ss.PendingDelete = true
		songs = append(songs, ss)
	}

	return songs, rows.Err()
}

// MarkSetSongPublished records that the set song rowID was pushed to
// Planning Center as item itemID, so a later PublishSet run treats it as
// already-published (updating it, not re-creating it).
func (s *Store) MarkSetSongPublished(ctx context.Context, setID, rowID int64, itemID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET pco_item_id = $1 WHERE id = $2 AND set_id = $3 AND pending_delete = false
	`, itemID, rowID, setID)
	return err
}

// ClearSetSongPCOItemID resets rowID back to "not published yet" -
// MarkSetSongPublished's reverse - for the PCO-side deletion race MergeSet's
// already-tracked-item reconciliation handles (see reconcileTrackedItems,
// sets_publish.go): an active, non-tombstoned row whose linked PCO item no
// longer exists among the plan's live items. Clearing pco_item_id back to
// "" (rather than deleting the local row) makes it re-creatable on the next
// Publish, the same as any other never-yet-published song - the local song
// choice itself is preserved, only the stale PCO link is dropped.
func (s *Store) ClearSetSongPCOItemID(ctx context.Context, setID, rowID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE set_songs SET pco_item_id = NULL WHERE id = $1 AND set_id = $2 AND pending_delete = false
	`, rowID, setID)
	return err
}

// DeleteSetSongRow permanently removes a tombstoned row after its PCO item
// has actually been deleted (see PublishSet) - there's nothing left to
// track once that succeeds.
func (s *Store) DeleteSetSongRow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM set_songs WHERE id = $1`, id)
	return err
}

// RevertSet discards every local song (including tombstoned pending
// deletions) and replaces them with importRows - used to reset a set back
// to exactly what Planning Center currently has. The caller (see the
// scheduling controller's RevertSet) derives importRows from a live
// GetItems call, so this is purely a local bulk-replace; it never talks to
// PCO itself.
func (s *Store) RevertSet(ctx context.Context, setID int64, importRows []SetSong) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM set_songs WHERE set_id = $1`, setID); err != nil {
		return err
	}

	for _, row := range importRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO set_songs (set_id, song_id, position, pco_item_id, anchor_item_id)
			VALUES ($1, $2, $3, $4, $5)
		`, setID, row.SongID, row.Position, row.PCOItemID, row.AnchorItemID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sets SET last_touched_set_song_id = NULL WHERE id = $1`, setID); err != nil {
		return err
	}

	return tx.Commit()
}

// ImportNewSongsFromPCO adds importRows as new local set_songs rows -
// MergeSet's ("pull in from Planning Center") store-side half: additive
// only, it never deletes or modifies any existing row, unlike RevertSet's
// full replace. The caller (see the scheduling controller's MergeSet)
// derives importRows from a live GetItems call via deriveMergeImportRows,
// already filtered down to song-linked items not already tracked locally -
// this just inserts them, each keeping the PCOItemID/AnchorItemID the walk
// already resolved (so a freshly-imported row starts "published," same as
// RevertSet's rows do - it already exists in Planning Center, there's
// nothing left to push). Position is computed per row, within the same
// transaction, as one past whatever's currently the highest position in
// that row's anchor group - so several new rows sharing an anchor (from
// this import or already present locally) chain in walk order, the same
// "append after what's there" rule AddSongToSet's chaining insert uses.
// Returns how many rows were actually inserted.
func (s *Store) ImportNewSongsFromPCO(ctx context.Context, setID int64, importRows []SetSong) (int, error) {
	if len(importRows) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, row := range importRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO set_songs (set_id, song_id, position, pco_item_id, anchor_item_id)
			SELECT $1, $2, COALESCE(MAX(position), -1) + 1, $3, $4
			FROM set_songs WHERE set_id = $1 AND anchor_item_id = $4 AND pending_delete = false
		`, setID, row.SongID, row.PCOItemID, row.AnchorItemID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return len(importRows), nil
}

// AddSongToSet adds songID right after whichever song was last placed here
// (see sets.last_touched_set_song_id) - so adding several songs in a row
// chains them together at wherever you're currently working, rather than
// always resetting to the top of service. With nothing to chain from (an
// empty set, or the last-touched row no longer active), it falls back to
// fallbackAnchorItemID instead - the build page resolves this from
// PreferredFirstSongAnchorTitle (see AddSetSong's firstSongFallbackAnchor
// in the controllers package) so a brand-new set's first song lands
// wherever this service type's first song usually goes, not always the
// literal top of service; pass "" to get that literal top-of-service
// behavior, same as before this parameter existed. Refuses to add a song
// that's already active in the set - see DuplicateSongInSet for the
// deliberate way to get a second copy (e.g. reprised as a response song).
//
// The second return value is the song id it chained after ("" when there
// was nothing to chain from, including whenever fallbackAnchorItemID was
// used instead) - the build page uses this to offer an inline "remember
// that these two go together?" relationship prompt right after the add,
// without making the caller re-derive the same adjacency this method
// already had to compute for positioning.
func (s *Store) AddSongToSet(ctx context.Context, setID int64, songID, fallbackAnchorItemID string) (SetSong, string, error) {
	if songID == "" {
		return SetSong{}, "", errors.New("song_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SetSong{}, "", err
	}
	defer tx.Rollback()

	var alreadyPresent bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM set_songs WHERE set_id = $1 AND song_id = $2 AND pending_delete = false)
	`, setID, songID).Scan(&alreadyPresent); err != nil {
		return SetSong{}, "", err
	}
	if alreadyPresent {
		return SetSong{}, "", fmt.Errorf("that song is already in this set")
	}

	anchor := fallbackAnchorItemID
	var afterPosition int
	var chainedAfterSongID string
	chaining := false

	var lastTouched sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT last_touched_set_song_id FROM sets WHERE id = $1`, setID).Scan(&lastTouched); err != nil {
		return SetSong{}, "", err
	}
	if lastTouched.Valid {
		err := tx.QueryRowContext(ctx, `
			SELECT position, anchor_item_id, song_id FROM set_songs
			WHERE id = $1 AND set_id = $2 AND pending_delete = false
		`, lastTouched.Int64, setID).Scan(&afterPosition, &anchor, &chainedAfterSongID)
		if err == nil {
			chaining = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return SetSong{}, "", err
		}
	}

	var ss SetSong
	var pcoItemID sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO set_songs (set_id, song_id, position, anchor_item_id)
		SELECT $1, $2, COALESCE(MAX(position), -1) + 1, $3
		FROM set_songs WHERE set_id = $1 AND pending_delete = false
		RETURNING id, set_id, song_id, position, created_at, pco_item_id, anchor_item_id
	`, setID, songID, anchor).Scan(&ss.ID, &ss.SetID, &ss.SongID, &ss.Position, &ss.CreatedAt, &pcoItemID, &ss.AnchorItemID)
	if err != nil {
		return SetSong{}, "", err
	}
	ss.PCOItemID = pcoItemID.String

	if chaining {
		// Splice the new row into the global position order right after
		// afterID's row, via a fractional sort key that only exists for
		// this one ORDER BY - ROW_NUMBER then turns it back into clean
		// contiguous integers. Filtering by anchor_item_id for display
		// preserves relative order, so this also lands it immediately
		// after afterID within their shared anchor group specifically,
		// not just somewhere in the global sequence.
		if _, err := tx.ExecContext(ctx, `
			WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					ORDER BY CASE WHEN id = $2 THEN $3 + 0.5 ELSE position END
				) - 1 AS new_position
				FROM set_songs WHERE set_id = $1 AND pending_delete = false
			)
			UPDATE set_songs SET position = ranked.new_position
			FROM ranked WHERE set_songs.id = ranked.id
		`, setID, ss.ID, afterPosition); err != nil {
			return SetSong{}, "", err
		}

		if err := tx.QueryRowContext(ctx, `
			SELECT position FROM set_songs WHERE id = $1
		`, ss.ID).Scan(&ss.Position); err != nil {
			return SetSong{}, "", err
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sets SET last_touched_set_song_id = $1 WHERE id = $2`, ss.ID, setID); err != nil {
		return SetSong{}, "", err
	}

	if err := tx.Commit(); err != nil {
		return SetSong{}, "", err
	}

	return ss, chainedAfterSongID, nil
}

// DuplicateSongInSet creates a second occurrence of sourceRowID's song
// (same song_id), placed immediately after it in the same anchor group -
// for a song that's reprised elsewhere in the service (e.g. the same
// opener sung again as the response). The new row starts unpublished
// regardless of whether the source is; publishing creates it as its own
// separate Planning Center item linked to the same library song.
func (s *Store) DuplicateSongInSet(ctx context.Context, setID, sourceRowID int64) (SetSong, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SetSong{}, err
	}
	defer tx.Rollback()

	var songID, anchor string
	var position int
	if err := tx.QueryRowContext(ctx, `
		SELECT song_id, position, anchor_item_id FROM set_songs
		WHERE id = $1 AND set_id = $2 AND pending_delete = false
	`, sourceRowID, setID).Scan(&songID, &position, &anchor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SetSong{}, sql.ErrNoRows
		}
		return SetSong{}, err
	}

	var ss SetSong
	var pcoItemID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO set_songs (set_id, song_id, position, anchor_item_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, set_id, song_id, position, created_at, pco_item_id, anchor_item_id
	`, setID, songID, position, anchor).Scan(&ss.ID, &ss.SetID, &ss.SongID, &ss.Position, &ss.CreatedAt, &pcoItemID, &ss.AnchorItemID); err != nil {
		return SetSong{}, err
	}
	ss.PCOItemID = pcoItemID.String

	// Splice the duplicate to sit right after the source (same trick as
	// AddSongToSet's chaining) rather than leaving it wherever the insert
	// above happened to place it in the global order.
	if _, err := tx.ExecContext(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY CASE WHEN id = $2 THEN $3 + 0.5 ELSE position END
			) - 1 AS new_position
			FROM set_songs WHERE set_id = $1 AND pending_delete = false
		)
		UPDATE set_songs SET position = ranked.new_position
		FROM ranked WHERE set_songs.id = ranked.id
	`, setID, ss.ID, position); err != nil {
		return SetSong{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT position FROM set_songs WHERE id = $1`, ss.ID).Scan(&ss.Position); err != nil {
		return SetSong{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sets SET last_touched_set_song_id = $1 WHERE id = $2`, ss.ID, setID); err != nil {
		return SetSong{}, err
	}

	if err := tx.Commit(); err != nil {
		return SetSong{}, err
	}

	return ss, nil
}

// RemoveSongFromSet removes rowID from the active set. If it was already
// published, the row is kept as a tombstone (pending_delete) so the next
// PublishSet run knows to delete its PCO item too, and its position is
// freed (set negative) so a re-add or resequence can reuse the slot.
// Never-published rows are just hard-deleted, since there's nothing in PCO
// to reconcile. Either way, remaining active positions stay contiguous
// (0..n-1) for AddSongToSet/RepositionSongInSet's arithmetic to rely on.
func (s *Store) RemoveSongFromSet(ctx context.Context, setID, rowID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var pcoItemID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT pco_item_id FROM set_songs WHERE id = $1 AND set_id = $2 AND pending_delete = false
	`, rowID, setID).Scan(&pcoItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}

	if pcoItemID.Valid && pcoItemID.String != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE set_songs SET pending_delete = true, position = -id WHERE id = $1
		`, rowID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM set_songs WHERE id = $1`, rowID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY position) - 1 AS new_position
			FROM set_songs WHERE set_id = $1 AND pending_delete = false
		)
		UPDATE set_songs SET position = ranked.new_position
		FROM ranked WHERE set_songs.id = ranked.id
	`, setID); err != nil {
		return err
	}

	return tx.Commit()
}

// ReplaceSongInSet swaps oldRowID for newSongID in place, preserving
// position. If oldRowID was already published, its row is tombstoned the
// same way RemoveSongFromSet does (so its PCO item gets deleted on the next
// publish); newSongID starts fresh as unpublished and takes over the slot.
// Refuses to replace into a song that's already active elsewhere in the
// set - see DuplicateSongInSet for deliberately creating a second copy.
func (s *Store) ReplaceSongInSet(ctx context.Context, setID, oldRowID int64, newSongID string) (SetSong, error) {
	if newSongID == "" {
		return SetSong{}, errors.New("song_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SetSong{}, err
	}
	defer tx.Rollback()

	var oldSongID string
	var position int
	var oldPCOItemID sql.NullString
	var anchorItemID string
	err = tx.QueryRowContext(ctx, `
		SELECT song_id, position, pco_item_id, anchor_item_id FROM set_songs
		WHERE id = $1 AND set_id = $2 AND pending_delete = false
	`, oldRowID, setID).Scan(&oldSongID, &position, &oldPCOItemID, &anchorItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return SetSong{}, fmt.Errorf("that song is not currently in this set")
	}
	if err != nil {
		return SetSong{}, err
	}
	if oldSongID == newSongID {
		return SetSong{}, errors.New("replacement must be a different song")
	}

	var alreadyPresent bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM set_songs WHERE set_id = $1 AND song_id = $2 AND pending_delete = false)
	`, setID, newSongID).Scan(&alreadyPresent); err != nil {
		return SetSong{}, err
	}
	if alreadyPresent {
		return SetSong{}, fmt.Errorf("that song is already in this set")
	}

	if oldPCOItemID.Valid && oldPCOItemID.String != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE set_songs SET pending_delete = true, position = -id WHERE id = $1
		`, oldRowID); err != nil {
			return SetSong{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM set_songs WHERE id = $1`, oldRowID); err != nil {
			return SetSong{}, err
		}
	}

	var ss SetSong
	var newPCOItemID sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO set_songs (set_id, song_id, position, anchor_item_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, set_id, song_id, position, created_at, pco_item_id, anchor_item_id
	`, setID, newSongID, position, anchorItemID).Scan(&ss.ID, &ss.SetID, &ss.SongID, &ss.Position, &ss.CreatedAt, &newPCOItemID, &ss.AnchorItemID)
	if err != nil {
		return SetSong{}, err
	}
	ss.PCOItemID = newPCOItemID.String

	if _, err := tx.ExecContext(ctx, `UPDATE sets SET last_touched_set_song_id = $1 WHERE id = $2`, ss.ID, setID); err != nil {
		return SetSong{}, err
	}

	if err := tx.Commit(); err != nil {
		return SetSong{}, err
	}

	return ss, nil
}

// RepositionSongInSet moves rowID to sit immediately after afterRowID (a
// currently-active row, inheriting its anchor group) or, when afterRowID is
// 0, to the very start of anchorItemID's group (anchorItemID "" being top
// of service). This is the single operation behind the order-of-service
// drag-and-drop - dropping a row onto another song, or onto a header/other
// foreign item, both boil down to "place after this row."
func (s *Store) RepositionSongInSet(ctx context.Context, setID, rowID int64, anchorItemID string, afterRowID int64) error {
	if rowID == afterRowID {
		return errors.New("cannot place a song after itself")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM set_songs WHERE id = $1 AND set_id = $2 AND pending_delete = false)
	`, rowID, setID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}

	targetAnchor := anchorItemID
	var afterPosition int
	placeAfterRow := afterRowID != 0
	if placeAfterRow {
		var anchor string
		err := tx.QueryRowContext(ctx, `
			SELECT position, anchor_item_id FROM set_songs
			WHERE id = $1 AND set_id = $2 AND pending_delete = false
		`, afterRowID, setID).Scan(&afterPosition, &anchor)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("that song is not currently in this set")
		}
		if err != nil {
			return err
		}
		targetAnchor = anchor
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE set_songs SET anchor_item_id = $1 WHERE id = $2
	`, targetAnchor, rowID); err != nil {
		return err
	}

	// Splice the moved row into the global position order right after
	// afterPosition (or, with no afterRowID, before every other active row
	// - -1 always sorts first since real positions are never negative).
	// Filtering by anchor_item_id for display preserves relative order, so
	// this also lands it correctly within its target group specifically,
	// not just somewhere in the global sequence - see AddSongToSet's
	// chaining logic, which uses the same trick.
	sortKey := "-1"
	args := []any{setID, rowID}
	if placeAfterRow {
		sortKey = "$3 + 0.5"
		args = append(args, afterPosition)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY CASE WHEN id = $2 THEN %s ELSE position END
			) - 1 AS new_position
			FROM set_songs WHERE set_id = $1 AND pending_delete = false
		)
		UPDATE set_songs SET position = ranked.new_position
		FROM ranked WHERE set_songs.id = ranked.id
	`, sortKey), args...); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sets SET last_touched_set_song_id = $1 WHERE id = $2`, rowID, setID); err != nil {
		return err
	}

	// Only the moved row itself is flagged - the renumbering above touches
	// every row's position (keeping 0..n-1 contiguous), but that's just
	// bookkeeping; everyone else's order *relative to each other* is
	// unchanged, so they're not what a publish would need to push. This
	// needs to be pushed on the next publish whether or not the moved row
	// itself is published yet - see SetSong.Moved's doc comment for why
	// the existing Unpublished/PendingDelete signals miss exactly this
	// case.
	if _, err := tx.ExecContext(ctx, `UPDATE set_songs SET moved = true WHERE id = $1`, rowID); err != nil {
		return err
	}

	return tx.Commit()
}

// ClearMovedFlags resets every SetSong.Moved in this set once a reorder has
// actually been pushed to Planning Center - see PublishSet, which calls
// this after a successful pco.ReorderItems, not before, so a failed
// publish attempt correctly leaves the pending flags set for the next try.
func (s *Store) ClearMovedFlags(ctx context.Context, setID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE set_songs SET moved = false WHERE set_id = $1`, setID)
	return err
}

// SetSuggestion is one recommended song, with every reason it's being
// recommended - a song related to the last one added by both flows_well
// and same_theme shows up once, not twice, with both reasons attached.
type SetSuggestion struct {
	SongID            string   `json:"song_id"`
	RelationshipTypes []string `json:"relationship_types"`
	Notes             []string `json:"notes"`
}

// Recommendations suggests songs to add next, based on flows_well/
// same_theme relationships with the most recently added song in the set.
// Songs already in the set are excluded. Returns nil (not an error) for an
// empty set - there's nothing yet to base a suggestion on.
func (s *Store) Recommendations(ctx context.Context, organizationID string, setID int64) ([]SetSuggestion, error) {
	setSongs, err := s.ListSetSongs(ctx, setID)
	if err != nil {
		return nil, err
	}
	if len(setSongs) == 0 {
		return nil, nil
	}

	lastSongID := setSongs[len(setSongs)-1].SongID

	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
	}

	rels, err := s.ListSongRelationships(ctx, organizationID, lastSongID)
	if err != nil {
		return nil, err
	}

	return computeRecommendations(rels, lastSongID, inSet), nil
}

// computeRecommendations is the pure grouping/filtering logic behind
// Recommendations, pulled out so it's unit-testable without a live
// *sql.DB (mirroring backupsToPrune in internal/server/jobs/backup.go and
// cacheEntryFresh in internal/server/middleware/nav.go). rels is every
// relationship touching lastSongID (from ListSongRelationships); inSet
// flags which song IDs are already in the set.
func computeRecommendations(rels []SongRelationship, lastSongID string, inSet map[string]bool) []SetSuggestion {
	bySong := map[string]*SetSuggestion{}
	var order []string
	for _, rel := range rels {
		if rel.RelationshipType != RelationshipFlowsWell && rel.RelationshipType != RelationshipSameTheme {
			continue
		}

		other := rel.SongIDB
		if rel.SongIDA != lastSongID {
			other = rel.SongIDA
		}
		if inSet[other] {
			continue
		}

		sug, ok := bySong[other]
		if !ok {
			sug = &SetSuggestion{SongID: other}
			bySong[other] = sug
			order = append(order, other)
		}
		sug.RelationshipTypes = append(sug.RelationshipTypes, rel.RelationshipType)
		if rel.Notes != "" {
			sug.Notes = append(sug.Notes, rel.Notes)
		}
	}

	suggestions := make([]SetSuggestion, 0, len(order))
	for _, songID := range order {
		suggestions = append(suggestions, *bySong[songID])
	}

	return suggestions
}

type SetConflict struct {
	SongID        string `json:"song_id"`
	ConflictsWith string `json:"conflicts_with"`
}

// AvoidPairings reports avoid_pairing relationships touching this set.
// candidates maps a not-yet-added song ID to the set song ID(s) it's
// flagged against - for warning the user in search results before they add
// it. existing lists pairs where both songs are already in the set (each
// pair reported once), for a heads-up on the set itself. Returns zero
// values (not an error) for an empty set.
func (s *Store) AvoidPairings(ctx context.Context, organizationID string, setID int64) (candidates map[string][]string, existing []SetConflict, err error) {
	setSongs, err := s.ListSetSongs(ctx, setID)
	if err != nil {
		return nil, nil, err
	}
	if len(setSongs) == 0 {
		return nil, nil, nil
	}

	relsBySong := make(map[string][]SongRelationship, len(setSongs))
	for _, ss := range setSongs {
		rels, err := s.ListSongRelationships(ctx, organizationID, ss.SongID)
		if err != nil {
			return nil, nil, err
		}
		relsBySong[ss.SongID] = rels
	}

	candidates, existing = computeAvoidPairings(setSongs, relsBySong)
	return candidates, existing, nil
}

// computeAvoidPairings is the pure conflict-detection logic behind
// AvoidPairings, pulled out so it's unit-testable without a live *sql.DB
// (see computeRecommendations above for the same reasoning). relsBySong
// holds every relationship touching each set song, keyed by that song's
// ID (each fetched via ListSongRelationships in the loop AvoidPairings
// runs). Returns zero values for an empty setSongs, matching AvoidPairings
// itself.
func computeAvoidPairings(setSongs []SetSong, relsBySong map[string][]SongRelationship) (candidates map[string][]string, existing []SetConflict) {
	if len(setSongs) == 0 {
		return nil, nil
	}

	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
	}

	candidates = map[string][]string{}
	seenExisting := map[string]bool{}

	for _, ss := range setSongs {
		for _, rel := range relsBySong[ss.SongID] {
			if rel.RelationshipType != RelationshipAvoidPairing {
				continue
			}

			other := rel.SongIDB
			if rel.SongIDA != ss.SongID {
				other = rel.SongIDA
			}

			if inSet[other] {
				a, b := ss.SongID, other
				if a > b {
					a, b = b, a
				}
				key := a + "|" + b
				if !seenExisting[key] {
					seenExisting[key] = true
					existing = append(existing, SetConflict{SongID: a, ConflictsWith: b})
				}
				continue
			}

			candidates[other] = append(candidates[other], ss.SongID)
		}
	}

	return candidates, existing
}
