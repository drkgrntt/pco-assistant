-- +goose Up
-- Purely additive - song_role_types is already a DB-driven lookup table
-- (see migration 00015), so this alone is enough for these to become
-- selectable/assignable from the Song Roles setup page and the build
-- page's quick-add panel. Making them actually show up (badges, filter
-- buttons) in the set builder is a Go/templ-side change, not this
-- migration - see internal/server/controllers/sets.go.
INSERT INTO song_role_types (code, label, sort_order) VALUES
    ('communion', 'Communion', 3),
    ('prayer', 'Prayer', 4),
    ('reflection', 'Reflection', 5),
    ('baptism', 'Baptism', 6);

-- +goose Down
DELETE FROM song_role_types WHERE code IN ('communion', 'prayer', 'reflection', 'baptism');
