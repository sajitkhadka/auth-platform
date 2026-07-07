# ADR-0001 — Separate identity and Google-API-access planes

**Status:** Accepted

## Context

Subapps need two different things from "central login":
1. **Identity / SSO** — sign in once, every app trusts the same user.
2. **Per-app Google API access** — some apps call Google APIs (Calendar, Drive, Gmail) on
   the user's behalf, each needing only its own scopes.

An IdP that federates to Google can cleanly deliver only **identity** scopes
(`openid email profile`). Delegated Google API access with divergent per-app scopes is a
distinct authorization problem: it needs per-(user, app) refresh tokens, incremental consent,
and scope-limited token vending — none of which belong in an identity federation.

## Decision

Split the system into two planes:
- **Authentik** is the authentication plane: federates to Google for identity only, and is a
  standard OIDC provider to every subapp.
- A dedicated **Connections broker** is the Google-API-access plane: owns one Google OAuth
  client, stores a separate encrypted refresh token per (user, app), and vends short-lived,
  scope-limited Google access tokens.

Subapps integrate with each plane independently; only apps that need Google APIs touch the
broker.

## Consequences

- **+** Least privilege: each app gets only its own Google scopes; identity stays clean.
- **+** Refresh tokens are centralized, encrypted, and never handed to apps.
- **+** Adding SSO to an app does not entangle it with Google API scopes.
- **−** Two services to run and reason about instead of one.
- **−** Google's consent screen names the broker ("Connect"), not each individual app
  (accepted trade-off; per-app Google clients would name each app but multiply Console
  management).
