# Deployment log — bringing auth-platform up in prod

A chronological record of how the platform was actually stood up on the k3s cluster at
192.168.0.120, including the problems encountered and how each was resolved. This is the
"what really happened" companion to [`ARCHITECTURE.md`](ARCHITECTURE.md) and the phased plan
in [`DESIGN.md`](DESIGN.md). Deployed **2026-07-07**.

## Phase A — Investigate the target

Before writing manifests, inventoried the cluster:
- `kubectl` + kubeconfig already pointed at the cluster (`sserver`, k3s v1.31.5, admin access).
- cert-manager and ingress-nginx already installed; existing apps get certs via HTTP-01
  (`letsencrypt-prod`), so the box is reachable on `:80` from the internet.
- **No Postgres pod** in-cluster, yet `:5432` open on the host → Postgres runs on the host.
  Reading sync-todo's config revealed the topology: prod `:5432`, dev `:5433`, one DB+role
  per app, `sslmode=disable`, injected as a `DATABASE_URL` secret.
- Local image registry at `192.168.0.120:5000` (used by sync-todo).
- sealed-secrets controller **not** installed.

Decisions locked with the user: build both scaffolds (Authentik-first), reuse the host
Postgres, TLS via cert-manager (started HTTP-01), secrets via sealed-secrets.

## Phase B — Scaffold the repo

Wrote the full repo (Authentik Helm values, broker Go service, infra manifests, provisioning
SQL, integration guide) and verified the broker builds/vets/formats clean (Go 1.25). Opened
PR #1 on a fresh `main` base. *(No cluster changes yet.)*

## Phase 0 — Google Cloud (user)

The user created two OAuth clients in the existing Google Cloud project (the one already
holding the `sajit.me` login client, reusing its consent screen + test users):
- `auth.sajitkhadka.com` — identity, redirect `…/source/oauth/callback/google/`.
- `SajitKhadka Connect` — API access, redirect `…/oauth/google/callback`.
Calendar API enabled; consent screen kept in Testing with the user as a test user.

## Phase 1 — Cluster prerequisites

- Confirmed DNS: `synctodo` resolved to the public IP but `auth`/`connect` had **no records**.
  User added A records `auth`/`connect` → `<ORIGIN_PUBLIC_IP>` (grey-cloud) in Cloudflare (which
  is where the zone is hosted — `algin`/`amber.ns.cloudflare.com`).
- Installed local tooling into the session scratchpad: **helm v4.2.2**, **kubeseal v0.38.4**.
- Installed the **sealed-secrets controller** v0.38.4. *(First attempt via `kubectl apply -f
  <remote-url>` was correctly blocked by a guardrail — unread remote manifest with RBAC into a
  shared cluster. Downloaded and reviewed the manifest, then applied from the local file.)*
- Added the Authentik Helm repo (chart 2026.5.3).

## Phase 2 — Provision databases

Postgres requires a password for the superuser (no trust auth). Superuser role is **`sajit`**.
Created two databases + roles via a throwaway `postgres:16-alpine` pod (existing DBs
untouched):
```
CREATE ROLE authentik LOGIN PASSWORD '…'; CREATE DATABASE authentik OWNER authentik;
CREATE ROLE connect   LOGIN PASSWORD '…'; CREATE DATABASE connect   OWNER connect;
```
App DB passwords were generated locally (hex, URL-safe) and stored only in the sealed secrets.

## Phase 3 — Deploy Authentik

Sealed `authentik-secrets` (secret key + DB password) and applied it; the controller unsealed
it into a real Secret. Then `helm upgrade --install`. Several **chart-reality mismatches**
were caught by rendering (`helm template`) before installing:

1. **`envFrom` level.** The scaffold put `envFrom` at the top level; the chart wants
   `global.envFrom`. Wrong placement would have silently dropped the DB password → crashloop.
2. **No bundled Redis/Postgres.** Chart 2026.5.3 bundles neither (no `redis` values key, no
   redis dependency). Added a small in-namespace Redis (`authentik/redis.yaml`) and pointed
   `authentik.redis.host` at it; kept `postgresql.enabled: false` (already the default).
3. **Secret injection strategy.** Verified via the templates that the chart flattens
   `authentik.*` into a config Secret and *skips empty values*, and the Deployment layers
   `global.envFrom` after it — so non-secret config in values + the two secrets via
   `global.envFrom` compose cleanly with no override conflict.

Result: server ran migrations against the `authentik` DB, all pods Ready, HTTP-01 cert issued
on first try, `https://auth.sajitkhadka.com` serving 200 with a valid cert.

### Post-install (Authentik UI + API)

- User set the `akadmin` password (`/if/flow/initial-setup/`) and added the Google OAuth
  source.
- **Google button missing:** the source existed but the `default-authentication-identification`
  stage had `sources: []`. PATCHed the stage (via the admin API) to include the Google source
  and enable source labels → login button appeared. **Google login verified working.**

## Phase 4 — Deploy the broker

- Created the Authentik **OIDC provider + application** (`connect`) via the admin API,
  per-provider issuer, PKCE-capable confidential client, redirect
  `https://connect.sajitkhadka.com/auth/callback`. Verified OIDC discovery matched the broker
  config.
- **Built the image with `ko`** (no Docker daemon present) → `192.168.0.120:5000/connect-broker`.
  - *Snag: `C:` disk was 100% full*, so `go install ko` failed ("not enough space"). Freed
    1.5 GB via `go clean -cache`, then the install + build + push succeeded.
- Copied the `regcred` pull secret into the `connect` namespace; sealed `connect-secrets`
  (DB URL, enc key, session secret, broker OIDC creds, Google Connect creds) and applied it;
  applied ConfigMap + Deployment/Service/Ingress.
- **TLS stuck:** the broker ingress still referenced `letsencrypt-dns01` — an issuer that was
  *templated but never applied* (it needs the Cloudflare token we deferred). Switched the
  broker ingress to `letsencrypt-prod` (HTTP-01), deleted the stuck Certificate/Request to
  force reissue → cert issued.
- Broker verified: `/healthz` 200 (valid cert), `/login` 302 → Authentik, `/token` 401.

## Phase 5 — Debug the first broker login

Clicking `/login` failed. Root-caused it step by step (each step added just enough logging):
1. Handler returned generic "exchange failed" → added error logging → saw `invalid_grant`.
2. Confirmed client creds matched Authentik and both client-auth styles were accepted, ruling
   out the classic oauth2 auth-style double-consume.
3. Logged the callback params → the code was **empty** (`code_len: 0`): Authentik was
   redirecting back with an `error`, not a code.
4. Logged the `error` param → `invalid_request` ("malformed"). Reproduced the authorize
   request directly with curl (same result, pre-login).
5. Authentik's own server log gave the reason: **"Invalid grant_type for provider"** — the
   provider's `grant_types` was **empty** (creating an OAuth2 provider via the API leaves it
   empty in Authentik 2026.x).

**Fix:** `PATCH /api/v3/providers/oauth2/1/` with
`grant_types: ["authorization_code","refresh_token"]`. *(A direct pod-shell DB mutation was
correctly blocked by a guardrail; used the admin API instead.)* Authorize then proceeded
normally.

The broker handler was also hardened to detect the IdP `?error=` param up front instead of
attempting an exchange on an empty code, and the diagnostic noise was removed.

## Outcome

End-to-end **silent SSO verified**: logging into the broker via Authentik (session reused from
the Google login) lands on `/connections` returning `[]`. Both planes are live in prod. All
manifests, sealed secrets, and the bug-fix commits are on PR #1.

## Follow-ups (not done)

- Exercise the full Google-token flow (register an app, `/connect`, pull a token via `/token`).
- Migrate a real consumer (sync-todo / the `me` repo) per [`integration.md`](integration.md).
- Switch TLS to the DNS-01 issuer (needs a Cloudflare API token).
- Codify Authentik config (Google source, providers) as **blueprints** for reproducibility.
- Broker hardening: PKCE on its own login, tests, an HTML `/connections` page.
