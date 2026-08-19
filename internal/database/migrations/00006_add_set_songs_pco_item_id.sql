-- +goose Up
-- Set once a set song has been pushed to Planning Center as a real plan
-- item, so a repeat "Publish" doesn't create duplicates. NULL means "not
-- published yet."
ALTER TABLE set_songs ADD COLUMN pco_item_id TEXT;

-- +goose Down
ALTER TABLE set_songs DROP COLUMN pco_item_id;
