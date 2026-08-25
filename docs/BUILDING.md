# Building devcontainer-nginx

## Local Go Build

To build the binary locally:

```bash
go mod download
go build -o devcontainer-nginx ./cmd
```

## Docker Build

To build the Docker image:

```bash
docker build -t devcontainer-nginx:local .
```

## Dependencies

The project uses:
- `github.com/docker/docker` - Docker API client
- `github.com/docker/go-connections` - Docker connection utilities

**Note:** Due to Go module versioning complexities with docker/docker (which uses v20+ versioning), you may encounter dependency resolution issues. Workarounds:

1. Use the Docker image build (recommended)
2. Use `go build -mod=mod` to let Go resolve dependencies
3. Use a Docker v1.x compatible version

## Using the Pre-built Image

The easiest way to deploy devcontainer-nginx is via Docker:

```bash
docker-compose up -d devcontainer-nginx
```
