-- +goose Up
-- Each session now carries the PCO OAuth token it was created with, so
-- every PCO API call made during that session can authenticate as the
-- actual signed-in person (see go-pco's WithAccessToken) instead of one
-- shared Personal Access Token regardless of who's logged in - required
-- for a second real PCO account/organization to use this app at all, not
-- just a nicety. Stored recoverable (not hashed, unlike sessions.id)
-- since the raw value is needed for outbound requests - see the
-- migration's accompanying app-side changes for that tradeoff spelled out.
ALTER TABLE sessions
    ADD COLUMN access_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN refresh_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN token_expires_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN access_token,
    DROP COLUMN refresh_token,
    DROP COLUMN token_expires_at;
