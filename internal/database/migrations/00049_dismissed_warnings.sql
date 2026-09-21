-- +goose Up
-- Dismissable Build-page warnings (docs/plans/shipped/
-- dismissable-warnings-plan.md) - a director can dismiss an avoid-pairing/
-- recently-repeated/vocal-range-mismatch warning they've already seen and
-- decided isn't worth acting on, instead of it re-flagging loud on every
-- render. Scope of dismissal is kind-dependent, all expressed through the
-- same (warning_kind, subject_key) pair rather than one table per kind:
-- avoid-pairing dismissals are permanent (subject_key is just the
-- canonical, order-independent song-pair - see the Store's
-- avoidPairingSubjectKey), while recently-repeated/vocal-range-mismatch
-- dismissals are scoped to one occurrence (subject_key folds in the plan
-- id, so a genuinely new occurrence on a future plan still shows loud
-- again). No FK to any local or PCO table - same "PCO/this app's own
-- scheduling tables are the source of truth, this is just a pointer at
-- them by id" convention song_relationships/song_transition_notes already
-- follow - so a dismissal row can outlive the thing it refers to (e.g. the
-- underlying avoid_pairing relationship gets deleted later); that's left
-- as harmless clutter rather than cleaned up, per that plan doc's own
-- resolved decision.
CREATE TABLE dismissed_warnings (
    id                     BIGSERIAL PRIMARY KEY,
    organization_id        TEXT NOT NULL,
    warning_kind           TEXT NOT NULL,
    subject_key            TEXT NOT NULL,
    dismissed_by_person_id TEXT NOT NULL,
    dismissed_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT dismissed_warnings_unique UNIQUE (organization_id, warning_kind, subject_key)
);

CREATE INDEX dismissed_warnings_lookup_idx
    ON dismissed_warnings (organization_id, warning_kind);

-- +goose Down
DROP TABLE dismissed_warnings;
