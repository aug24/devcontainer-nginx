# Configuration Reference

## Environment variables

The proxy supports the following settings, with defaults shown below:

```bash
BASE_DOMAIN=local.dev-gutools.co.uk
DOCKER_SOCKET=/var/run/docker.sock
NGINX_CONFIG_PATH=/etc/nginx/conf.d/generated.conf
TLS_CERT=/etc/nginx/certs/wildcard.crt
TLS_KEY=/etc/nginx/certs/wildcard.key
LOCAL_CA_CERT=/etc/nginx/mkcert/rootCA.pem
LOCAL_CA_KEY=/etc/nginx/mkcert/rootCA-key.pem
DEBOUNCE_MS=500
LOG_LEVEL=info
```

### BASE_DOMAIN

Base domain used when generating application hostnames.

Default:

```text
local.dev-gutools.co.uk
```

Example:

```text
myapp.local.dev-gutools.co.uk
```

### DOCKER_SOCKET

Path to the Docker socket used by the Go controller.

Default:

```text
/var/run/docker.sock
```

### NGINX_CONFIG_PATH

Location for the dynamically generated config file.

Default:

```text
/etc/nginx/conf.d/generated.conf
```

### TLS_CERT and TLS_KEY

Paths to the certificate and private key that Nginx presents to clients.

Defaults:

```text
/etc/nginx/certs/wildcard.crt
/etc/nginx/certs/wildcard.key
```

If these files are absent at startup, the entrypoint tries to generate a wildcard certificate for `*.${BASE_DOMAIN}` using the local mkcert CA if it is available. Otherwise it falls back to a self-signed cert.

### LOCAL_CA_CERT and LOCAL_CA_KEY

Used by the entrypoint when generating a local wildcard cert with `mkcert`.

Defaults:

```text
/etc/nginx/mkcert/rootCA.pem
/etc/nginx/mkcert/rootCA-key.pem
```

### DEBOUNCE_MS

Debounce period for Docker event processing.

Default:

```text
500
```

This coalesces multiple Docker events into a single reconciliation pass.

### LOG_LEVEL

Currently informational and used for runtime logging configuration. The default is `info` but the project is primarily driven by the Go logger.

## Docker Compose example

```yaml
services:
  devcontainer-nginx:
    build: .
    image: devcontainer-nginx:local
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - "${HOME}/Library/Application Support/mkcert:/etc/nginx/mkcert:ro"
      - "./certs:/etc/nginx/certs:ro"
    environment:
      BASE_DOMAIN: "local.dev-gutools.co.uk"
      DOCKER_SOCKET: "/var/run/docker.sock"
      NGINX_CONFIG_PATH: "/etc/nginx/conf.d/generated.conf"
      TLS_CERT: "/etc/nginx/certs/wildcard.crt"
      TLS_KEY: "/etc/nginx/certs/wildcard.key"
      LOCAL_CA_CERT: "/etc/nginx/mkcert/rootCA.pem"
      LOCAL_CA_KEY: "/etc/nginx/mkcert/rootCA-key.pem"
      DEBOUNCE_MS: "500"
    restart: unless-stopped
```

## DNS and local hostnames

The proxy does not manage DNS itself. You must make the generated hostnames resolve to your local machine.

Typical options:

- wildcard DNS record such as `*.local.dev-gutools.co.uk -> <your-host-ip>`
- `/etc/hosts` entries for local testing

Example:

```bash
127.0.0.1 myapp.local.dev-gutools.co.uk
127.0.0.1 devcontainer-nginx.local.dev-gutools.co.uk
```

## Homepage and debug endpoints

The proxy exposes a diagnostics homepage at:

```text
https://devcontainer-nginx.<base-domain>/
```

Available endpoints:

- `/nginx.conf` — base nginx config
- `/generated.conf` — generated route config
- `/nginx/config` — combined nginx output for debugging
- `/containers` — discovered container list
- `/container/{name}` — per-container server blocks
- `/tunnels` — tunnel management UI

Examples:

```bash
curl -k https://devcontainer-nginx.local.dev-gutools.co.uk/containers
curl -k https://devcontainer-nginx.local.dev-gutools.co.uk/generated.conf
```

## Optional container metadata

Labels are not required, but they can improve the generated route set.

### devcontainer.hostname

```bash
docker run -d \
  --name myapp \
  --label devcontainer.hostname=custom-name \
  -p 3000:3000 \
  your-app:latest
```

This produces:

```text
custom-name.local.dev-gutools.co.uk
```

### devcontainer.application.port

```bash
docker run -d \
  --name myapp \
  --label devcontainer.application.port=3000 \
  -p 3000:3000 \
  -p 8080:8080 \
  your-app:latest
```

This tells the proxy which port to prefer when several ports are exposed.

### portsAttributes (Dev Containers)

```json
{
  "forwardPorts": [9100, 1234],
  "portsAttributes": {
    "9100": { "label": "backend" },
    "1234": { "label": "frontend" }
  }
}
```

Each labeled forward port can become a separate route.

When a devcontainer runs multiple services, Docker may expose several ports but not tell the proxy which ones are the meaningful application endpoints. The `forwardPorts` list tells the proxy which ports are intentionally exposed for local access, and `portsAttributes` assigns each port a human-readable label such as `backend` or `frontend`. Without this metadata, a multi-service container can look ambiguous: the proxy may only pick one port, choose the wrong port, or skip the container entirely.

This is especially useful for devcontainers that host separate web apps or APIs behind one container, where each service needs its own hostname and TLS route on the host machine rather than a single shared endpoint.

## HTTPS upstream verification

When a tunnel uses an HTTPS upstream, the generated Nginx route can be configured to validate the upstream certificate.

- default: `verifySSL=true`
- explicit opt-out: `verifySSL=false`
- if the protocol is not HTTPS, verification is always disabled

The verification name is taken from the configured ALB/backend hostname so NGINX validates against the correct certificate identity, not the local presentation alias.

## Health checks

The container exposes a health endpoint at:

```text
https://devcontainer-nginx.<base-domain>/health
```

The app also provides a Docker health check that confirms Nginx is serving traffic.

````
This is the description of what the code block changes:
<changeDescription>
Explain why a devcontainer's forwardPorts/portsAttributes block is needed for multi-service containers so each host service can get its own route and label.
</changeDescription>

This is the code block that represents the suggested code change:
````markdown
### portsAttributes (Dev Containers)

```json
{
  "forwardPorts": [9100, 1234],
  "portsAttributes": {
    "9100": { "label": "backend" },
    "1234": { "label": "frontend" }
  }
}
```

Each labeled forward port can become a separate route.

When a devcontainer runs multiple services, Docker may expose several ports but not tell the proxy which ones are the meaningful application endpoints. The `forwardPorts` list tells the proxy which ports are intentionally exposed for local access, and `portsAttributes` assigns each port a human-readable label such as `backend` or `frontend`. Without this metadata, a multi-service container can look ambiguous: the proxy may only pick one port, choose the wrong port, or skip the container entirely.

This is especially useful for devcontainers that host separate web apps or APIs behind one container, where each service needs its own hostname and TLS route on the host machine rather than a single shared endpoint.

````
<userPrompt>
Provide the fully rewritten file, incorporating the suggested code change. You must produce the complete file.
</userPrompt>
