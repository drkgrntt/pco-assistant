-- +goose Up
-- Every table below holds data specific to one Planning Center
-- organization, but nothing scoped a row to the org that created it - any
-- second organization signed into this same instance could read (and, for
-- sets/rosters, publish over) every other org's data. organization_id is
-- the signed-in person's PCO organization id (see pco.GetOrganization,
-- captured at sign-in - migration 00017's organization_name is this same
-- lookup's display-only counterpart), folded into every table's
-- uniqueness constraint: two different orgs having the same relationship
-- between coincidentally same-numbered songs are different facts, not a
-- conflict.
--
-- There's no historical data to backfill (nothing before this tracked
-- which org a row belonged to), so this is only safe to run against an
-- already-emptied database - see cmd/dbreset.
--
-- set_songs/roster_assignments don't get their own organization_id - they
-- only ever exist through their parent sets/rosters row, which is now the
-- thing that's org-scoped, and every lookup already goes through
-- set_id/roster_id rather than querying them directly.

ALTER TABLE sessions ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';

ALTER TABLE song_relationships ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE song_relationships DROP CONSTRAINT song_relationships_unique;
ALTER TABLE song_relationships ADD CONSTRAINT song_relationships_unique
    UNIQUE (organization_id, song_id_a, song_id_b, relationship_type);
CREATE INDEX song_relationships_organization_id_idx ON song_relationships (organization_id);

ALTER TABLE song_person_preferences ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE song_person_preferences DROP CONSTRAINT song_person_preferences_unique;
ALTER TABLE song_person_preferences ADD CONSTRAINT song_person_preferences_unique
    UNIQUE (organization_id, song_id, person_id);
CREATE INDEX song_person_preferences_organization_id_idx ON song_person_preferences (organization_id);

ALTER TABLE song_roles ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE song_roles DROP CONSTRAINT song_roles_unique;
ALTER TABLE song_roles ADD CONSTRAINT song_roles_unique
    UNIQUE (organization_id, song_id, role);
CREATE INDEX song_roles_organization_id_idx ON song_roles (organization_id);

ALTER TABLE song_tags ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE song_tags DROP CONSTRAINT song_tags_unique;
ALTER TABLE song_tags ADD CONSTRAINT song_tags_unique
    UNIQUE (organization_id, song_id, category, value);
CREATE INDEX song_tags_organization_id_idx ON song_tags (organization_id);

ALTER TABLE person_relationships ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE person_relationships DROP CONSTRAINT person_relationships_unique;
ALTER TABLE person_relationships ADD CONSTRAINT person_relationships_unique
    UNIQUE (organization_id, person_id_a, person_id_b, relationship_type);
CREATE INDEX person_relationships_organization_id_idx ON person_relationships (organization_id);

ALTER TABLE sets ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sets DROP CONSTRAINT sets_unique_plan;
ALTER TABLE sets ADD CONSTRAINT sets_unique_plan
    UNIQUE (organization_id, service_type_id, plan_id);
CREATE INDEX sets_organization_id_idx ON sets (organization_id);

ALTER TABLE rosters ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE rosters DROP CONSTRAINT rosters_unique_plan;
ALTER TABLE rosters ADD CONSTRAINT rosters_unique_plan
    UNIQUE (organization_id, service_type_id, plan_id);
CREATE INDEX rosters_organization_id_idx ON rosters (organization_id);

-- +goose Down
ALTER TABLE rosters DROP CONSTRAINT rosters_unique_plan;
ALTER TABLE rosters ADD CONSTRAINT rosters_unique_plan UNIQUE (service_type_id, plan_id);
ALTER TABLE rosters DROP COLUMN organization_id;

ALTER TABLE sets DROP CONSTRAINT sets_unique_plan;
ALTER TABLE sets ADD CONSTRAINT sets_unique_plan UNIQUE (service_type_id, plan_id);
ALTER TABLE sets DROP COLUMN organization_id;

ALTER TABLE person_relationships DROP CONSTRAINT person_relationships_unique;
ALTER TABLE person_relationships ADD CONSTRAINT person_relationships_unique
    UNIQUE (person_id_a, person_id_b, relationship_type);
ALTER TABLE person_relationships DROP COLUMN organization_id;

ALTER TABLE song_tags DROP CONSTRAINT song_tags_unique;
ALTER TABLE song_tags ADD CONSTRAINT song_tags_unique UNIQUE (song_id, category, value);
ALTER TABLE song_tags DROP COLUMN organization_id;

ALTER TABLE song_roles DROP CONSTRAINT song_roles_unique;
ALTER TABLE song_roles ADD CONSTRAINT song_roles_unique UNIQUE (song_id, role);
ALTER TABLE song_roles DROP COLUMN organization_id;

ALTER TABLE song_person_preferences DROP CONSTRAINT song_person_preferences_unique;
ALTER TABLE song_person_preferences ADD CONSTRAINT song_person_preferences_unique
    UNIQUE (song_id, person_id);
ALTER TABLE song_person_preferences DROP COLUMN organization_id;

ALTER TABLE song_relationships DROP CONSTRAINT song_relationships_unique;
ALTER TABLE song_relationships ADD CONSTRAINT song_relationships_unique
    UNIQUE (song_id_a, song_id_b, relationship_type);
ALTER TABLE song_relationships DROP COLUMN organization_id;

ALTER TABLE sessions DROP COLUMN organization_id;
