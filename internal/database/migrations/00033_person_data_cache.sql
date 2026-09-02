-- +goose Up
-- Local caches for three per-person Planning Center reads that used to run
-- live on every Team Builder render (see rosters_warnings.go/
-- rosters_builder.go and pco-api-risks.md's addendum on this): blockouts,
-- cross-team plan-people history (all teams, for crossTeamAssignmentsThisWeek),
-- and per-team plan-people history (for otherAssignmentDates). Same
-- "sync-marker row + zero-or-more detail rows" shape as
-- song_arrangement_cache (00023), not that table's own "a row's mere
-- existence means checked" shortcut - these are list-valued (zero blockouts
-- and never-synced both look like zero rows otherwise), so each dataset
-- gets its own dedicated sync-marker table recording when it was last
-- checked, independent of whether anything was actually found.
--
-- Deliberately two separate tables/sync passes for plan-people history
-- (person_plan_history for crossTeamAssignmentsThisWeek's all-teams call,
-- person_team_plan_history for otherAssignmentDates' per-team call) rather
-- than one merged table filtered two ways - plan.md's own design-fork
-- section proposed the merge, contingent on live-verifying PCO's
-- GetPersonPlanPeople PerPage ceiling for multi-team volunteers, which
-- could not be confirmed this session (see the pco-api-risks.md addendum
-- for this change). Falling back to the conservative two-table design
-- rather than risking warning accuracy to save one table.
CREATE TABLE person_blockout_sync (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id)
);

CREATE TABLE person_blockouts (
    organization_id  TEXT NOT NULL,
    person_id        TEXT NOT NULL,
    pco_blockout_id  TEXT NOT NULL,
    starts_at        TIMESTAMPTZ NOT NULL,
    ends_at          TIMESTAMPTZ NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    repeat_frequency TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (organization_id, person_id, pco_blockout_id)
);

-- All-teams plan-people history, backing crossTeamAssignmentsThisWeek. Also
-- carries team_name (not in plan.md's original proposed schema) - resolved
-- once per sync from the same GetPersonPlanPeople response's "team" include
-- and stored alongside team_id, so the read path can render the same
-- "prefer the resolved team name, fall back to the position name" label
-- crossTeamAssignmentsThisWeek always has without needing a live PCO
-- lookup (or a second cached table) just to resolve a team id to a name.
CREATE TABLE person_plan_history_sync (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id)
);

CREATE TABLE person_plan_history (
    organization_id    TEXT NOT NULL,
    person_id          TEXT NOT NULL,
    pco_plan_person_id TEXT NOT NULL,
    team_id            TEXT NOT NULL DEFAULT '',
    team_name          TEXT NOT NULL DEFAULT '',
    team_position_name TEXT NOT NULL DEFAULT '',
    plan_id            TEXT NOT NULL,
    sort_date          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, person_id, pco_plan_person_id)
);

-- Per-(person, team) plan-people history, backing otherAssignmentDates -
-- the team-filtered counterpart to person_plan_history above. Keyed
-- additionally by team_id in both the sync marker and the detail table, so
-- a person active on several teams gets one independent sync pass (and TTL)
-- per team rather than sharing one all-teams pass that could dilute any
-- single team's own 25-row PCO page budget (the exact risk the merged
-- design would have risked - see this migration's header comment).
CREATE TABLE person_team_plan_history_sync (
    organization_id TEXT NOT NULL,
    person_id       TEXT NOT NULL,
    team_id         TEXT NOT NULL,
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, person_id, team_id)
);

CREATE TABLE person_team_plan_history (
    organization_id    TEXT NOT NULL,
    person_id          TEXT NOT NULL,
    team_id            TEXT NOT NULL,
    pco_plan_person_id TEXT NOT NULL,
    team_position_name TEXT NOT NULL DEFAULT '',
    plan_id            TEXT NOT NULL,
    sort_date          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, person_id, team_id, pco_plan_person_id)
);

-- +goose Down
DROP TABLE person_team_plan_history;
DROP TABLE person_team_plan_history_sync;
DROP TABLE person_plan_history;
DROP TABLE person_plan_history_sync;
DROP TABLE person_blockouts;
DROP TABLE person_blockout_sync;
