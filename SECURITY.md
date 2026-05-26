# Security policy

## Supported versions

Security fixes are applied on the latest release line published from this repository (semver tags on `develop` / GitHub Releases). Older tags are not maintained unless stated in a release advisory.

## Reporting a vulnerability

**Do not** open a public GitHub issue for security vulnerabilities.

Preferred channels:

1. **Private vulnerability reporting** (if enabled on this repository): use **Security → Advisories → Report a vulnerability** on GitHub.
2. **GitHub Security Advisories** for this repository: [fluid-pub/agent-proxmox security advisories](https://github.com/fluid-pub/agent-proxmox/security/advisories).
3. If neither channel is available, contact the Fluid maintainers through your usual Fluid support or security contact path.

Include enough detail to reproduce the issue (affected version, configuration, steps, impact). We aim to acknowledge reports within a few business days and will coordinate disclosure once a fix is available.

## What to expect

- Confirmed issues are tracked as security advisories or private reports until a fix is released.
- Credit is given to reporters when they agree, unless anonymity is requested.
- Dependabot and CodeQL may open pull requests for dependency or static-analysis findings; those are handled like other contributions via `develop`.

## Scope

This policy covers the **agent-proxmox** source code, container image, and release artifacts built from this repository. It does not cover third-party services (Proxmox VE, your control plane deployment, or credentials you configure locally).
