# ADR-0005 — Secrets via sealed-secrets

**Status:** Accepted

## Context

The platform has several secrets: Authentik secret key, DB passwords, Google client secrets,
the broker's Authentik client secret, and the broker's token-encryption/session keys. We want
them version-controlled with the manifests (GitOps) without committing plaintext. Options
considered: sealed-secrets, SOPS, or plain Secret manifests kept out of git.

## Decision

Use **Bitnami sealed-secrets**. A controller in `kube-system` decrypts committed
`SealedSecret` CRs into real `Secret`s. Workflow:
- Each secret has a git-ignored `*.template.yaml` showing its shape.
- Fill in the plaintext locally, `kubeseal` it into `*.sealed.yaml`, commit **only** the
  sealed file, `kubectl apply` it.
- `SealedSecret`s are name+namespace scoped, so each is sealed for its target namespace.

## Consequences

- **+** Encrypted secrets live safely alongside manifests in git.
- **+** Simple mental model; no external KMS/age keychain to manage.
- **−** The controller's private key is the single thing that can decrypt everything — it
  **must be backed up** out of git (losing it means resealing every secret).
- **−** Rotating a secret means re-sealing and re-applying.
- **Operational note:** the controller was installed from a locally-reviewed manifest (not a
  blind `kubectl apply -f <url>`), consistent with the guardrails on shared-cluster changes.
