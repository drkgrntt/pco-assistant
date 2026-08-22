-- +goose Up
-- Whether a specific song's position/anchor has moved (drag-and-drop)
-- since it was last published - per-row, not a single set-wide flag, so
-- the builder can say exactly which songs moved instead of just "the
-- order changed somewhere." Reordering an already-published song touches
-- neither pco_item_id (still set - the PCO item itself wasn't deleted,
-- just moved) nor pending_delete, so without this the set builder's
-- "Discard changes" button (which keys off exactly those two signals -
-- see UnpublishedCount/PendingDeleteCount in the scheduling controller)
-- never appeared for a pure reorder, even though there was a real local
-- change to discard or publish.
ALTER TABLE set_songs ADD COLUMN moved BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE set_songs DROP COLUMN moved;
