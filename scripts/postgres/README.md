# Postgres provisioning

auth-platform reuses the **existing host Postgres** on `192.168.0.120` rather than
running Postgres in-cluster (same pattern sync-todo uses):

| env  | endpoint              | databases                 |
|------|-----------------------|---------------------------|
| prod | `192.168.0.120:5432`  | `authentik`, `connect`    |
| dev  | `192.168.0.120:5433`  | `authentik_dev`, `connect_dev` |

## Run the provisioning SQL

You need the **postgres superuser** password (not stored in the cluster). If you have
`psql` locally:

```bash
# PROD instance
psql "postgres://postgres:SUPERPW@192.168.0.120:5432/postgres" -f provision.sql
```

No local `psql`? Run it from a throwaway pod in the cluster:

```bash
kubectl run pg-admin --rm -it --restart=Never --image=postgres:16-alpine -- \
  psql "postgres://postgres:SUPERPW@192.168.0.120:5432/postgres" -c "\
    CREATE ROLE authentik LOGIN PASSWORD 'PW'; CREATE DATABASE authentik OWNER authentik; \
    CREATE ROLE connect  LOGIN PASSWORD 'PW'; CREATE DATABASE connect  OWNER connect;"
```

For **dev**, point at `:5433` and use the `*_dev` roles/databases (uncomment that block
in `provision.sql`).

## After provisioning

Put the resulting connection strings into the sealed secrets — never commit plaintext:
- Authentik reads its DB creds from the Helm values / `authentik-secrets` (see `authentik/`).
- The broker reads `DATABASE_URL` from `connect-secrets` (see `broker/deploy/`).
