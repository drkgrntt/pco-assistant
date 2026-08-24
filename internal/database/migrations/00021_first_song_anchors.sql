-- +goose Up
-- Remembers, per organization + service type, which plan item title the
-- first song usually gets placed after (see AddSongToSet's
-- fallbackAnchorItemID, and the write hook in buildSetBuilderData) - a PCO
-- item id is only good for one plan, but the same template's header/
-- section titles recur across every plan of a given service type. '' is a
-- valid, meaningful value ("top of service" *is* the remembered position),
-- distinct from no row existing at all (nothing remembered yet).
CREATE TABLE service_type_first_song_anchors (
    organization_id   TEXT NOT NULL,
    service_type_id   TEXT NOT NULL,
    anchor_item_title TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, service_type_id)
);

-- +goose Down
DROP TABLE service_type_first_song_anchors;
