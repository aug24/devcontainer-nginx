# devcontainer-nginx

A Docker-based reverse proxy that automatically discovers and routes to development containers without requiring per-project configuration.

![screenshot.png](docs/screenshot.png)

## TL;DR

### Start server
```bash
docker build -t devcontainer-nginx:local .
docker-compose up -d --build --force-recreate devcontainer-nginx
```

### Start your app 
 - any container or devcontainer with a name and a single port
 - any devcontainer with ports and portAttributes

eg
```bash
docker run -d --name myapp -p 3000:3000 your-app:latest
```

### Access it
Open https://myapp.local.dev-gutools.co.uk

See the [quick start guide](./docs/QUICK-START.md) for detailed instructions.

### The UI

The proxy also serves a simple homepage at `https://devcontainer-nginx.local.dev-gutools.co.uk/`.

It shows:

- which containers were discovered
- which routes are active
- the generated app URLs
- links to the live nginx config and container list

This is the easiest way to check what the proxy is routing and to open an app quickly from the browser.

## What This Is

**devcontainer-nginx** is a single container that:

- Discovers running Docker containers and Dev Containers automatically using the Docker Engine API
- Generates HTTPS reverse proxy routes based on existing container metadata
- Exposes applications via secure hostnames: `https://<project>.local.dev-gutools.co.uk`
- Requires **zero configuration changes** in application repositories

**No labels, no configuration, no code changes needed.** Simply start your app and access it via `https://app-name.local.dev-gutools.co.uk`.

## Documentation

- **[Quick Start Guide](./docs/QUICK-START.md)** — Setup and first run
- **[Configuration Reference](./docs/CONFIGURATION.md)** — Environment variables and advanced options
- **[Architecture](./docs/ARCHITECTURE.md)** — How the proxy works, component details
- **[Building](./docs/BUILDING.md)** — Build from source or Docker image
- **[Security](./docs/SECURITY.md)** — Security considerations and best practices
- **[Requirements](./docs/REQUIREMENTS.md)** — Design principles and constraints

## Key Features

- **Zero configuration** — No labels, environment variables, or code changes needed
- **Automatic discovery** — Finds containers using Docker Engine API and existing metadata
- **HTTPS by default** — TLS-terminated reverse proxy with wildcard certificates
- **Smart port detection** — Intelligently selects ports from exposed ports, forwarded ports, or labels
- **Event-driven** — Responds to Docker container start/stop events (no polling)
- **Safe updates** — Validates Nginx configuration before reloading
- **Read-only access** — Docker socket mounted read-only for security

## License

MIT
