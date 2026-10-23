# Quick Start Guide

## Prerequisites

- Docker Desktop or Docker Engine
- TLS certificates for your local domain, typically via `mkcert`
- A trusted local CA for the wildcard domain you use (for example `*.local.dev-gutools.co.uk`)
- DNS or `/etc/hosts` entries for the generated hostnames if you want browser access without an external resolver
- Nothing else already listening on ports 80/443 on your machine

## Step 1: Build the image

```bash
docker build -t devcontainer-nginx:local .
```

## Step 2: Start the proxy container

```bash
docker compose up -d --build
```

This starts the proxy and mounts:

- the Docker socket read-only
- your certificate directory into `/etc/nginx/certs`
- the mkcert-local CA files for auto-generation when needed

The entrypoint will create a wildcard certificate if `TLS_CERT` and `TLS_KEY` do not exist, using the local mkcert root if available. If no mkcert CA is configured, it falls back to a self-signed certificate.

## Step 3: Run an app container or open a devcontainer

Any container with a reasonable name is usually enough:

```bash
docker run -d --name myapp \
  -p 3000:3000 \
  your-app:latest
```

Supported patterns include:

- containers with a single exposed or published port
- dev containers with forwarded ports or `portsAttributes`
- any container that the built-in discovery heuristics recognize as a dev target

## Step 4: Open the homepage

Visit:

```text
https://devcontainer-nginx.local.dev-gutools.co.uk/
```

This page shows:

- discovered containers
- generated routes
- active tunnel entries
- direct links to the generated app URLs
- exported config endpoints

## Step 5: Open your app

After discovery, the app is usually available at:

```text
https://myapp.local.dev-gutools.co.uk
```

If you prefer, you can also use the HTTP form:

```text
http://myapp.local.dev-gutools.co.uk
```

## Using dev containers

A devcontainer with a forwarded port is discovered automatically. For example:

```json
{
  "name": "My Project",
  "image": "node:18",
  "forwardPorts": [3000]
}
```

This can become:

```text
https://my-project.local.dev-gutools.co.uk
```

## Using multiple ports

If a container exposes multiple ports, the project can route multiple hostnames. For instance, a devcontainer with `portsAttributes` may produce:

- `myapp-backend.local.dev-gutools.co.uk`
- `myapp-frontend.local.dev-gutools.co.uk`

## Using Docker Compose

```yaml
services:
  app:
    image: node:18
    expose:
      - "3000"
```

This is automatically discovered and exposed as:

```text
https://app.local.dev-gutools.co.uk
```

## Troubleshooting

### The app is not appearing

1. Check the proxy logs:

```bash
docker logs devcontainer-nginx | tail -n 100
```

2. Inspect the container metadata:

```bash
docker inspect myapp | jq '.Config.Labels, .Config.ExposedPorts, .NetworkSettings.Networks'
```

3. Verify the generated config page:

```bash
curl -k https://devcontainer-nginx.local.dev-gutools.co.uk/generated.conf
```

### TLS or certificate errors

```bash
docker exec devcontainer-nginx ls -la /etc/nginx/certs/
curl -vk --resolve myapp.local.dev-gutools.co.uk:443:127.0.0.1 https://myapp.local.dev-gutools.co.uk
```

### Connectivity issues

```bash
docker exec devcontainer-nginx curl -v http://myapp:3000
docker inspect devcontainer-nginx | jq '.NetworkSettings.Networks'
```

## Tunnel workflow

The built-in tunnels page is available at:

```text
https://devcontainer-nginx.local.dev-gutools.co.uk/tunnels
```

It supports creating local tunnel entries that reverse-proxy to an ALB or other upstream target. For HTTPS backends, you can toggle whether nginx verifies the upstream certificate before proxying.

---

That's it: start the proxy, run your app, and visit the generated hostname.
