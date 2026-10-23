# Requirements and Constraints

This project was designed to let developers access local apps without per-project setup, while keeping the proxy safe and conservative.

## Core principles

1. discover the current Docker and Dev Container state without requiring repo changes
2. avoid adding new labels, networks, or app configuration requirements
3. keep the proxy safe, local, and easy to reason about
4. use existing host metadata whenever possible
5. validate generated config before reloading NGINX

## Critical constraints

- do not require each application repository to adopt a new convention
- do not require teams to join a new shared Docker network
- do not require Traefik-style labels or app-specific config files
- do not require an extra service to proxy the Docker socket
- do not modify running application containers or their source trees
- do not create new per-project startup steps solely for routing

## Design philosophy

The implementation should prefer the behaviour already present in docker and devcontainers, then layer only the minimal tooling needed to expose those services as local HTTPS routes.

This is why the project inspects metadata, resolves ports conservatively, and generates route names automatically instead of forcing developers to maintain explicit configuration.

## Operational goals

- automatic route generation for valid app containers
- safe fallback when a container cannot be confidently identified
- browser-friendly local URLs under a wildcard dev domain
- consistent TLS termination and generated route inspection
- optional tunnel support when an upstream ALB or internal service needs to be proxied
