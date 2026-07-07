# Centralized Login — Design (auth.sajitkhadka.com)

**Building blocks:** Authentik (self-hosted OIDC IdP on k3s) + a dedicated Google Connections
broker for per-app Google API access.

## Context

Several self-hosted subapps (`sync-todo`, the main `sajitkhadka.com` site, more to come)
authenticate independently today:

- **sajitkhadka.com** (`D:\projects\me`) — Next.js 14 + **NextAuth/Auth.js v5**, talks to
  Google directly (`openid email profile`), JWT session, **host-only cookie** (no
  `.sajitkhadka.com` sharing). It is only an OAuth *client*, not a provider — it cannot issue
  identity to other apps.
- **sync-todo** — Go backend with a **demo identity** (unauthenticated `X-SyncTodo-User-ID`
  header / `sync_todo_user_id` cookie).
- Nothing today is a real Identity Provider.

**Goal:** stand up `auth.sajitkhadka.com` as a single centralized login for all subapps,
backed by Google. Two requirements shape the design:

1. **One login (SSO)** — sign in once, all subapps trust the same identity.
2. **Per-app Google API access** — some subapps must call Google APIs (Calendar, Drive,
   Gmail…) on the user's behalf, and each app should get **only the scopes it needs**.

These are two *different* concerns. An IdP that federates to Google can only cleanly deliver
**identity** scopes (`openid email profile`). Delegated Google API access with divergent
per-app scopes is a separate authorization problem. This design separates them into two planes.

**Scope:** central platform design + the integration contract subapps must follow. Per-app
migration (sync-todo, the me repo) is a documented follow-up.

---

## Target architecture

Three planes:

```
                         ┌────────────────────────────┐
   Google (identity) ◄───┤  Authentik  (SSO / OIDC)    │  auth.sajitkhadka.com
   openid email profile  │  - federates to Google      │
                         │  - issues OIDC tokens to apps│
                         └──────────────┬──────────────┘
                                        │ id_token/access_token (who you are)
                    ┌───────────────────┼────────────────────┐
                    ▼                   ▼                    ▼
              subapp A            subapp B (sync-todo)   sajitkhadka.com
              (OIDC client)       (OIDC client)          (OIDC client)
                    │
                    │ "give me a Google token for MY scopes"
                    ▼
   Google (APIs)◄─┤  Google Connections broker  │  connect.sajitkhadka.com
   Calendar/Drive │  - one Google OAuth client   │
   per-app scopes │  - per-(user,app) refresh    │
                  │    tokens + scope-limited     │
                  │    token vending              │
                  └──────────────────────────────┘
```

- **Authentication plane** = Authentik. Federates to Google for **identity only**. Every
  subapp is a standard OIDC client. SSO works via front-channel redirect to
  `auth.sajitkhadka.com` (each app keeps its own session) — **not** via a shared
  `.sajitkhadka.com` cookie. This avoids the fragile cross-subdomain cookie approach.
- **Google API access plane** = the Connections broker. Owns a single Google OAuth client,
  does **incremental authorization per app**, stores a **separate refresh token per
  (user, app)** with that app's scope set, and vends short-lived, scope-limited Google access
  tokens. This delivers "different scopes per subapp" with least privilege.
- **Consumer plane** = the subapps, integrating via the contract below.

### DNS / domains
- `auth.sajitkhadka.com` → Authentik
- `connect.sajitkhadka.com` → Google Connections broker
- each subapp on its own subdomain (existing)

---

## Component 1 — Authentik (SSO IdP)

**Deploy on k3s** (already running k3s for sync-todo). Authentik needs: server + worker,
**PostgreSQL**, **Redis**, ingress + TLS via cert-manager on `auth.sajitkhadka.com`. Use the
official Authentik Helm chart. Secrets (`AUTHENTIK_SECRET_KEY`, DB creds, Google client
secret) via k8s Secrets (SOPS/sealed-secrets).

**Configure Google as an OAuth Source** in Authentik:
- Scopes: `openid email profile` **only** (identity — nothing else here).
- Google client: create a **dedicated** OAuth client "auth.sajitkhadka.com" in Google Cloud
  (cleaner than reusing the me repo's `AUTH_GOOGLE_ID`), redirect URI
  `https://auth.sajitkhadka.com/source/oauth/callback/google/`.
- Authentik user `sub` (stable UUID) becomes the canonical identity for all apps.

**Per-subapp provider**: for each app create an Authentik *OIDC Provider + Application*
(client_id/secret, redirect URIs, allowed scopes, PKCE required for public clients).

**SSO verification**: first app login redirects through Google; a second app login reuses the
Authentik session and issues a code silently.

---

## Component 2 — Google Connections broker (`connect.sajitkhadka.com`)

A small service (recommend **Go** — aligns with k3s ops and the sync-todo backend; TS is fine
if you prefer the me-repo stack). It is itself an **OIDC client of Authentik** so it always
knows the current user. Persists to Postgres (own DB, can share the cluster PG).

**Owns one Google OAuth client** "SajitKhadka Connect": redirect URI
`https://connect.sajitkhadka.com/oauth/google/callback`; enable the needed Google APIs in the
Cloud project. Trade-off to accept: Google's consent screen names *Connect*, not each
individual app. (Alternative: per-app Google clients so each app is named on its own consent —
more Console management. Recommend the single client.)

**Data model** (Postgres):
```
app_registry(app_id PK, name, allowed_scopes[], authentik_client_id)
google_grant(user_sub, app_id, granted_scopes[], refresh_token_enc,
             access_token_cache, expires_at, updated_at,
             PRIMARY KEY(user_sub, app_id))
```
Refresh tokens **encrypted at rest** (AES-GCM; key from k8s secret / KMS). One grant row per
(user, app) is what makes scopes per-app.

**Endpoints:**
1. `GET /connect/{app_id}/start` — requires the user's Authentik session; redirects to Google
   with `scope=<app's allowed scopes>`, `access_type=offline`, `prompt=consent` (first grant),
   `include_granted_scopes=true`, `login_hint=<email>`, signed `state` (CSRF + return URL).
2. `GET /oauth/google/callback` — exchanges the code, stores the encrypted refresh token +
   granted scopes for (user_sub, app_id), redirects back to the app.
3. `POST /token` — called **by the subapp**, authenticated with the subapp's **Authentik
   access token** (broker validates the JWT against Authentik JWKS: `iss`, `aud`/`azp` → maps
   to `app_id`, `sub` → user). Returns a fresh Google access token, refreshing via the stored
   refresh token when expired. **Never returns the refresh token.** Enforces that an app can
   only vend tokens for **its own** `app_id` and the authenticated user.
4. `GET /connections` / `DELETE /connections/{app_id}` — list / revoke (also calls Google's
   revoke endpoint and deletes the grant).

**Incremental scope changes**: adding a scope to an app's registry triggers a fresh consent
for just that app's delta on next `/connect`.

---

## Component 3 — Subapp integration contract (for all consumers)

Every subapp follows the same pattern:

1. **Register** as an Authentik OIDC application → `client_id`/`secret`, redirect URI.
2. **Login**: OIDC Authorization Code + PKCE against `auth.sajitkhadka.com`. Validate the
   `id_token` (`iss = https://auth.sajitkhadka.com/...`, `aud = client_id`). Use `sub` as the
   **stable user id**; JIT-provision a local user on first login (store email/name/picture).
3. **Session**: the app holds its own post-exchange session; refresh via Authentik refresh
   token or silent re-auth.
4. **Google API access** (only apps that need it): send the user through broker
   `/connect/{app_id}/start` once, then call broker `/token` to obtain access tokens; handle
   the "not connected" case by prompting a connect.

**Reference migrations (follow-up work):**
- **sajitkhadka.com (`D:\projects\me`)** — swap the NextAuth `Google` provider for a generic
  **OIDC provider pointing at Authentik** (`auth.config.ts` / `auth.ts`). Its service-account
  Drive flow (`GOOGLE_CLIENT_EMAIL`/`GOOGLE_PRIVATE_KEY`) is unrelated and stays. Becomes the
  reference Next.js consumer.
- **sync-todo** — replace `requestUserID` in `backend/internal/handlers/auth.go` to validate an
  Authentik JWT (verify via JWKS, map `sub` → `User.ID`, JIT-provision through the
  `store.Repository` seam). Frontend (`frontend/src/lib/session.ts`, `login/page.tsx`) uses an
  OIDC client instead of the user-picker. WebSocket (`backend/internal/handlers/ws.go`) keeps
  identity in an **HttpOnly+Secure cookie** or a short-lived connect ticket, since browsers
  can't set custom WS headers.

---

## Key constraint: Google OAuth verification for sensitive scopes

Calendar/Drive/Gmail are **sensitive or restricted** scopes. Options:
- **Testing mode** (recommended for personal use): keep the Connect Google app in "Testing",
  add yourself as a test user → no verification needed, but **refresh tokens expire after
  7 days**. The broker must handle re-consent gracefully.
- **Published + verified**: submit the app for Google verification (sensitive scopes) or a
  CASA security assessment (restricted scopes like full Gmail) to remove the 7-day limit.

Decide per scope; document it in the broker README. This does not block SSO (identity scopes
are non-sensitive).

---

## Rollout phases

- **Phase 0 — Google Cloud prep**: create the two OAuth clients (Authentik IdP client + Connect
  broker client), enable required APIs, configure the consent screen + test users, set redirect
  URIs.
- **Phase 1 — Authentik**: deploy on k3s at `auth.sajitkhadka.com`, wire Google source, create
  one test OIDC application, verify Google login + second-app silent SSO end-to-end.
- **Phase 2 — Connections broker**: build + deploy at `connect.sajitkhadka.com` (registry,
  encrypted storage, connect flow, token vending, revoke). Verify with a test app requesting a
  Calendar scope.
- **Phase 3 — First real consumer**: migrate the me repo (or a throwaway test app) to the
  central login; finalize the written integration contract.
- **Phase 4 — Roll out** to remaining subapps (sync-todo, etc.) as individual follow-ups.

---

## Verification

- **Authentik SSO**: log in via Google; inspect `id_token` claims (`sub`, `email`); open a
  second registered app and confirm login is silent (no re-prompt).
- **Broker connect**: run `/connect/{app}/start`; confirm a refresh token is stored
  **encrypted**; call `/token` and use the returned access token against a real Google API
  (e.g. Calendar `events.list`); call `DELETE /connections/{app}` and confirm the grant + Google
  authorization are revoked.
- **Security assertions**:
  - App A's Authentik token cannot vend tokens for App B (`/token` rejects mismatched `aud`).
  - `/token` rejects invalid/expired Authentik JWTs.
  - Expired Google access tokens auto-refresh; refresh tokens are never returned to apps.
  - Two apps for the same user hold independent scope sets (least privilege verified).

---

## Deliverables

- k3s manifests/Helm values for Authentik (server, worker, Postgres, Redis, ingress+TLS).
- **Google Connections broker** service (Go recommended): endpoints above, Postgres schema,
  encryption of refresh tokens, Authentik JWT validation, README covering the Google
  verification constraint.
- Google Cloud config: two OAuth clients + consent screen (documented, not code).
- A written **subapp integration guide** (the contract in Component 3) for future apps.
- (Follow-up) ADRs in each consuming repo when it migrates — e.g. update sync-todo
  `docs/adr/0008-demo-identity-auth-boundary.md` to point at the central login.
