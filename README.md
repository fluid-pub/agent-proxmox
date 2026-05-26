# fluid-pub/agent-proxmox

Fluid **execution agent** for **Proxmox VE**: maintains a **persistent WSS** connection to the control plane (`/v1/agents/websocket`) for skills, logs, and `runtime_config` sync — not HTTP polling like probes. Proxmox API calls (clone/start, list, status, delete) use `proxmox.base_url` in config.

## Repository layout

| Path | Role |
|------|------|
| `core/` | Git submodule → [`fluid-pub/agent-core`](https://github.com/fluid-pub/agent-core) |
| `cmd/` | Entrypoint and `cmd/version.go` (semver for releases) |
| `internal/` | Proxmox API client, agent skills, config |
| `config/agent.example.yml` | Configuration template |
| `.github/workflows/` | CI and release via [`fluid-pub/actions`](https://github.com/fluid-pub/actions) |

## Local development

One-time per clone, enable the same **`gofmt`** check as CI:

```bash
./scripts/install-git-hooks.sh
```

```bash
git submodule update --init --recursive
cp config/agent.example.yml config/agent.yml
cp env.secrets.example env.secrets
# Set PROXMOX_* and FLUID_CONTROLPLANE_WEBSOCKET_URL (+ org UUID + connection token) in env.secrets (never commit that file).
source env.secrets
make dev
```

Credentials and API URLs come from **your** `env.secrets` and local `config/agent.yml` only — nothing operator-specific is stored in this repository.

`make dev` runs `go run ./cmd` with `-config config/agent.yml`.

**Monorepo Fluid** (`code/agents/proxmox/`): use `make monorepo-replace` so `go.mod` points at `../core` instead of the `core/` submodule. Do not commit that replace on `develop` (CI and releases use `replace => ./core`).

### Git in the Fluid workspace

If this directory is not yet a clone of this repository:

```bash
cd code/agents/proxmox
git init
git remote add origin git@github.com:fluid-pub/agent-proxmox.git
git fetch origin
git checkout -B develop origin/develop
git submodule update --init --recursive
./scripts/install-git-hooks.sh
make monorepo-replace   # optional when using code/agents/core in the monorepo
```

Keep `env.secrets` and `config/agent.yml` local (gitignored).

## Control plane connection

| Phase | Transport | Purpose |
|-------|-----------|---------|
| Steady state | **WSS** (`controlplane.websocket_url`) | `skill_invoke` / `skill_result`, log events, `runtime_config` push |
| First boot only (optional) | HTTP `POST /api/v1/enrollment/enroll` | Exchange `FLUID_ENROLLMENT_TOKEN` for `organization_uuid` + connection token, then **WSS** |

`env.secrets.example` lists WSS credentials first. `FLUID_CONTROLPLANE_HTTP_BASE` is only required when enrolling without a pre-provisioned connection token (see control plane `agent_enrollment_bootstrap`). Durable credentials: `/etc/fluid/proxmox/credentials.yaml` (`-credentials` flag). Enroll with `agent_type: proxmox`, `principal: execution_agent`.

## Changelog

Release notes: [CHANGELOG.md](CHANGELOG.md).

## Prerequisite: agent-core

CI and release workflows check out [`fluid-pub/agent-core`](https://github.com/fluid-pub/agent-core) at `develop` (or the same semver tag on release). That repository must contain the shared library (`fluid/agents/core`) before the first **agent-proxmox** tag build succeeds. Monorepo copies live under `code/agents/core/` until published.

## Releases

Push a semver tag **without** `v` (e.g. `0.1.0`) matching `var Version` in `cmd/version.go`. The release workflow publishes:

- `ghcr.io/fluid-pub/agent-proxmox:<tag>`
- GitHub Release asset `fluid-agent-proxmox-linux-amd64` and `SHA256SUMS.txt`

Tag creation on this public repository is restricted to the org **`release-managers`** team.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting. Repository automation includes Dependabot and CodeQL.
