-- +goose Up
-- Same shape as song_relationships (see 00001), with one semantic
-- difference the app layer (not this schema) enforces: works_well/avoid_
-- pairing are symmetric and get stored in canonical (person_id_a <
-- person_id_b) order like song relationships, but mentorship is directional
-- (person_id_a = mentor, person_id_b = mentee) and is never reordered - see
-- CreatePersonRelationship.
CREATE TABLE person_relationships (
    id BIGSERIAL PRIMARY KEY,
    person_id_a TEXT NOT NULL,
    person_id_b TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT person_relationships_not_self CHECK (person_id_a <> person_id_b),
    CONSTRAINT person_relationships_unique UNIQUE (person_id_a, person_id_b, relationship_type)
);

CREATE INDEX person_relationships_person_id_a_idx ON person_relationships (person_id_a);
CREATE INDEX person_relationships_person_id_b_idx ON person_relationships (person_id_b);

-- +goose Down
DROP TABLE person_relationships;
