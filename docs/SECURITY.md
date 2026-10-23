# Security Considerations

## Docker socket access

The proxy mounts the Docker socket read-only:

```yaml
- /var/run/docker.sock:/var/run/docker.sock:ro
```

The application only inspects Docker state; it does not create or mutate containers. This keeps the discovery flow narrow and avoids unnecessary privileged operations.

## TLS and certificate handling

The proxy terminates TLS for the developer-facing hostnames and exposes a certificate path controlled by environment variables.

- `TLS_CERT` — public certificate
- `TLS_KEY` — private key
- `LOCAL_CA_CERT` / `LOCAL_CA_KEY` — mkcert CA files used for local certificate generation

If certs are missing, the entrypoint can generate a wildcard certificate for the configured base domain using a local mkcert root. If no local CA is available, it falls back to a self-signed certificate.

### Important notes

- private keys are never intended to be committed to git
- certs should be mounted read-only from a trusted local directory
- for browser trust, keep the CA trusted by the OS/browser

## NGINX security

The generated config validates configuration before reload and only updates routes if the NGINX config passes a syntax check.

For HTTPS upstreams, verification can be enabled or disabled explicitly. The secure default is to verify the upstream certificate, and the route generator sets a proper trust bundle when verification is on.

## Container identification safety

The project uses conservative heuristics to pick app containers:

1. explicit container metadata when present
2. reasonable naming patterns
3. common web port inference
4. exclusion of obvious system containers

This avoids broad, unsafe routing of unrelated containers.

## Network isolation

The proxy connects to app containers over Docker networking or localhost; the application containers do not need direct incoming TLS exposure.

This keeps developer-facing traffic routed through the proxy instead of exposing extra ports or requiring custom network wiring.

## Logging and data handling

The project logs discovery and route updates but avoids logging private key material or full app secrets. The project does not inspect app environment variables or read arbitrary secrets from running services.

## Recommendations

1. keep the Docker socket read-only
2. use a trusted local CA for development domains
3. prefer the default HTTPS certificate verification for ALB-backed tunnels
4. review proxy logs for unexpected route changes or repeated failures
5. keep the host OS/browser CA trust updated

## Troubleshooting security concerns

### Verify the socket is read-only

```bash
docker inspect devcontainer-nginx | jq '.Mounts[] | select(.Source | contains("docker.sock"))'
```

Expected output includes `"RW": false`.

### Review the generated certificate files

```bash
docker exec devcontainer-nginx ls -la /etc/nginx/certs/
```

### Review logs safely

```bash
docker logs devcontainer-nginx | grep -iE 'password|secret|token|key'
```

This should normally return no results.
