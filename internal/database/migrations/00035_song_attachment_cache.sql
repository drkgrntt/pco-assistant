-- +goose Up
-- Caches a song's Planning Center Attachments (lead sheets/chord charts/
-- scans/audio - arbitrary uploaded files, see pco.GetAttachments) as
-- reference links on the Build page - see
-- docs/plans/not-started/song-lead-sheet-attachments-plan.md. A separate table
-- from song_arrangement_cache (00023), not new columns on it, since that
-- table is one-row-per-song and a song can have zero, one, or many
-- attachments.
--
-- Deliberately does NOT cache the attachment's own download url - go-pco's
-- AttachmentAttributes.URL is not confirmed (as of this migration) to be a
-- long-lived link rather than an expiring pre-signed one, so this app never
-- persists it: RefreshSongAttachmentURL (song_attachment_cache.go)
-- live-fetches a fresh url from Planning Center on the actual click instead.
-- Replace-all per song on every (re)sync (UpsertSongAttachments), same
-- "always overwrite" convention as song_arrangement_cache, so an attachment
-- removed in Planning Center doesn't linger here.
CREATE TABLE song_attachment_cache (
    organization_id  TEXT NOT NULL,
    song_id          TEXT NOT NULL,
    pco_attachment_id TEXT NOT NULL,
    filename         TEXT NOT NULL DEFAULT '',
    content_type     TEXT NOT NULL DEFAULT '',
    cached_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, song_id, pco_attachment_id)
);

CREATE INDEX song_attachment_cache_org_song_idx ON song_attachment_cache (organization_id, song_id);

-- +goose Down
DROP TABLE song_attachment_cache;
