# Architecture

## System Overview

```
                    DNS
                     │
     *.local.dev-gutools.co.uk
                     │
                     ▼
              Developer Mac
                  :443
                     │
                     ▼
         ┌─────────────────────┐
         │ devcontainer-nginx  │
         ├─────────────────────┤
         │ Nginx (TLS term)    │
         │ Go discovery        │
         └──────────┬──────────┘
                    │
               Docker Engine
                    │
         ┌──────────┼──────────┐
         ▼          ▼          ▼
     DevContainer  DevContainer DevContainer
     myapp:3000    api:8000     frontend:5173
```

## Single Container Design

Everything runs inside one container containing:
- Nginx (TLS termination and reverse proxy)
- Go controller process (Docker discovery and configuration management)

## Component Architecture

```
devcontainer-nginx/
├── cmd/main.go                 # Entry point & orchestration
├── internal/discovery/
│   ├── docker.go              # Docker API client wrapper
│   ├── identifier.go          # Dev Container identification
│   └── resolver.go            # Network connectivity resolution
└── internal/nginx/
    └── config.go              # Nginx configuration generation
```

### Key Components

**main.go** (~260 lines)
- Command-line flag parsing
- Docker client initialization
- Initial container reconciliation
- Event watching loop with debouncing
- Graceful shutdown handling

**docker.go** (~227 lines)
- Docker daemon connection and health checking
- Container listing and inspection
- Event stream monitoring
- Network management
- Container metadata parsing

**identifier.go** (~263 lines)
- Conservative container detection
- Project name extraction from labels
- Hostname sanitization for DNS compliance
- Application port discovery
- Complete metadata validation

**resolver.go** (~173 lines)
- Shared network detection
- Route resolution (container name or published port)
- Docker Desktop `host.docker.internal` support
- Network preference ranking

**config.go** (~192 lines)
- Dynamic server block generation
- Upstream configuration
- TLS certificate setup
- WebSocket proxy configuration
- Atomic config file writing
- Configuration validation with `nginx -t`
- Graceful Nginx reload with `nginx -s reload`

## How Discovery Works

### Container Identification

A container is recognized as a development container using heuristics:

1. **Dev Container metadata labels** (if present): `devcontainer.*` labels indicate explicit intent
2. **Docker Compose labels** (if present): `com.docker.compose.*` labels indicate orchestrated apps
3. **Reasonable naming** (default): containers with sensible names (e.g., `myapp`, `frontend`, `api`) are proxied
4. **System container exclusions**: known system containers (nginx, postgres, redis, etc.) are skipped

No labels are **required**. Containers created with `docker run --name myapp` are automatically discovered and proxied.

### Port Selection

The proxy determines which port to proxy to:

1. **Explicit label** → `devcontainer.application.port`
2. **Dev Container `portsAttributes` labels** → if set, each labeled forwarded port becomes a route
3. **Dev Container forwarded ports** → uses existing metadata
4. **Single exposed port** → if only one TCP port is exposed
5. **Common web ports** → tries 3000, 5173, 8000, 8080, 80 in order
6. **Forwarded-port order fallback** → if multiple forwarded ports are present
7. **Ambiguous** → container is skipped with warning

### Hostname Generation

Generated from:

1. **Hostname label** → `devcontainer.hostname`
2. **Compose project name** → `com.docker.compose.project`
3. **Sanitized container name** → lowercase, hyphens only

Final hostname: `{project-name}.local.dev-gutools.co.uk`

### Network Connectivity

Reaches containers via:

1. **Shared Docker network** (preferred) → direct container-name DNS resolution
2. **Published port** (fallback) → via `host.docker.internal`
3. **Unreachable** → container is skipped

## Nginx Configuration

### Base Configuration

Static configuration (`/etc/nginx/nginx.conf`):
- Worker configuration
- TLS settings
- Gzip compression
- Access logging

### Generated Configuration

Dynamic routes (`/etc/nginx/conf.d/generated.conf`):
- Server blocks for each discovered container
- Upstream configuration
- WebSocket support with proper headers
- Atomic updates with validation

### Safe Updates

When Docker state changes:

1. Discover current eligible containers
2. Resolve their routes
3. Generate a complete temporary configuration
4. Validate with `nginx -t`
5. Atomically replace the generated configuration
6. Gracefully reload Nginx with `nginx -s reload`
7. If validation fails, retain the previous working configuration

## Event-Driven Updates

- **Docker Events**: Uses Docker events API instead of polling
- **Debouncing**: Coalesces rapid events (default 500ms)
- **Full Reconciliation**: After debounce period, performs complete reconciliation
- **Recovery**: On Docker API disconnect, reconnects and reconciles

## TLS/Certificate Handling

- Nginx terminates TLS
- Wildcard certificate mounted from host
- Certificate paths configurable via environment variables
- Support for mkcert-generated certificates

## Security

- Docker socket mounted **read-only**
- Proxy only inspects Docker state; does not create/modify containers
- No secrets logged
- Conservative container detection

## Performance Characteristics

- Event-driven discovery (no polling)
- Debounced configuration updates
- Minimal resource usage: ~30-50MB memory, <1% CPU
- Sub-100ms route generation
- Written in Go for efficiency
