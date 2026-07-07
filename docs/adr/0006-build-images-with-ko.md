# ADR-0006 — Build Go images with ko (no Docker)

**Status:** Accepted

## Context

The broker is a pure-Go service. It must be built into a container image and pushed to the
in-cluster registry (`192.168.0.120:5000`, plain HTTP). The development machine has no running
Docker daemon, so the committed `Dockerfile` couldn't be used locally, and building on the
node was not readily available. Options: start Docker Desktop, run an in-cluster kaniko build
from the git repo, or use `ko`.

## Decision

Build with **`ko`** (`github.com/google/ko`). `ko build ./cmd/broker` compiles the Go binary
locally and produces/pushes a distroless static image directly to the registry — no Docker
daemon required:

```
KO_DOCKER_REPO=192.168.0.120:5000/connect-broker \
  ko build ./cmd/broker --bare --tags latest --insecure-registry --platform=linux/amd64
```

The `Dockerfile` is kept for anyone who prefers a Docker/kaniko build; it produces an
equivalent distroless static image.

## Consequences

- **+** No Docker daemon needed; fast, reproducible builds for a Go service.
- **+** Distroless static base by default (small, no shell) — same intent as the Dockerfile.
- **−** `ko` builds Go only (fine here; the broker is pure Go).
- **−** `--insecure-registry` is required because the registry is plain HTTP.
- **Note:** `ko` (and the Go toolchain it pulls) needs disk headroom — a full `C:` broke the
  first build until the Go build cache was cleared.
