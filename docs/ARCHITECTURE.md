# auth-platform — Architecture

Detailed architecture of the centralized-login platform: the two planes, every
deployed service, the request flows, and the supporting infrastructure. For the original
design rationale see [`DESIGN.md`](DESIGN.md); for onboarding a new app see
[`runbooks/add-an-app.md`](runbooks/add-an-app.md); for decisions see [`adr/`](adr/).

## 1. Overview

The platform separates two concerns that are often conflated:

- **Authentication (identity)** — *who is the user*. Handled by **Authentik**, which federates
  to Google and issues OIDC tokens to every subapp. One login, reused across apps (SSO).
- **Authorization for Google APIs** — *which Google scopes may an app use on the user's
  behalf*. Handled by the **Connections broker**, which holds one Google OAuth client and
  vends short-lived, scope-limited Google access tokens per `(user, app)`.

```
                         ┌────────────────────────────────┐
   Google (identity) ◄───┤  Authentik   auth.sajitkhadka.com│
   openid email profile  │  - OAuth Source federates Google │
                         │  - OIDC provider per subapp       │
                         └───────────────┬──────────────────┘
                                         │ id_token / access_token (who you are)
                    ┌────────────────────┼─────────────────────┐
                    ▼                    ▼                     ▼
              subapp A              Connect broker         (future subapps)
              (OIDC client)         (OIDC client +          (OIDC clients)
                                     Google broker)
                                          │
                                          │ "give me a Google token for MY scopes"
                                          ▼
                             Google APIs (Calendar/Drive/…)
```

Both planes run on an existing single-node **k3s** cluster (192.168.0.120) and
reuse cluster services already present there (cert-manager, ingress-nginx, host Postgres,
a local image registry).

| Host | Service | Namespace |
|------|---------|-----------|
| `auth.sajitkhadka.com`    | Authentik (SSO IdP)          | `authentik` |
| `connect.sajitkhadka.com` | Connections broker (Go)      | `connect`   |

## 2. Infrastructure

Single-node k3s on the LAN host (192.168.0.120), with `:80/:443` reachable from the
internet via the edge router; DNS on Cloudflare.

Reused, already-present cluster components:
- **ingress-nginx** — single ingress controller, terminates TLS on `:443`.
- **cert-manager** — ACME certificates. Existing `letsencrypt-prod` ClusterIssuer (HTTP-01).
- **Host Postgres** — runs on the host (not in k8s): prod `:5432`, dev `:5433`. Reached over
  the LAN with `sslmode=disable`, one DB+role per app.
- **Local image registry** `192.168.0.120:5000` — plain HTTP; node containerd already trusts it.

Added for auth-platform:
- **sealed-secrets** controller (`kube-system`, Bitnami v0.38.4) — decrypts committed
  `SealedSecret` CRs into real `Secret`s. See [ADR-0005](adr/0005-sealed-secrets.md).
- **DNS-01 ClusterIssuer** (`letsencrypt-dns01`) — *templated but not yet applied*; TLS
  currently uses the existing HTTP-01 issuer. See [ADR-0004](adr/0004-tls-http01-now-dns01-later.md).

## 3. Component: Authentik (authentication plane)

Deployed via the official Helm chart (`authentik/authentik` **2026.5.3**) into namespace
`authentik`.

### 3.1 Runtime topology

| Workload | Purpose |
|----------|---------|
| `authentik-server` (Deployment) | HTTP/OIDC API + web UI; runs DB migrations on boot |
| `authentik-worker` (Deployment) | background tasks (outposts, events, email) |
| `authentik-redis`  (Deployment) | cache + task broker — **we deploy this ourselves** |

This chart version bundles **neither Postgres nor Redis**. Postgres is the external host
instance (`authentik` DB on `:5432`); Redis is a small in-namespace `redis:7-alpine`
Deployment+Service (`authentik/redis.yaml`), no persistence (Authentik only uses it as an
ephemeral cache/broker).

### 3.2 Configuration & secrets injection

The chart flattens the Helm `authentik:` map into a generated `Secret` whose keys are env
vars (`authentik.postgresql.host` → `AUTHENTIK_POSTGRESQL__HOST`, …); **empty values are
skipped**. The server/worker Deployments do `envFrom: secretRef(<that secret>)` and then
append `global.envFrom`.

We put **non-secret** config in Helm values (`values-common.yaml` + `values-prod.yaml`):
DB host/port/name/user, `redis.host`, ingress. The **two real secrets** (the Authentik
secret key and the DB password) live in a sealed `authentik-secrets` Secret injected via
`global.envFrom` — appended last, so it fills in the keys the chart left empty. No value
lives in git in plaintext.

Keys in `authentik-secrets`: `AUTHENTIK_SECRET_KEY`, `AUTHENTIK_POSTGRESQL__PASSWORD`.

### 3.3 Identity federation & providers

- **Google OAuth Source** (configured in the UI) — federates to Google for `openid email
  profile` only, using the dedicated `auth.sajitkhadka.com` Google client. Callback:
  `https://auth.sajitkhadka.com/source/oauth/callback/google/`. The source is attached to the
  `default-authentication-identification` stage's `sources` list (otherwise no Google button
  renders on the login page).
- **Per-subapp OIDC Provider + Application** — each consumer gets an OAuth2/OpenID provider
  (confidential or public+PKCE) and an Application (whose `slug` becomes the per-provider
  issuer path). Providers **must** have `grant_types` including `authorization_code`; the
  API leaves it empty by default. Authentik `sub` (stable UUID) is the canonical user id.

### 3.4 Ingress / TLS

Ingress `authentik-server` on `auth.sajitkhadka.com`, `ingressClassName: nginx`, annotation
`cert-manager.io/cluster-issuer: letsencrypt-prod` (HTTP-01), TLS secret `authentik-tls`.

## 4. Component: Connections broker (Google API access plane)

A small **Go** service (`broker/`), built as a distroless static image with `ko` and pushed
to `192.168.0.120:5000/connect-broker`. Deployed to namespace `connect`. It is itself an
**OIDC client of Authentik** (Authentik application slug `connect`) so it always knows the
current user, and it owns the single "SajitKhadka Connect" Google OAuth client.

### 4.1 Package layout

```
cmd/broker/main.go        entrypoint: config -> store.Migrate -> HTTP server + graceful shutdown
internal/config           env config (required-var validation)
internal/crypto           AES-256-GCM encrypt/decrypt for refresh tokens at rest
internal/store            Postgres (pgx) — app_registry, google_grant + embedded migrations
internal/authentik        validates INBOUND subapp access tokens via JWKS (go-oidc)
internal/google           Google OAuth: consent URL / code exchange / refresh / revoke (x/oauth2)
internal/session          HMAC-signed login cookie + signed connect `state` (CSRF + return URL)
internal/server           HTTP routing + handlers, wires everything together
migrations/               *.sql, embedded (go:embed) and applied on startup
```

### 4.2 Data model (Postgres `connect` DB)

```
app_registry(app_id PK, name, allowed_scopes[], authentik_client_id UNIQUE, created_at)
google_grant(user_sub, app_id -> app_registry, granted_scopes[], refresh_token_enc,
             access_token_cache, expires_at, updated_at, PRIMARY KEY(user_sub, app_id))
```

- **One `google_grant` row per (user, app)** is what makes Google scopes per-app.
- `refresh_token_enc` is AES-256-GCM (base64 `nonce||ciphertext||tag`); the key is a
  32-byte value from the sealed secret (`TOKEN_ENC_KEY`).
- `authentik_client_id` maps an inbound token's `azp`/`aud` → `app_id` (see `/token`).

### 4.3 Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET  | `/healthz` | none | liveness/readiness |
| GET  | `/login` → `/auth/callback` | Authentik (broker's own session) | establish broker session |
| GET  | `/connect/{app_id}/start` | broker session cookie | redirect to Google consent for that app's scopes |
| GET  | `/oauth/google/callback` | signed `state` | store the encrypted refresh token for (user, app) |
| POST | `/token` | Bearer = subapp's Authentik access token | return a fresh Google access token |
| GET  | `/connections` | broker session | list the user's connected apps |
| DELETE | `/connections/{app_id}` | broker session | revoke at Google + delete grant |

### 4.4 Security properties

- **App isolation on `/token`:** the `app_id` is derived from the *authenticated* token's
  `azp`/`aud` client_id (→ `app_registry`), never from the request body. App A's token cannot
  vend App B's Google tokens.
- **Refresh tokens never leave the broker** — `/token` returns only short-lived access
  tokens; refresh tokens are encrypted at rest and only used server-side.
- **State signing** — the Google connect flow carries a signed `state` (HMAC) binding
  app_id + user + return URL + nonce + expiry (CSRF protection).
- **Session cookie** — HMAC-signed, `HttpOnly`, `Secure`, `SameSite=Lax`, 12 h TTL.

### 4.5 Configuration

Non-secret (`connect-config` ConfigMap): `PORT`, `BASE_URL`, `AUTHENTIK_ISSUER`,
`AUTHENTIK_TOKEN_ISSUER`, `AUTHENTIK_JWKS_URL`.
Secret (sealed `connect-secrets`): `DATABASE_URL`, `TOKEN_ENC_KEY`, `SESSION_SECRET`,
`BROKER_OIDC_CLIENT_ID/SECRET` (broker's Authentik client), `GOOGLE_CLIENT_ID/SECRET`.

### 4.6 Issuer mode

The broker uses Authentik **per-provider** issuer mode (`.../application/o/connect/`), so its
own OIDC login discovery and `iss` match cleanly. `AUTHENTIK_TOKEN_ISSUER`/`JWKS` point at
that same issuer for now — valid because the broker is currently the only `/token` consumer.
When a second app calls `/token`, switch to global issuer mode (and adjust the verifier). See
[ADR-0003](adr/0003-per-provider-issuer-mode.md).

## 5. Request flows

### 5.1 SSO login (broker's own, representative of any subapp)

```
Browser → GET connect/login
  broker sets oidc_state cookie, 302 → auth.../authorize?client_id&redirect_uri&scope&state
Authentik → (session already present from Google login) → 302 → connect/auth/callback?code&state
Browser → GET connect/auth/callback
  broker verifies state==cookie, exchanges code at auth.../token (confidential client),
  verifies id_token (iss=connect issuer, aud=client_id) via go-oidc,
  sets signed session cookie (sub,email), 302 → /connections
```
Because the Authentik session is reused, the second app's login is **silent** — no re-prompt.

### 5.2 Connect a Google grant (per app)

```
Browser → GET connect/connect/{app_id}/start   (requires broker session)
  broker loads app_registry[app_id], signs state, 302 → Google consent
        with scope=<app.allowed_scopes>, access_type=offline, prompt=consent,
        include_granted_scopes=true, login_hint=<email>
Google → 302 → connect/oauth/google/callback?code&state
  broker verifies state, exchanges code, AES-GCM-encrypts the refresh token,
  upserts google_grant(user_sub, app_id, granted_scopes, refresh_token_enc, ...)
  302 → return URL
```

### 5.3 Vend a Google access token (called by a subapp)

```
Subapp → POST connect/token   Authorization: Bearer <subapp's Authentik access token>
  broker verifies the JWT via Authentik JWKS (iss + signature),
  maps token azp/aud → app_registry → app_id  (isolation boundary),
  loads google_grant(user_sub, app_id):
     - if cached access token still valid → return it
     - else decrypt refresh token, refresh at Google, cache, return
  → { access_token, expires_at, scopes }   (never the refresh token)
```

## 6. Environments

Prod is deployed. A dev split is designed but not yet stood up: namespaces `authentik-dev` /
`connect-dev`, host Postgres `:5433` (`authentik_dev` / `connect_dev`), hosts
`dev.auth.…` / `dev.connect.…`.

## 7. Secrets & supply chain

- All secrets are committed only as `SealedSecret`s; plaintext `*.template.yaml` files show
  shape and are git-ignored. See [ADR-0005](adr/0005-sealed-secrets.md).
- The broker image is built with `ko` (no Docker daemon needed) and pushed to the in-cluster
  registry. See [ADR-0006](adr/0006-build-images-with-ko.md).
