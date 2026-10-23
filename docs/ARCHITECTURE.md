# Architecture

## System overview

```text
                      DNS
                       │
        *.local.dev-gutools.co.uk
                       │
                       ▼
                Developer machine
                 :80 / :443
                       │
                       ▼
          ┌──────────────────────────┐
          │ devcontainer-nginx       │
          │ - Nginx TLS terminator   │
          │ - Go discovery service  │
          │ - route generator        │
          │ - tunnel UI             │
          └──────────────┬──────────┘
                         │
                         ▼
                Docker Engine / sockets
                         │
          ┌──────────────┼───────────────┐
          ▼              ▼               ▼
     app container  devcontainer      another app
      myapp:3000     api:8000       frontend:5173
```

## Single-container design

The project runs as one container that contains:

- Nginx
- a Go controller process
- both the generated config and the route discovery logic
- the optional tunnel management UI

## Components

```text
devcontainer-nginx/
├── cmd/
│   ├── main.go                     # startup, reconciliation, homepage server
│   ├── homepage.go                 # diagnostics and HTML rendering
│   ├── tunnel_ui.go                # tunnel UI HTML and JS
│   ├── tunnels.go                  # tunnel request lifecycle and route generation
│   └── ...
├── internal/
│   ├── discovery/
│   │   ├── docker.go               # Docker API client
│   │   ├── identifier.go           # devcontainer detection heuristics
│   │   └── resolver.go             # network/port resolution
│   └── nginx/
│       └── config.go               # NGINX config generation and validation
├── entrypoint.sh                   # certificate bootstrapping and process startup
├── docker-compose.yml              # local dev setup
└── README.md
```

## Discovery workflow

When the proxy starts, it:

1. reads the Docker socket
2. lists currently running containers
3. inspects each container's metadata
4. classifies likely dev targets
5. resolves a route target (shared network, published port, or forwarded port)
6. generates a route entry and writes NGINX config
7. validates and reloads NGINX

## Container identification

The implementation uses conservative heuristics rather than requiring app changes:

1. explicit Dev Container metadata when present
2. Docker Compose metadata when present
3. ordinary container naming patterns
4. system container exclusions

The discovery layer keeps the workflow permissive while avoiding obvious non-app containers.

## Port selection logic

The resolver prefers the best available route target in this order:

1. explicit `devcontainer.application.port`
2. forwarded ports from devcontainer metadata
3. other published or exposed ports
4. common web ports as a fallback
5. skip containers that are still ambiguous

## Hostname generation

Generated hostnames are based on:

- `devcontainer.hostname` if set
- Compose project naming when available
- a sanitized container name otherwise

The final route is built as:

```text
<hostname>.<base-domain>
```

## NGINX config generation

The config generator produces:

- default HTTP and HTTPS listeners
- one upstream per route
- proxy blocks for each hostname
- websocket-friendly proxy headers
- TLS certificate settings for the external listener
- optional upstream certificate verification settings

The generator deduplicates by hostname and replaces stale entries with newer route data when duplicate names appear.

## HTTPS upstream verification

For tunnel routes that use HTTPS upstreams, the config generator emits:

- `proxy_ssl_server_name on;`
- `proxy_ssl_name <backend-hostname>;`
- `proxy_ssl_verify on|off;`
- `proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;` when verification is enabled

This keeps certificate checks secure by default while allowing explicit opt-out with the tunnel form checkbox.

## Event-driven updates

The app watches Docker events and re-runs reconciliation after a debounce period to avoid churn from rapid startup and shutdown events.

## Security model

- Docker socket is mounted read-only
- the proxy only reads metadata and does not mutate containers
- it avoids logging sensitive values like certificate contents or app secrets
- generated config is validated before reload

## Operational behavior

When config changes, the process:

1. builds a fresh route set
2. writes the config atomically
3. runs `nginx -t`
4. reloads nginx only if validation passes
5. keeps the latest valid config in place if validation fails

## Tunnel flow

Tunnels are represented as `TunnelRequest` / `TunnelEntry` objects and converted into nginx routes via `TunnelManager.NginxRoutes()`. The route includes:

- hostname
- local port
- upstream protocol
- `VerifySSL`
- `VerifyName` (the ALB or upstream host to validate against)

This is how the UI checkbox and generated config remain aligned with the actual HTTPS certificate verification behavior.
