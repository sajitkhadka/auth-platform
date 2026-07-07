# Documentation index

| Doc | What it covers |
|-----|----------------|
| [DESIGN.md](DESIGN.md) | Original design + rollout plan (the "why" and target architecture) |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Detailed architecture as built: Authentik, broker, every service, request flows, infra |
| [DEPLOYMENT-LOG.md](DEPLOYMENT-LOG.md) | Chronological record of how it was stood up + every issue hit and its fix |
| [integration.md](integration.md) | Subapp integration **contract** (what an app must do) |
| [runbooks/add-an-app.md](runbooks/add-an-app.md) | Step-by-step **runbook** to onboard a new app (SSO + Google access) |
| [adr/](adr/) | Architecture Decision Records |

## Quick facts

- **Live:** `auth.sajitkhadka.com` (Authentik SSO) + `connect.sajitkhadka.com` (Google broker).
- **Cluster:** k3s at 192.168.0.120; host Postgres `:5432`/`:5433`; registry `:5000`.
- **Secrets:** sealed-secrets (controller in `kube-system`). **Back up the controller key.**
- **TLS:** HTTP-01 (`letsencrypt-prod`) today; DNS-01 templated for later.
