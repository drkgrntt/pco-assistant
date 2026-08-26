-- +goose Up
-- A row's mere existence is the signal: this song was first observed with
-- no Planning Center scheduling history at all (see TrackNewSong,
-- PublishedSongAppearanceCounts) - i.e. genuinely new to the church, not
-- just new to this app. Once marked, this org's local publish count for
-- the song (from set_songs/sets) is a complete, accurate record of how
-- many times it's actually been sung, since there's no pre-existing
-- history it could have missed. A song that already had a scheduling date
-- the first time this app ever saw it never gets a row here, permanently -
-- it's not new, no matter what its local count later becomes.
CREATE TABLE tracked_new_songs (
    organization_id TEXT NOT NULL,
    song_id         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, song_id)
);

-- +goose Down
DROP TABLE tracked_new_songs;
