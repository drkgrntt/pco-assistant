-- +goose Up
-- A "roster" is a draft position-fill plan for one PCO plan - the people
-- equivalent of a "set" (see 00005). One roster per plan (same unique
-- constraint pattern), identified by the same (service_type_id, plan_id)
-- pair.
CREATE TABLE rosters (
    id BIGSERIAL PRIMARY KEY,
    service_type_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rosters_unique_plan UNIQUE (service_type_id, plan_id)
);

-- Unlike set_songs, there's no position/anchor ordering here - filling a
-- named slot has no sequence to it, so this is a flatter table than
-- set_songs. team_id/team_position_name identify which slot an assignment
-- fills (matching PCO's own NeededPosition/PlanPerson shape); pco_plan_
-- person_id/status/pending_delete mirror set_songs' publish-tracking
-- pattern (pco_item_id/pending_delete) for the same reason: a row can exist
-- locally before it's pushed to Planning Center, and a removed-but-already-
-- published row needs to stick around as a tombstone until its PCO record
-- is actually deleted.
CREATE TABLE roster_assignments (
    id BIGSERIAL PRIMARY KEY,
    roster_id BIGINT NOT NULL REFERENCES rosters (id) ON DELETE CASCADE,
    person_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    team_position_name TEXT NOT NULL,
    pco_plan_person_id TEXT,
    status TEXT NOT NULL DEFAULT 'Unconfirmed',
    pending_delete BOOLEAN NOT NULL DEFAULT false,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX roster_assignments_roster_id_idx ON roster_assignments (roster_id);

-- +goose Down
DROP TABLE roster_assignments;
DROP TABLE rosters;
