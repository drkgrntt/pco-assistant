package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TagCategoryTheme/TagCategoryScripture/TagCategoryIdea aren't given any
// special algorithmic meaning - same as song_roles.go's RoleOpener/
// RoleResponse, roles are now just as purely descriptive/filterable as
// tags are, with no Go-side special-casing either way. Tags in particular
// let a song be found later by sermon theme, related scripture, or a loose
// idea. They're seeded into song_tag_categories by
// migration 00016, which is the source of truth for which categories
// actually exist - add a row there for another one, no code change needed.
const (
	TagCategoryTheme     = "theme"
	TagCategoryScripture = "scripture"
	TagCategoryIdea      = "idea"
)

type SongTagCategory struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// ListSongTagCategories returns every selectable category, ordered for
// display in the picker - add a row to song_tag_categories to make a new
// one available, no code change needed. Not organization-scoped: shared
// vocabulary, not any one org's data.
func (s *Store) ListSongTagCategories(ctx context.Context) ([]SongTagCategory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, label, sort_order
		FROM song_tag_categories
		ORDER BY sort_order, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []SongTagCategory{}
	for rows.Next() {
		var t SongTagCategory
		if err := rows.Scan(&t.Code, &t.Label, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

type SongTag struct {
	ID             int64     `json:"id"`
	OrganizationID string    `json:"organization_id"`
	SongID         string    `json:"song_id"`
	Category       string    `json:"category"`
	Value          string    `json:"value"`
	CreatedAt      time.Time `json:"created_at"`
}

// See CreateSongRelationshipParams for why there's no OrganizationID field
// here.
type CreateSongTagParams struct {
	SongID   string `json:"song_id" form:"song_id"`
	Category string `json:"category" form:"category"`
	Value    string `json:"value" form:"value"`
}

func (s *Store) CreateSongTag(ctx context.Context, organizationID string, params CreateSongTagParams) (SongTag, error) {
	value := strings.TrimSpace(params.Value)
	if params.SongID == "" || value == "" {
		return SongTag{}, errors.New("song_id and value are required")
	}

	var tag SongTag
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO song_tags (organization_id, song_id, category, value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id, song_id, category, value) DO UPDATE SET value = EXCLUDED.value
		RETURNING id, organization_id, song_id, category, value, created_at
	`, organizationID, params.SongID, params.Category, value).Scan(&tag.ID, &tag.OrganizationID, &tag.SongID, &tag.Category, &tag.Value, &tag.CreatedAt)
	// category references song_tag_categories (see migration 00016) - a code
	// that isn't a row there fails as a foreign key violation.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongTag{}, fmt.Errorf("unknown category %q", params.Category)
	}
	if err != nil {
		return SongTag{}, err
	}

	return tag, nil
}

// UpdateSongTag overwrites an existing tag's song/category/value in place,
// keyed by id and organizationID (see UpdateSongRelationship's doc comment
// for why both) - CreateSongTag's ON CONFLICT DO UPDATE upserts by
// (organization_id, song_id, category, value), which only coincidentally
// acts like an edit when none of those change; moving a tag to a different
// song, category, or value needs an id-keyed update so it doesn't leave
// the original row behind.
func (s *Store) UpdateSongTag(ctx context.Context, organizationID string, id int64, params CreateSongTagParams) (SongTag, error) {
	value := strings.TrimSpace(params.Value)
	if params.SongID == "" || value == "" {
		return SongTag{}, errors.New("song_id and value are required")
	}

	var tag SongTag
	err := s.db.QueryRowContext(ctx, `
		UPDATE song_tags
		SET song_id = $1, category = $2, value = $3
		WHERE id = $4 AND organization_id = $5
		RETURNING id, organization_id, song_id, category, value, created_at
	`, params.SongID, params.Category, value, id, organizationID).Scan(&tag.ID, &tag.OrganizationID, &tag.SongID, &tag.Category, &tag.Value, &tag.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SongTag{}, fmt.Errorf("that tag no longer exists - it may have been deleted")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return SongTag{}, fmt.Errorf("that song already has that tag")
	}
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return SongTag{}, fmt.Errorf("unknown category %q", params.Category)
	}
	if err != nil {
		return SongTag{}, err
	}

	return tag, nil
}

// ListSongTags filters to organizationID, and by songID and/or category;
// pass "" for either to leave it unfiltered. Used both to show a song's
// tags and, from the set builder, to annotate the whole candidate pool at
// once (see buildSetBuilderData) so searching/filtering by tag there is a
// client-side text match against already-rendered data, the same way role
// filtering already works - not a separate round trip per keystroke.
func (s *Store) ListSongTags(ctx context.Context, organizationID, songID, category string) ([]SongTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, song_id, category, value, created_at
		FROM song_tags
		WHERE organization_id = $1 AND ($2 = '' OR song_id = $2) AND ($3 = '' OR category = $3)
		ORDER BY created_at DESC
	`, organizationID, songID, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []SongTag{}
	for rows.Next() {
		var tag SongTag
		if err := rows.Scan(&tag.ID, &tag.OrganizationID, &tag.SongID, &tag.Category, &tag.Value, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

func (s *Store) DeleteSongTag(ctx context.Context, organizationID string, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM song_tags WHERE id = $1 AND organization_id = $2`, id, organizationID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
