# Subapp integration contract

How any `*.sajitkhadka.com` subapp joins the central login. Two planes, integrated
independently: **identity** (Authentik SSO) and, only if needed, **Google API access**
(the Connections broker).

## 1. Register the app in Authentik

In the Authentik admin (`https://auth.sajitkhadka.com/if/admin/`):

1. Create an **OIDC Provider** — confidential client for server-side apps; public client
   **with PKCE required** for SPAs/native.
2. Create an **Application** bound to that provider. Note the `client_id` / `client_secret`.
3. Set redirect URI(s) to the app's callback, e.g. `https://<app>.sajitkhadka.com/auth/callback`.
4. **Issuer mode: global**, signed with the shared signing key (required so the broker can
   validate tokens from every app against one JWKS — see the broker README).

## 2. Log in (OIDC Authorization Code + PKCE)

- Authorization endpoint: `https://auth.sajitkhadka.com/application/o/authorize/`
- Token endpoint: `https://auth.sajitkhadka.com/application/o/token/`
- Discovery: `https://auth.sajitkhadka.com/application/o/<app-slug>/.well-known/openid-configuration`

Validate the `id_token`:
- `iss` = the configured (global) issuer.
- `aud` = your `client_id`.
- Use **`sub`** (stable Authentik UUID) as the user id. **JIT-provision** a local user on
  first login (store `email`, `name`, `picture`).

Hold your **own** post-exchange session; refresh via the Authentik refresh token or silent
re-auth. Do **not** rely on a shared `.sajitkhadka.com` cookie — SSO is front-channel
redirect (a second app login reuses the Authentik session and returns a code silently).

## 3. Google API access (only apps that need it)

1. **Register** the app in the broker `app_registry` with its least-privilege
   `allowed_scopes` and its `authentik_client_id` (see `broker/deploy/register-app.example.sql`).
2. **Connect once:** send the user to
   `https://connect.sajitkhadka.com/connect/{app_id}/start?return=<url>`. They consent to
   *your* scopes; the broker stores an encrypted refresh token for `(user, app)`.
3. **Get tokens:** call `POST https://connect.sajitkhadka.com/token` with
   `Authorization: Bearer <the user's Authentik access token for your app>`. Response:
   ```json
   { "access_token": "ya29...", "expires_at": "2026-07-07T12:00:00Z", "scopes": ["..."] }
   ```
   Use `access_token` against the Google API. The broker refreshes under the hood and
   **never** returns a refresh token.
4. **Handle "not connected"** (`/token` → 404): prompt the user back through step 2.
   Handle "re-consent needed" (Google Testing-mode 7-day refresh expiry) the same way.
5. **Disconnect:** `DELETE https://connect.sajitkhadka.com/connections/{app_id}` (revokes at
   Google + deletes the grant).

### WebSocket / non-browser identity note

Browsers can't set custom headers on WS upgrades. Carry identity in an **HttpOnly+Secure
cookie** or a short-lived connect ticket rather than an `Authorization` header (this is the
approach sync-todo's `ws.go` will use when it migrates).

## Reference consumers (follow-up migrations)

- **sajitkhadka.com (`D:\projects\me`)** — swap the NextAuth `Google` provider for a generic
  OIDC provider pointing at Authentik. Its service-account Drive flow is unrelated and stays.
- **sync-todo** — replace `requestUserID` (`backend/internal/handlers/auth.go`) with Authentik
  JWT validation (JWKS, `sub` → `User.ID`, JIT-provision via `store.Repository`); frontend
  swaps the user-picker for an OIDC client; `ws.go` uses the cookie/ticket approach above.
