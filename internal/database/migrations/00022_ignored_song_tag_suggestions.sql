-- +goose Up
-- Permanent per-org "don't suggest this word again" list - see
-- internal/scheduling/song_tag_suggestions.go and the Song Tag
-- Suggestions page (shared-title-word tag discovery). No un-ignore UI for
-- now, same as every other dismiss flow in this app.
CREATE TABLE ignored_song_tag_suggestions (
    organization_id TEXT NOT NULL,
    word            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, word)
);

-- +goose Down
DROP TABLE ignored_song_tag_suggestions;
