# ADR-0002 — Reuse the existing host Postgres

**Status:** Accepted

## Context

Both Authentik and the broker need Postgres. The cluster already has Postgres running **on
the host** (not in k8s): a prod instance on `192.168.0.120:5432` and a dev instance on
`:5433`, reached over the LAN with `sslmode=disable`, with one database + role per app
(the pattern sync-todo already uses). The Authentik Helm chart can also deploy its own
bundled Postgres subchart.

## Decision

Reuse the existing host Postgres. Create dedicated databases + roles: `authentik` and
`connect` on `:5432` (prod), `authentik_dev` / `connect_dev` on `:5433` (dev). Do **not**
deploy the chart's bundled Postgres (`postgresql.enabled: false`).

Provisioning is a documented SQL script (`scripts/postgres/provision.sql`) run by the
Postgres superuser; connection strings live only in sealed secrets.

## Consequences

- **+** One Postgres to operate, back up, and monitor; consistent with existing apps.
- **+** No stateful Postgres pod / PVC to manage in-cluster.
- **−** The host Postgres is a shared dependency and a single point of failure for multiple
  apps; provisioning needs superuser access out-of-band.
- **−** DB creds cross the LAN with `sslmode=disable` (acceptable on a trusted home LAN;
  revisit if the topology changes).
- **Note:** this chart version does not bundle Redis either — Authentik's Redis is a separate
  small in-namespace Deployment (not the host).
