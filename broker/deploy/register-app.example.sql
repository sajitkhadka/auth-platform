-- Register a subapp in the broker's app_registry so it can request Google scopes.
-- Run against the broker DB (connect / connect_dev). app_id is your stable slug;
-- authentik_client_id must match the OIDC client_id Authentik issues that app
-- (the broker maps an inbound token's azp/aud -> app_id via this column).

INSERT INTO app_registry (app_id, name, allowed_scopes, authentik_client_id)
VALUES (
  'sync-todo',
  'Sync Todo',
  ARRAY[
    'https://www.googleapis.com/auth/calendar.readonly'
  ],
  'REPLACE_authentik_client_id_for_sync_todo'
)
ON CONFLICT (app_id) DO UPDATE
  SET name = EXCLUDED.name,
      allowed_scopes = EXCLUDED.allowed_scopes,
      authentik_client_id = EXCLUDED.authentik_client_id;
