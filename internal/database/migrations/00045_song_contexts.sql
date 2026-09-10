-- +goose Up
-- Song Contexts ("where this song gets sung" - big church / youth / other
-- campus, or whatever an org actually calls its own contexts) - see
-- docs/plans/shipped/song-context-tags-plan.md for the resolved design
-- fork this implements (Option B: a real, org-defined classification axis,
-- song_roles-shaped, not a Service-Type filter).
--
-- Deliberately NOT copying song_role_types' TEXT code shape (migration
-- 00015): that shape fits a small, human-legible, developer-curated
-- vocabulary (opener, communion, ...). song_context_types is per-org and
-- user-created instead - there's no natural string code for a value
-- someone just typed in ("Big church," "Youth," ...), so a surrogate
-- BIGSERIAL id is the join key, with no slugify step and no code-vs-label
-- sync problem on rename.
--
-- Both tables are org-scoped (unlike song_role_types, which is shared
-- vocabulary) - per-org editable, no seeded defaults: an org starts with
-- zero context types and names its own, the same as every other org-owned
-- *value* in this app (tag values, relationship notes) already starts
-- empty.
CREATE TABLE song_context_types (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_context_types_unique UNIQUE (organization_id, label)
);
CREATE INDEX song_context_types_org_idx ON song_context_types (organization_id);

-- ON DELETE CASCADE is deliberate: deleting a context type (e.g. "Youth")
-- also removes every song's assignment to it - the setup page's delete
-- button confirms with wording that says so explicitly (mirrors
-- ArchiveSong's explicit hx-confirm wording, sets_builder.go).
CREATE TABLE song_contexts (
    id BIGSERIAL PRIMARY KEY,
    organization_id TEXT NOT NULL,
    song_id TEXT NOT NULL,
    context_type_id BIGINT NOT NULL REFERENCES song_context_types (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT song_contexts_unique UNIQUE (organization_id, song_id, context_type_id)
);
CREATE INDEX song_contexts_song_id_idx ON song_contexts (song_id);
CREATE INDEX song_contexts_context_type_id_idx ON song_contexts (context_type_id);

-- +goose Down
DROP TABLE song_contexts;
DROP TABLE song_context_types;
