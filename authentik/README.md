# Authentik — SSO IdP (`auth.sajitkhadka.com`)

Authentik is the **authentication plane**: it federates to Google for identity
(`openid email profile` only) and issues OIDC tokens to every subapp. Deployed on the
existing k3s cluster via the official Helm chart, using the **host Postgres** and a
**bundled Redis**.

| env  | namespace       | host                        | Postgres              |
|------|-----------------|-----------------------------|-----------------------|
| prod | `authentik`     | `auth.sajitkhadka.com`      | `192.168.0.120:5432/authentik` |
| dev  | `authentik-dev` | `dev.auth.sajitkhadka.com`  | `192.168.0.120:5433/authentik_dev` |

## Prerequisites

1. **Databases** provisioned — see [`../scripts/postgres/`](../scripts/postgres/).
2. **sealed-secrets** controller installed — see [`../infra/sealed-secrets/`](../infra/sealed-secrets/).
3. **DNS-01 ClusterIssuer** applied — see [`../infra/cert-manager/`](../infra/cert-manager/).
4. `helm` CLI + the chart repo:
   ```bash
   helm repo add authentik https://charts.goauthentik.io && helm repo update
   ```

## Deploy (prod)

```bash
# 1. seal + apply the secret (namespace: authentik)
kubectl create namespace authentik --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f secrets/authentik-secrets.prod.sealed.yaml

# 2. install/upgrade the chart
helm upgrade --install authentik authentik/authentik \
  -n authentik \
  -f helm/values-common.yaml -f helm/values-prod.yaml
```

Dev is identical with `-n authentik-dev` and `values-dev.yaml` + the dev sealed secret.

## Post-install configuration (in the Authentik UI)

The chart brings up Authentik empty. Bootstrap creds are printed by the chart notes
(or set `AUTHENTIK_BOOTSTRAP_PASSWORD`). Then, at `https://auth.sajitkhadka.com/if/admin/`:

1. **Google OAuth Source** (Directory → Federation & Social login → Create → Google):
   - Consumer key/secret = the **"auth.sajitkhadka.com"** Google OAuth client (Phase 0).
   - Scopes: `openid email profile` **only**.
   - Redirect URI in Google Console: `https://auth.sajitkhadka.com/source/oauth/callback/google/`.
2. **Per-subapp OIDC Provider + Application** (one per consumer):
   - Provider: OAuth2/OpenID, client type = confidential (or public + PKCE),
     redirect URIs = the app's callback, sign with a signing key.
   - Application: bind to the provider; note the `client_id` / `client_secret`.
   - The broker (`connect.sajitkhadka.com`) is itself one such OIDC application.

These are UI steps for now; they can later be codified as Authentik **blueprints**
(YAML) and mounted, so the config is reproducible — tracked as follow-up.

## Verify SSO

- Log into the first app via Google; inspect the `id_token` (`sub`, `email`, `iss =
  https://auth.sajitkhadka.com/application/o/<app>/`).
- Open a second registered app → login should be **silent** (Authentik session reused,
  code issued without re-prompting Google).
