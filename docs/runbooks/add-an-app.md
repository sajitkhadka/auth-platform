# Runbook — Add an app to the platform

How to onboard a new subapp. Two independent parts:
- **Part 1 — SSO** (every app): register it as an OIDC client of Authentik.
- **Part 2 — Google API access** (only if the app calls Google APIs): register it with the
  broker and connect a Google grant.

This is the operational companion to the integration *contract* in
[`../integration.md`](../integration.md). Prereqs: `kubectl` access to the cluster and an
Authentik **admin API token** (Directory → Tokens and App passwords → Create, owner `akadmin`,
intent API — make it **non-expiring** for automation).

Set up shell vars:
```bash
AK=https://auth.sajitkhadka.com/api/v3
H="Authorization: Bearer <ADMIN_API_TOKEN>"
APP=myapp                       # your app slug / id
REDIRECT=https://myapp.sajitkhadka.com/auth/callback
```

---

## Part 1 — Register as an SSO (OIDC) client

You can do this in the Authentik UI (**Applications → Providers → Create → OAuth2/OpenID**,
then **Applications → Create**) or via the API. The API path is below.

### 1a. Look up the flow / key / scope IDs (once)

```bash
curl -sS -H "$H" "$AK/flows/instances/?designation=authorization"   # pick implicit- or explicit-consent
curl -sS -H "$H" "$AK/flows/instances/?designation=invalidation"    # pick default-provider-invalidation-flow
curl -sS -H "$H" "$AK/crypto/certificatekeypairs/"                  # a signing key with a private key
curl -sS -H "$H" "$AK/propertymappings/provider/scope/"            # openid / email / profile pks
```

### 1b. Create the provider

> **Gotcha (Authentik 2026.x):** you MUST set `grant_types`. A provider created via the API
> with an empty `grant_types` makes the authorize endpoint reject every request with
> `invalid_request` / "The request is otherwise malformed" (logged as "Invalid grant_type for
> provider"). Include `authorization_code` (+ `refresh_token`).

```bash
curl -sS -X POST -H "$H" -H "Content-Type: application/json" -d '{
  "name": "'"$APP"'",
  "authorization_flow": "<authorization_flow_pk>",
  "invalidation_flow": "<invalidation_flow_pk>",
  "signing_key": "<signing_key_pk>",
  "client_type": "confidential",
  "grant_types": ["authorization_code", "refresh_token"],
  "redirect_uris": [{"matching_mode":"strict","url":"'"$REDIRECT"'"}],
  "issuer_mode": "per_provider",
  "sub_mode": "hashed_user_id",
  "include_claims_in_id_token": true,
  "property_mappings": ["<openid_pk>","<email_pk>","<profile_pk>"]
}' "$AK/providers/oauth2/"
# -> note the returned "client_id" and "client_secret" and provider "pk"
```
For a public client (SPA/native) use `"client_type": "public"` and require PKCE.

### 1c. Create the application (its slug becomes the issuer path)

```bash
curl -sS -X POST -H "$H" -H "Content-Type: application/json" -d '{
  "name": "My App", "slug": "'"$APP"'", "provider": <provider_pk>
}' "$AK/core/applications/"
```

### 1d. Wire up the app

- OIDC endpoints (discovery): `https://auth.sajitkhadka.com/application/o/<APP>/.well-known/openid-configuration`
- Validate the `id_token`: `iss = https://auth.sajitkhadka.com/application/o/<APP>/`,
  `aud = client_id`. Use `sub` as the stable user id; JIT-provision on first login.
- The app holds its own session after the code exchange.

### 1e. Verify

```bash
# authorize must NOT return error=invalid_request:
curl -sS -o /dev/null -w "%{redirect_url}\n" \
  "https://auth.sajitkhadka.com/application/o/authorize/?client_id=<client_id>&redirect_uri=$REDIRECT&response_type=code&scope=openid+email+profile&state=diag"
```
Then log in through the app in a browser; a second app login should be silent (SSO).

---

## Part 2 — Give the app Google API access (optional)

Only for apps that call Google APIs. The app must already have Part 1 done (it authenticates
`/token` calls with its Authentik access token).

### 2a. Register the app in the broker

Insert a row in the broker's `app_registry` (least-privilege scopes; `authentik_client_id`
links the inbound token's `azp`/`aud` to this `app_id`). Run against the `connect` DB:

```bash
kubectl run pg-admin --rm -it --restart=Never --image=postgres:16-alpine -- \
  psql "postgres://connect:<CONNECT_DB_PW>@192.168.0.120:5432/connect?sslmode=disable" -c "
    INSERT INTO app_registry (app_id, name, allowed_scopes, authentik_client_id)
    VALUES ('$APP','My App',
            ARRAY['https://www.googleapis.com/auth/calendar.readonly'],
            '<client_id_from_1b>')
    ON CONFLICT (app_id) DO UPDATE SET
      name=EXCLUDED.name, allowed_scopes=EXCLUDED.allowed_scopes,
      authentik_client_id=EXCLUDED.authentik_client_id;"
```
See `broker/deploy/register-app.example.sql`.

### 2b. Ensure the scope is consentable

The scope must be a Google **sensitive/restricted** scope your Connect Google client is
allowed to request. In Testing mode, add yourself as a test user (refresh tokens then expire
after 7 days — the broker surfaces this as "re-consent needed").

### 2c. Connect a grant (once per user, in a browser)

Send the user to:
```
https://connect.sajitkhadka.com/connect/<APP>/start?return=<url-to-return-to>
```
They consent to *your* scopes; the broker stores an AES-GCM-encrypted refresh token for
`(user, app)`.

### 2d. Use it from the app

```
POST https://connect.sajitkhadka.com/token
Authorization: Bearer <the user's Authentik access token for YOUR app>

-> { "access_token": "ya29…", "expires_at": "…", "scopes": ["…"] }
```
Use `access_token` against the Google API. Handle:
- **404 "not connected"** → send the user back through 2c.
- **502 "re-consent needed"** → refresh token expired (Testing mode) → 2c again.

Disconnect: `DELETE https://connect.sajitkhadka.com/connections/<APP>` (revokes at Google +
deletes the grant).

### 2e. Verify

```bash
curl -sS -o /dev/null -w "no-bearer=%{http_code}\n" -X POST https://connect.sajitkhadka.com/token   # 401
# with a valid app token but no grant yet -> 404 (prompt connect)
```

---

## Notes

- **Adding a scope later** to an app's `allowed_scopes` triggers a fresh consent for that
  app's delta on the next `/connect`.
- **Issuer mode:** new apps currently use per-provider issuer. If/when the broker validates
  `/token` for multiple apps, see [ADR-0003](../adr/0003-per-provider-issuer-mode.md).
- **Reproducibility:** these UI/API steps can later be codified as Authentik **blueprints**
  (YAML) so app onboarding is declarative rather than click-ops.
