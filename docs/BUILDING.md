# Building devcontainer-nginx

## Local Go build

```bash
go mod download
go build -o devcontainer-nginx ./cmd
```

## Docker build

```bash
docker build -t devcontainer-nginx:local .
```

## Run locally with Docker Compose

```bash
docker compose up -d --build
```

This is the recommended workflow for local development because it mirrors the runtime environment, TLS mount layout, and generated config flow used in the container.

## Dependencies

The project uses the Docker SDK and supporting Go packages, including:

- `github.com/docker/docker`
- `github.com/docker/go-connections`
- standard library packages for HTTP, JSON, and process management

## Helpful notes

- The project is intended to be built and run in a containerized dev environment.
- If dependency resolution is difficult in a raw Go environment, prefer the Docker build path.
- The entrypoint handles certificate generation and startup sequencing automatically when mounted files are absent.

## Building with the project scripts

There are helper scripts in the repository root that are useful for local shell access and host tunnel operations. See the repository root for the scripts directory and its purpose.
