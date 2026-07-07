# Google Connections broker (`connect.sajitkhadka.com`)

The **Google API access plane**. Owns the single "SajitKhadka Connect" Google OAuth
client, does incremental authorization per app, stores a **separate encrypted refresh
token per (user, app)**, and vends short-lived, scope-limited Google access tokens.

It is itself an **OIDC client of Authentik** (so it always knows the logged-in user).

## Layout

```
cmd/broker/            entrypoint (config -> store.Migrate -> HTTP server)
internal/config        env config
internal/crypto        AES-256-GCM for refresh tokens at rest
internal/store         Postgres (app_registry, google_grant) + embedded migrations
internal/authentik     validates INBOUND subapp access tokens (JWKS)
internal/google        Google OAuth: consent URL / exchange / refresh / revoke
internal/session       HMAC-signed login cookie + signed connect `state`
migrations/            *.sql (embedded, applied on startup)
deploy/                k8s manifests + sealed-secret template
```

## Endpoints

| Method | Path                        | Auth                        | Purpose |
|--------|-----------------------------|-----------------------------|---------|
| GET    | `/healthz`                  | none                        | liveness |
| GET    | `/login` → `/auth/callback` | Authentik (broker's own)    | establish broker session |
| GET    | `/connect/{app_id}/start`   | broker session (cookie)     | redirect to Google consent for that app's scopes |
| GET    | `/oauth/google/callback`    | signed `state`              | store encrypted refresh token for (user, app) |
| POST   | `/token`                    | Bearer = subapp's Authentik access token | return a fresh Google access token |
| GET    | `/connections`              | broker session              | list the user's connected apps |
| DELETE | `/connections/{app_id}`     | broker session              | revoke at Google + delete grant |

**`/token` isolation:** the `app_id` is derived from the *authenticated* token's
`azp`/`aud` client_id (→ `app_registry`), never from the request body — so App A's token
can't vend App B's tokens. The refresh token is never returned.

## Required Authentik configuration

`/token` validates tokens from **any** registered subapp against **one** issuer + JWKS.
That requires Authentik OIDC providers to use a **shared ("global") issuer and signing
key**. Set the providers' *Issuer mode* to **global** and sign them with the **same
signing key**, then point `AUTHENTIK_JWKS_URL` / `AUTHENTIK_TOKEN_ISSUER` at that shared
key set. (If you instead use per-provider issuers/keys, the verifier must be extended to
resolve keys per `azp` — tracked as follow-up.)

## Environment

Non-secret (`deploy/configmap.yaml`): `PORT`, `BASE_URL`, `AUTHENTIK_ISSUER`,
`AUTHENTIK_TOKEN_ISSUER`, `AUTHENTIK_JWKS_URL`.

Secret (`deploy/connect-secrets.template.yaml`, sealed): `DATABASE_URL`, `TOKEN_ENC_KEY`
(base64 32 bytes), `SESSION_SECRET`, `BROKER_OIDC_CLIENT_ID/SECRET`,
`GOOGLE_CLIENT_ID/SECRET`.

## Register an app

Insert a row per subapp (see `deploy/register-app.example.sql`). `allowed_scopes` is the
per-app least-privilege scope set; `authentik_client_id` links the app's Authentik OIDC
client to its `app_id`.

## Build & deploy

```bash
go build ./... && go vet ./...          # local sanity (Go 1.25)

# build + push to the in-cluster registry
docker build -t 192.168.0.120:5000/connect-broker:latest .
docker push 192.168.0.120:5000/connect-broker:latest

# deploy (prod)
kubectl apply -f deploy/namespace.yaml
kubectl apply -f deploy/connect-secrets.sealed.yaml   # sealed, see infra/sealed-secrets
kubectl apply -f deploy/configmap.yaml
kubectl apply -f deploy/deployment.yaml
```

## Google verification constraint

Calendar/Drive/Gmail are **sensitive/restricted** scopes. Keeping the Connect Google app
in **Testing** (add yourself as a test user) needs no verification but **refresh tokens
expire after 7 days** — the broker surfaces this as a "re-consent needed" error on
`/token`/callback, and the user re-runs `/connect/{app}/start`. Publish + verify (or a
CASA assessment for full Gmail) to remove the 7-day limit. Decide per scope.

## Not yet implemented (skeleton TODOs)

- PKCE on the broker's own Authentik login (currently confidential client + state only).
- Per-provider JWKS resolution (see the global-issuer requirement above).
- Incremental scope *delta* re-consent UX (adding a scope currently re-consents the full set).
- Tests (crypto round-trip, state signing, `/token` isolation) and structured request logging.
- A minimal HTML page for `/connections` (currently returns JSON).
