# ADR-0003 — Per-provider OIDC issuer mode (for now)

**Status:** Accepted (revisit when a second app calls `/token`)

## Context

Authentik can issue OIDC tokens with either:
- **per-provider** issuer: `iss = https://auth.sajitkhadka.com/application/o/<app-slug>/`,
  with per-app OIDC discovery at that URL; or
- **global** issuer: one `iss = https://auth.sajitkhadka.com/application/o/` for all providers.

The broker's `/token` endpoint validates inbound access tokens from subapps. The original
design assumed **global** issuer so `/token` could validate every app's token against one
issuer + JWKS, keying app identity off the token's `azp`/`aud`.

But the broker is *also* an OIDC **client** of Authentik for its own login. With global
issuer mode, Authentik still serves discovery per-app (`.../connect/.well-known/…`) while the
tokens carry the global `iss` — so the discovery document's `issuer` differs from the URL the
client discovered at, breaking the standard go-oidc `NewProvider` check (it would need
`InsecureIssuerURLContext`, i.e. a code change).

## Decision

Use **per-provider** issuer mode. The broker's own login discovery and `iss` match cleanly
with no code change. `AUTHENTIK_TOKEN_ISSUER`/`AUTHENTIK_JWKS_URL` point at the broker's own
per-provider issuer (`.../application/o/connect/`), which is correct because **the broker is
currently the only `/token` consumer**.

## Consequences

- **+** The broker's own OIDC login works with stock go-oidc; no discovery hacks.
- **+** Simplest correct configuration for a single consumer; deployed and verified.
- **−** `/token` currently validates only against one issuer/JWKS. When a **second** app
  begins calling `/token`, we must either switch all providers to global issuer mode (and add
  `InsecureIssuerURLContext` for the broker's own login) **or** extend
  `internal/authentik/verifier.go` to resolve keys per `azp`. This is the load-bearing item to
  revisit before multi-app token vending.
