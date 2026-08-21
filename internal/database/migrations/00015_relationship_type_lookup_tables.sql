-- +goose Up
-- Turns the relationship_type/role/preference columns from free text
-- validated in Go (see the valid* maps this migration's Go-side companion
-- change removes) into foreign keys against small lookup tables. The point
-- is purely operational: adding a new song relationship type, person
-- relationship type, song role, or song preference no longer needs a code
-- change/deploy - just an INSERT into the relevant table below. Business
-- logic that gives specific codes special meaning (avoid_pairing conflict
-- detection, flows_well/same_theme candidate scoring, opener/response
-- candidate filtering) still lives in Go and only recognizes those
-- well-known codes - a newly added row is a valid, selectable, displayable
-- tag from day one, but only picks up algorithmic behavior if the Go code
-- is later taught to look for it.
CREATE TABLE song_relationship_types (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO song_relationship_types (code, label, sort_order) VALUES
    ('flows_well', 'Flows well', 1),
    ('same_theme', 'Same theme', 2),
    ('avoid_pairing', 'Avoid pairing', 3);

ALTER TABLE song_relationships
    ADD CONSTRAINT song_relationships_relationship_type_fkey
    FOREIGN KEY (relationship_type) REFERENCES song_relationship_types (code);

-- person_relationship_types carries a "directional" flag - see
-- CreatePersonRelationship/UpdatePersonRelationship, which query it to
-- decide whether a pair gets canonically reordered (symmetric types, so
-- entering A/B or B/A dedupes to the same row) or stored exactly as
-- entered (directional types, e.g. mentorship, where A/B and B/A are
-- different facts).
CREATE TABLE person_relationship_types (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    directional BOOLEAN NOT NULL DEFAULT false,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO person_relationship_types (code, label, directional, sort_order) VALUES
    ('works_well', 'Works well with', false, 1),
    ('mentorship', 'Mentorship (A mentors B)', true, 2),
    ('avoid_pairing', 'Doesn''t work well with', false, 3);

ALTER TABLE person_relationships
    ADD CONSTRAINT person_relationships_relationship_type_fkey
    FOREIGN KEY (relationship_type) REFERENCES person_relationship_types (code);

CREATE TABLE song_role_types (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO song_role_types (code, label, sort_order) VALUES
    ('opener', 'Opener', 1),
    ('response', 'Response', 2);

ALTER TABLE song_roles
    ADD CONSTRAINT song_roles_role_fkey
    FOREIGN KEY (role) REFERENCES song_role_types (code);

CREATE TABLE song_preference_types (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO song_preference_types (code, label, sort_order) VALUES
    ('preferred', 'Preferred', 1),
    ('discouraged', 'Discouraged', 2);

-- Superseded by the FK above - a CHECK and a FK both restricting the same
-- column would just be redundant, and the CHECK can't see rows added to
-- song_preference_types later anyway.
ALTER TABLE song_person_preferences
    DROP CONSTRAINT song_person_preferences_preference_check;

ALTER TABLE song_person_preferences
    ADD CONSTRAINT song_person_preferences_preference_fkey
    FOREIGN KEY (preference) REFERENCES song_preference_types (code);

-- +goose Down
ALTER TABLE song_person_preferences DROP CONSTRAINT song_person_preferences_preference_fkey;
ALTER TABLE song_person_preferences
    ADD CONSTRAINT song_person_preferences_preference_check
    CHECK (preference IN ('preferred', 'discouraged'));
DROP TABLE song_preference_types;

ALTER TABLE song_roles DROP CONSTRAINT song_roles_role_fkey;
DROP TABLE song_role_types;

ALTER TABLE person_relationships DROP CONSTRAINT person_relationships_relationship_type_fkey;
DROP TABLE person_relationship_types;

ALTER TABLE song_relationships DROP CONSTRAINT song_relationships_relationship_type_fkey;
DROP TABLE song_relationship_types;
