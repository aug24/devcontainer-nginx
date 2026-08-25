# Requirements and Constraints

Original specification and design requirements for devcontainer-nginx.

See [copilot.md](./copilot.md) for the complete requirements document.

## Key Principles

1. **Discover existing Docker/Dev Container state; do not impose new per-project configuration**
2. No changes required to application repositories
3. Zero configuration needed for developers
4. Conservative container detection using existing metadata
5. Safe, read-only Docker API access

## Critical Constraints

- Do NOT require individual teams to adopt a new Docker network, labels, environment variables, `devcontainer.json` settings, Dockerfile directives, or Compose configuration.
- Do NOT assume a Docker network named `dev-proxy` exists.
- Do NOT require Dev Containers to join a new shared network.
- Do NOT require Traefik-style labels.
- Do NOT require teams to add `EXPOSE` directives solely for this proxy.
- Do NOT modify application repositories.
- Do NOT make a new convention a prerequisite unless demonstrated to be necessary.

## Design Philosophy

The implementation must first exploit **existing Docker and Dev Containers behaviour**.
