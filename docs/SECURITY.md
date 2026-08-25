# Security Considerations

## Docker Socket Access

The Docker socket is highly privileged. The proxy handles this securely:

- **Read-only mounting**: `/var/run/docker.sock:/var/run/docker.sock:ro`
- **Limited operations**: Only inspects containers; does not create, start, stop, or exec into containers
- **No container modification**: The proxy discovers existing state without modifying it

### Docker Permissions Required

The proxy uses the Docker API for:
- Listing containers
- Inspecting container metadata (labels, ports, networks)
- Monitoring events (container start, stop, network changes)

These operations require read-only access to:
- Container metadata
- Event stream
- Network configuration

### Advanced: Docker Socket Proxy

For additional isolation, consider using a Docker socket proxy that restricts the proxy to only necessary operations. This adds a security layer but is not required for basic functionality.

## TLS/Certificate Security

- **Private keys never logged**: The proxy does not log certificate contents
- **Read-only certificate mounts**: Certificates mounted as read-only
- **mkcert support**: Use trusted local CAs for development

### Certificate Management

```bash
# Generate certificates using mkcert
mkcert -install
mkcert "*.local.dev-gutools.co.uk"

# Mount in docker-compose.yml
volumes:
  - "./certs:/etc/nginx/certs:ro"
```

**Important**: Keep private keys secure and never commit them to version control.

## Nginx Security

- **TLS termination**: All communication between developer and proxy is encrypted
- **Application containers**: Communicate with proxy over Docker networks or localhost (no direct TLS required)
- **No secret exposure**: Credentials are not logged or exposed

## Container Identification

The proxy uses conservative heuristics for container identification:

1. Prefers explicit metadata labels
2. Falls back to reasonable naming conventions
3. Excludes known system containers

This means:
- Not every container is proxied
- System containers (nginx, postgres, redis) are excluded
- Containers without clear metadata are skipped

## Network Isolation

By default, containers are only reachable through the proxy:

- Proxy connects to application containers via Docker networks or localhost
- Applications do not require incoming TLS termination
- Network access is controlled by Docker networking

## Secrets and Environment Variables

- Proxy does not access application environment variables
- Application secrets are not logged
- Proxy only reads container metadata (labels, ports, networks)

## Logging

The proxy logs:
- Container discovery events
- Route generation
- Nginx configuration updates
- Error conditions

The proxy **does not log**:
- Private keys
- Application secrets
- Full container environment variables
- Docker socket paths

## Recommendations

1. **Mount Docker socket read-only** (default in examples)
2. **Keep certificates local** (never commit to git)
3. **Use mkcert for development** (trusted local CA)
4. **Monitor proxy logs** for unauthorized access attempts
5. **Restrict Docker daemon access** at the host level
6. **Use network policies** to limit application exposure
7. **Consider socket proxy** for additional isolation in shared environments

## Compliance Notes

- No personal data processed
- No external API calls
- All operations local to the Docker daemon
- Suitable for development workstations
- Not recommended for production without additional security measures

## Troubleshooting Security Issues

### Verify read-only socket mount

```bash
docker inspect devcontainer-nginx | jq '.Mounts[] | select(.Source | contains("docker.sock"))'
```

Expected output includes `"RW": false`

### Check certificate permissions

```bash
docker exec devcontainer-nginx ls -la /etc/nginx/certs/
```

Should show read-only permissions (r--r--r--)

### Review logs for sensitive data

```bash
docker logs devcontainer-nginx | grep -i password
docker logs devcontainer-nginx | grep -i secret
```

Should return no results.
