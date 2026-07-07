# ADR-0004 — HTTP-01 TLS now, DNS-01 later

**Status:** Accepted (HTTP-01 in use; DNS-01 templated, not applied)

## Context

Certificates for `auth.` and `connect.sajitkhadka.com` are issued by cert-manager + Let's
Encrypt. Two challenge types are available:
- **HTTP-01** via the existing `letsencrypt-prod` ClusterIssuer — already works for every
  other app on this cluster; the box is reachable on `:80` from the internet.
- **DNS-01** via a new `letsencrypt-dns01` issuer — works behind NAT and can issue wildcards,
  but needs a Cloudflare API token (the zone is on Cloudflare).

The user's initial preference was DNS-01, but that requires provisioning a DNS-provider token.

## Decision

Ship on **HTTP-01** now (zero extra setup, proven on this cluster), and keep a
`letsencrypt-dns01` ClusterIssuer + Cloudflare token template ready in `infra/cert-manager/`
to switch to later. Both the Authentik and broker ingresses annotate
`cert-manager.io/cluster-issuer: letsencrypt-prod`.

## Consequences

- **+** Certs issued immediately on first deploy; no dependency on a DNS token.
- **+** HTTP-01 remains a reliable fallback.
- **−** No wildcard certs; issuance depends on `:80` reachability. If the port-forward breaks,
  renewals fail.
- **Migration:** apply `infra/cert-manager/clusterissuer-letsencrypt-dns01.yaml` after sealing
  the Cloudflare token, then flip the two ingress annotations to `letsencrypt-dns01`.
  *(A stuck issuance during bring-up was traced to the broker ingress still pointing at the
  never-applied `letsencrypt-dns01` issuer — a reminder to keep annotations consistent with
  what's actually installed.)*
