-- Broker schema. Applied on startup by internal/store (embedded).
-- One grant row per (user, app) is what makes Google scopes per-app.

CREATE TABLE IF NOT EXISTS app_registry (
    app_id              TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    allowed_scopes      TEXT[] NOT NULL DEFAULT '{}',
    authentik_client_id TEXT NOT NULL,          -- maps an inbound token's azp/aud -> app_id
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS app_registry_client_id_idx
    ON app_registry (authentik_client_id);

CREATE TABLE IF NOT EXISTS google_grant (
    user_sub            TEXT NOT NULL,
    app_id              TEXT NOT NULL REFERENCES app_registry(app_id) ON DELETE CASCADE,
    granted_scopes      TEXT[] NOT NULL DEFAULT '{}',
    refresh_token_enc   TEXT NOT NULL,          -- AES-256-GCM, base64
    access_token_cache  TEXT,                   -- last minted access token (opaque)
    expires_at          TIMESTAMPTZ,            -- access_token_cache expiry
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_sub, app_id)
);
