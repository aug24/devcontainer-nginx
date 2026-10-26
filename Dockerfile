# Multi-stage Dockerfile for devcontainer-nginx

# Stage 1: Builder
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git

# Copy source
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o devcontainer-nginx ./cmd

# Stage 2: Runtime
FROM mcr.microsoft.com/devcontainers/base:ubuntu26.04

ENV DEBIAN_FRONTEND=noninteractive

# Install required tools
RUN apt-get update \
    && apt-get install -y --no-install-recommends nginx openssl curl ca-certificates mkcert awscli \
    && ARCH="$(uname -m)" \
    && case "$ARCH" in \
        x86_64) PLUGIN_ARCH="ubuntu_64bit" ;; \
        aarch64|arm64) PLUGIN_ARCH="ubuntu_arm64" ;; \
        *) echo "Unsupported architecture for Session Manager plugin: $ARCH" >&2; exit 1 ;; \
    esac \
    && curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/${PLUGIN_ARCH}/session-manager-plugin.deb" -o /tmp/session-manager-plugin.deb \
    && dpkg -i /tmp/session-manager-plugin.deb \
    && rm -f /tmp/session-manager-plugin.deb \
    && rm -rf /var/lib/apt/lists/*

# Copy built binary
COPY --from=builder /build/devcontainer-nginx /usr/local/bin/

# Copy nginx config
COPY nginx/nginx.conf /etc/nginx/nginx.conf
COPY entrypoint.sh /entrypoint.sh
COPY scripts/host-tunnel /usr/local/bin/host-tunnel
COPY scripts/shell /usr/local/bin/shell
COPY static /usr/share/devcontainer-nginx/static

# Create required directories
RUN mkdir -p /etc/nginx/conf.d /etc/nginx/certs /var/log/nginx \
    /var/cache/nginx /var/log/devcontainer-nginx \
    && chown -R www-data:www-data /var/log/nginx /var/cache/nginx /var/log/devcontainer-nginx \
    && chmod 755 /var/log/devcontainer-nginx \
    && chmod +x /entrypoint.sh /usr/local/bin/host-tunnel /usr/local/bin/shell

# Create baseline generated config
RUN echo "# Generated configuration (empty on startup)" > /etc/nginx/conf.d/generated.conf

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD curl --silent --show-error --fail --insecure https://127.0.0.1/health >/dev/null || exit 1

# Expose HTTPS port
EXPOSE 443

# Run both Nginx and discovery controller
ENTRYPOINT ["/entrypoint.sh"]
