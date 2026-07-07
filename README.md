# auth-platform

Centralized login for all `*.sajitkhadka.com` subapps — one Google-backed sign-in (SSO) plus
per-app Google API access.

- **Identity Provider:** [Authentik](https://goauthentik.io/) at `auth.sajitkhadka.com`
  (self-hosted on k3s), federating to Google for identity.
- **Google API access:** a dedicated **Google Connections broker** at
  `connect.sajitkhadka.com` that gives each subapp only the Google scopes it needs.

See [`docs/DESIGN.md`](docs/DESIGN.md) for the full architecture and rollout plan, and
[`docs/integration.md`](docs/integration.md) for the subapp integration contract.

## Status

Scaffolded — not yet deployed. Broker service compiles/vets/formats clean; all cluster
manifests + runbooks written. Deployment is gated on manual Phase 0 steps (Google Cloud
OAuth clients) and DB/secret provisioning.

## Infrastructure (existing k3s at `192.168.0.120`)

Reuses what's already on the cluster: **cert-manager**, **ingress-nginx**, the **host
Postgres** (prod `:5432`, dev `:5433` — one DB+user per app), and the local image registry
`192.168.0.120:5000`. Adds a **sealed-secrets** controller and a **DNS-01** ClusterIssuer.

## Repo layout

```
auth-platform/
├── docs/
│   ├── DESIGN.md            Architecture + rollout plan
│   └── integration.md       Subapp integration contract
├── infra/
│   ├── cert-manager/        DNS-01 ClusterIssuer (+ Cloudflare token template)
│   └── sealed-secrets/      controller install + sealing runbook
├── scripts/postgres/        provisioning SQL for authentik/connect DBs (prod+dev)
├── authentik/               Authentik SSO IdP — Helm values (dev/prod) + sealed secret template
│   ├── helm/                values-common / values-dev / values-prod
│   ├── secrets/             authentik-secrets.template.yaml
│   └── README.md            deploy + post-install (Google source, per-app providers)
└── broker/                  Google Connections broker (Go)
    ├── cmd/ internal/ migrations/   service code
    ├── deploy/              k8s manifests + connect-secrets template
    ├── Dockerfile
    └── README.md
```

## Deploy order

1. **Phase 0 (manual):** create the two Google OAuth clients + consent screen (see
   `docs/DESIGN.md`). Not code.
2. **Cluster prereqs:** install sealed-secrets (`infra/sealed-secrets/`), apply the DNS-01
   ClusterIssuer (`infra/cert-manager/`).
3. **Databases:** run `scripts/postgres/provision.sql` against the host Postgres.
4. **Authentik:** `authentik/` — seal secret, `helm upgrade --install`, then configure the
   Google source + per-app providers in the UI.
5. **Broker:** `broker/` — build/push image, seal secret, `kubectl apply -f deploy/`.
6. **Consumers:** migrate apps per `docs/integration.md`.
