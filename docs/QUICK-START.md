# Quick Start Guide

## Prerequisites

- Docker Desktop (macOS or Linux)
- mkcert installed and its root CA trusted: `mkcert -install`
- mkcert root CA files available at `${HOME}/Library/Application Support/mkcert/rootCA.pem` and `rootCA-key.pem`
- DNS configured: `*.local.dev-gutools.co.uk` → your machine IP
- Nothing listening locally on port 443

## Step 1: Build and Start the Proxy

```bash
docker build -t devcontainer-nginx:local .

docker-compose up -d devcontainer-nginx
docker logs devcontainer-nginx
```

On first start, the container uses the mounted mkcert root CA files (`rootCA.pem` and `rootCA-key.pem`) from `${HOME}/Library/Application Support/mkcert` to generate a wildcard certificate for `*.local.dev-gutools.co.uk`. Because that CA is already trusted by your system, all `https://` URLs work without any extra flags.

## Step 2: Start Your Application

Start any container with a reasonable name (no explicit labels required):

```bash
docker run -d --name myapp \
  -p 3000:3000 \
  your-app:latest
```

Supported containers:
- Any container with a name and a single port
- Any dev container with a name and a single port
- Any dev container with `portsAttributes`

## Step 3: Open the Homepage

Open: `https://devcontainer-nginx.local.dev-gutools.co.uk/` or `http://devcontainer-nginx.local.dev-gutools.co.uk/`

This shows:
- All discovered containers with active routes
- Links to your applications (open in new tab)
- Nginx configuration details

## Step 4: Access Your Application

Open `https://myapp.local.dev-gutools.co.uk` (or `http://myapp.local.dev-gutools.co.uk`) in your browser, or click either link on the homepage.

---

**That's it.** No configuration, no labels, no code changes needed.

## Using with VS Code Dev Containers

### devcontainer.json

```json
{
  "name": "My Project",
  "image": "node:18",
  "forwardPorts": [3000]
}
```

Open in Dev Containers → automatically routed to `https://my-project.local.dev-gutools.co.uk`

### With Multiple Ports

```json
{
  "forwardPorts": [9100, 1234],
  "portsAttributes": {
    "9100": { "label": "myApp" },
    "1234": { "label": "myApp-frontend" }
  }
}
```

This creates routes for:
- `myapp.<base-domain>`
- `myapp-frontend.<base-domain>`

## Using with Docker Compose

```yaml
services:
  app:
    image: node:18
    expose:
      - "3000"
```

Then access: `https://app.local.dev-gutools.co.uk` (automatically discovered)

## Troubleshooting

### Container not appearing

1. Check proxy logs:
   ```bash
   docker logs devcontainer-nginx | grep -i myapp
   ```

2. Verify container metadata:
   ```bash
   docker inspect myapp | jq '.Config.Labels'
   docker inspect myapp | jq '.Config.ExposedPorts'
   docker inspect myapp | jq '.NetworkSettings.Networks'
   ```

3. Check port selection:
   ```bash
   docker inspect myapp | jq '.Config.ExposedPorts, .HostConfig.PortBindings'
   ```

### TLS/Certificate errors

```bash
docker exec devcontainer-nginx ls -la /etc/nginx/certs/
curl -v --resolve myapp.local.dev-gutools.co.uk:443:127.0.0.1 https://myapp.local.dev-gutools.co.uk
```

### Connectivity issues

```bash
docker exec devcontainer-nginx curl -v http://myapp:3000
docker inspect devcontainer-nginx | jq '.NetworkSettings.Networks'
```
