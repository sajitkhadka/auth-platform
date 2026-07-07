# sealed-secrets

The cluster does **not** have the sealed-secrets controller yet. All auth-platform
secrets (Authentik secret key, DB creds, Google client secrets, broker encryption key,
Cloudflare token) are committed only in **sealed** form; plaintext `*.template.yaml`
files show the shape and are never filled in and committed.

## 1. Install the controller (once)

```bash
helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
helm install sealed-secrets sealed-secrets/sealed-secrets \
  --namespace kube-system
```

(Controller lives in `kube-system` in this repo's `kubeseal` invocations — pass
`--controller-namespace kube-system` when sealing.)

## 2. Install the kubeseal CLI (local)

Windows (scoop): `scoop install kubeseal` — or grab the release binary from
https://github.com/bitnami-labs/sealed-secrets/releases and put it on PATH.

## 3. Seal a secret

Every `*.template.yaml` in this repo becomes a real `Secret` once you fill it in.
Never commit the filled-in plaintext — seal it:

```bash
# fill in the plaintext (kept out of git via .gitignore), then:
kubeseal --format=yaml --controller-namespace kube-system \
  < authentik-secrets.yaml > authentik-secrets.sealed.yaml
# commit ONLY the .sealed.yaml, then:
kubectl apply -f authentik-secrets.sealed.yaml
```

A `SealedSecret` is namespace+name scoped by default — seal each secret for the
namespace it will live in (`authentik`, `connect`, `connect-dev`, `cert-manager`).

## Backup the controller key

The controller's private key is what makes SealedSecrets decryptable. Back it up:

```bash
kubectl get secret -n kube-system \
  -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-key.backup.yaml
```

Store that backup OUT of git (password manager / offline). Losing it means resealing
every secret against a freshly generated key.
