-- +goose Up
-- Where a song sits relative to the plan's other (non-song, or
-- not-locally-tracked) items - the PCO item id it should be placed
-- immediately after, or '' for "top of service, before everything else."
-- Lets a song be anchored to a specific point in a pre-built template
-- (e.g. right after a "Welcome" header) instead of always landing at the
-- end of the plan. See buildServiceOrder in the scheduling controller for
-- how this merges with the plan's real items into one displayed order.
ALTER TABLE set_songs ADD COLUMN anchor_item_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE set_songs DROP COLUMN anchor_item_id;
