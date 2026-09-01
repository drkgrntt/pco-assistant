-- +goose Up
-- A purely local, at-a-glance note per plan ("the one with the guest
-- speaker") - see ideas.md/issues.md for the framing. Deliberately its own
-- table, not a column on sets: a sets row only exists once someone's
-- actually started a draft for that plan, but the whole point of this note
-- is spotting a plan *before* ever opening it (in the Build page's plan
-- picker list), so it has to exist independently of whether a draft does.
-- Same (organization_id, service_type_id, plan_id) identity as sets/
-- rosters (00005/00013), and the same flat, no-children shape as
-- service_type_first_song_anchors (00021), so a composite primary key
-- fits better here than sets/rosters' own BIGSERIAL id + unique
-- constraint (which only exists there to give set_songs/roster_assignments
-- something to foreign-key against).
CREATE TABLE plan_notes (
    organization_id TEXT NOT NULL,
    service_type_id TEXT NOT NULL,
    plan_id         TEXT NOT NULL,
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, service_type_id, plan_id)
);

-- +goose Down
DROP TABLE plan_notes;
