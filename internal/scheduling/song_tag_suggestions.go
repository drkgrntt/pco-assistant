package scheduling

import "context"

// IgnoreSongTagSuggestion permanently dismisses word from this org's
// future song-tag suggestions (see ListIgnoredSongTagSuggestionWords, and
// the suggestion algorithm in the controllers package that reads it) -
// idempotent, so ignoring an already-ignored word is a harmless no-op
// rather than an error.
func (s *Store) IgnoreSongTagSuggestion(ctx context.Context, organizationID, word string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ignored_song_tag_suggestions (organization_id, word)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, word) DO NOTHING
	`, organizationID, word)
	return err
}

// ListIgnoredSongTagSuggestionWords returns every word this org has
// dismissed, as a lookup set - the suggestion algorithm skips any group
// whose shared word is in this set.
func (s *Store) ListIgnoredSongTagSuggestionWords(ctx context.Context, organizationID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT word FROM ignored_song_tag_suggestions WHERE organization_id = $1
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	words := map[string]bool{}
	for rows.Next() {
		var word string
		if err := rows.Scan(&word); err != nil {
			return nil, err
		}
		words[word] = true
	}

	return words, rows.Err()
}
