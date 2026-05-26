# Changelog — agent-proxmox

All notable changes to **fluid-pub/agent-proxmox** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tag naming: `0.y.z` (no `v` prefix). Align `cmd/version.go` and `config/agent.example.yml` (`agent.version`) with the tag before release.

## [Unreleased]

## [0.1.0] - 2026-05-26

### Added

- Initial public release aligned with **agent-core** (WebSocket execution, enrollment, `runtime_config` sync).
- Skills: `proxmox.vm.list`, `proxmox.vm.status`, `proxmox.vm.delete`, `proxmox.vm.clone_and_start` (clone, post-config including cloud-init `ide2`, task polling, start).
- CI/CD via `fluid-pub/actions`, distroless image, GitHub Release asset `fluid-agent-proxmox-linux-amd64`.
- Local `gofmt` pre-commit hook matching CI.
