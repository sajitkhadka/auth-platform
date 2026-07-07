# Architecture Decision Records

Short records of the significant, non-obvious decisions behind auth-platform. Format:
Status · Context · Decision · Consequences. Newer decisions may supersede older ones.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-two-plane-identity-and-api-access.md) | Separate identity and Google-API-access planes | Accepted |
| [0002](0002-reuse-host-postgres.md) | Reuse the existing host Postgres | Accepted |
| [0003](0003-per-provider-issuer-mode.md) | Per-provider OIDC issuer mode (for now) | Accepted |
| [0004](0004-tls-http01-now-dns01-later.md) | HTTP-01 TLS now, DNS-01 later | Accepted |
| [0005](0005-sealed-secrets.md) | Secrets via sealed-secrets | Accepted |
| [0006](0006-build-images-with-ko.md) | Build Go images with ko (no Docker) | Accepted |
