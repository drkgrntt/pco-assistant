-- +goose Up
-- Drops the "Song leader sync setting" table (migration 00038) - the
-- per-service-type pick of which Item Note category the song leader
-- (set_songs.leader_person_id) should sync a "Leading: [name]" line into
-- on Publish/Refresh. That vocal-notes-line sync (syncLeaderNote,
-- leader_note.go/sets_publish.go) has been removed outright now that the
-- leader syncs straight to PCO's own real "Song leader" field
-- (ItemAssignment, syncLeaderAssignment) instead - see
-- docs/plans/shipped/worship-leader-pco-sync-plan.md for why that's the
-- real mechanism, and docs/plans/shipped/song-leader-plan.md's updated
-- Status note for this follow-up removal. The real assignment is
-- strictly better (it's what actually drives PCO's own notifications),
-- so keeping the note-line sync alongside it meant a setting that only
-- added setup friction with no remaining benefit.
DROP TABLE service_type_vocal_note_categories;

-- +goose Down
CREATE TABLE service_type_vocal_note_categories (
    organization_id TEXT NOT NULL,
    service_type_id TEXT NOT NULL,
    category_id     TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, service_type_id)
);
