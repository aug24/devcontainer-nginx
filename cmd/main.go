package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dev-tools/devcontainer-nginx/internal/discovery"
	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

func main() {
	baseDomain := flag.String("base-domain", "local.dev-gutools.co.uk", "Base domain for generated hostnames")
	dockerSocket := flag.String("docker-socket", "/var/run/docker.sock", "Path to Docker socket")
	nginxConfigPath := flag.String("nginx-config", "/etc/nginx/conf.d/generated.conf", "Path where to write generated Nginx config")
	tlsCert := flag.String("tls-cert", "/etc/nginx/certs/wildcard.crt", "Path to TLS certificate")
	tlsKey := flag.String("tls-key", "/etc/nginx/certs/wildcard.key", "Path to TLS private key")
	debounceMs := flag.Int("debounce", 500, "Event debounce period in milliseconds")
	flag.Parse()

	log.Println("Starting devcontainer-nginx discovery controller")

	if os.Getenv("DOCKER_HOST") == "" && *dockerSocket != "" {
		socket := *dockerSocket
		if !strings.HasPrefix(socket, "unix://") {
			socket = "unix://" + socket
		}
		if err := os.Setenv("DOCKER_HOST", socket); err != nil {
			log.Fatalf("Failed to set DOCKER_HOST: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create Docker client
	dockerClient, err := discovery.NewDockerClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Create Nginx config generator
	configGen := nginx.NewConfigGenerator(*nginxConfigPath, *tlsCert, *tlsKey)
	diagnosticsState := &DiagnosticsState{}
	startDiagnosticsServer(ctx, "/etc/nginx/nginx.conf", *nginxConfigPath, configGen, diagnosticsState)

	// Create identifier and resolver
	identifier := discovery.NewIdentifier()
	proxyID := os.Getenv("PROXY_CONTAINER_ID")
	if proxyID == "" {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			proxyID = hostname
		}
	}
	if proxyID == "" {
		proxyID = "unknown"
	}
	resolver := discovery.NewNetworkResolver(proxyID)

	if proxyID != "unknown" {
		proxyInfo, err := dockerClient.InspectContainer(ctx, proxyID)
		if err != nil {
			log.Printf("WARNING: Failed to inspect proxy container %q: %v", proxyID, err)
		} else if len(proxyInfo.NetworkSettings) > 0 {
			resolver.SetProxyNetworks(proxyInfo.NetworkSettings)
		} else {
			log.Printf("WARNING: Proxy container %q has no inspectable networks", proxyID)
		}
	}

	// Perform initial reconciliation
	log.Println("Performing initial reconciliation")
	reconcileRoutes(ctx, dockerClient, identifier, resolver, configGen, diagnosticsState, proxyID, *baseDomain)

	// Start event watch in background
	go watchEvents(ctx, dockerClient, identifier, resolver, configGen, diagnosticsState, proxyID, *baseDomain, time.Duration(*debounceMs)*time.Millisecond)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	<-sigChan
	log.Println("Received shutdown signal")
	cancel()
}

func reconcileRoutes(
	ctx context.Context,
	dockerClient *discovery.DockerClient,
	identifier *discovery.Identifier,
	resolver *discovery.NetworkResolver,
	configGen *nginx.ConfigGenerator,
	diagnosticsState *DiagnosticsState,
	proxyID string,
	baseDomain string,
) {
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		log.Printf("ERROR: Failed to list containers: %v\n", err)
		return
	}

	log.Printf("Discovered %d running containers\n", len(containers))

	var routes []nginx.Route
	var diagnostics []ContainerDiagnostic
	for _, container := range containers {
		// Inspect provides richer/normalized metadata than list output.
		containerInfo, err := dockerClient.InspectContainer(ctx, container.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v\n", container.Name, err)
			diagnostics = append(diagnostics, ContainerDiagnostic{
				Name:   container.Name,
				Status: "skipped",
				Reason: fmt.Sprintf("inspect failed: %v", err),
			})
			continue
		}

		if isProxyContainer(containerInfo, proxyID) {
			diagnostics = append(diagnostics, ContainerDiagnostic{
				Name:     containerInfo.Name,
				Status:   "skipped",
				Reason:   "proxy container",
				Metadata: summarizeContainerMetadata(containerInfo),
			})
			continue
		}

		containerRoutes, diag := diagnoseContainer(containerInfo, identifier, resolver, baseDomain)
		diagnostics = append(diagnostics, diag)
		routes = append(routes, containerRoutes...)
	}

	diagnosticsState.Set(diagnostics)

	// Always expose a stable diagnostics hostname for nginx config inspection.
	routes = append(routes, nginx.Route{
		Hostname:   "devcontainer-nginx." + baseDomain,
		TargetHost: "127.0.0.1",
		TargetPort: 18080,
	})

	// Generate and update config
	config := configGen.GenerateConfig(routes)
	if err := configGen.WriteConfig(config); err != nil {
		log.Printf("ERROR: Failed to write config: %v\n", err)
		return
	}

	if !configGen.ValidateConfig() {
		log.Println("ERROR: Configuration validation failed")
		return
	}

	if !configGen.ReloadNginx() {
		log.Println("ERROR: Failed to reload Nginx")
	}

	log.Printf("Successfully updated routes with %d entries\n", len(routes))
}

func startDiagnosticsServer(ctx context.Context, nginxBasePath, generatedConfigPath string, configGen *nginx.ConfigGenerator, diagnosticsState *DiagnosticsState) {
	mux := http.NewServeMux()
	writeConfig := func(w http.ResponseWriter, path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read %s: %v", path, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(content)
	}

	mux.HandleFunc("/nginx.conf", func(w http.ResponseWriter, r *http.Request) {
		writeConfig(w, nginxBasePath)
	})

	mux.HandleFunc("/generated.conf", func(w http.ResponseWriter, r *http.Request) {
		writeConfig(w, generatedConfigPath)
	})

	mux.HandleFunc("/nginx/config", func(w http.ResponseWriter, r *http.Request) {
		baseConfig, err := os.ReadFile(nginxBasePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read %s: %v", nginxBasePath, err), http.StatusInternalServerError)
			return
		}

		generatedConfig, err := os.ReadFile(generatedConfigPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read %s: %v", generatedConfigPath, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(
			w,
			"### %s ###\n%s\n\n### %s ###\n%s\n",
			nginxBasePath,
			string(baseConfig),
			generatedConfigPath,
			string(generatedConfig),
		)
	})

	mux.HandleFunc("/containers", func(w http.ResponseWriter, r *http.Request) {
		names := configGen.ListContainers()
		sort.Strings(names)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderContainersHTML(names)))
	})

	mux.HandleFunc("/container/", func(w http.ResponseWriter, r *http.Request) {
		containerName := strings.TrimPrefix(r.URL.Path, "/container/")
		if containerName == "" {
			http.Error(w, "missing container name", http.StatusBadRequest)
			return
		}
		config := configGen.ContainerRoutes(containerName)
		if config == "" {
			http.Error(w, fmt.Sprintf("no routes found for container %q", containerName), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(config))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderDiagnosticsHTML(diagnosticsState.Snapshot())))
	})

	server := &http.Server{
		Addr:              "127.0.0.1:18080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Diagnostics server shutdown error: %v", err)
		}
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Diagnostics server error: %v", err)
		}
	}()
}

func watchEvents(
	ctx context.Context,
	dockerClient *discovery.DockerClient,
	identifier *discovery.Identifier,
	resolver *discovery.NetworkResolver,
	configGen *nginx.ConfigGenerator,
	diagnosticsState *DiagnosticsState,
	proxyID string,
	baseDomain string,
	debounce time.Duration,
) {
	messages, errors := dockerClient.WatchEvents(ctx)
	ticker := time.NewTicker(debounce)
	defer ticker.Stop()

	needsReconcile := false

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errors:
			log.Printf("ERROR watching events: %v\n", err)
			return
		case <-messages:
			needsReconcile = true
			ticker.Reset(debounce)
		case <-ticker.C:
			if needsReconcile {
				needsReconcile = false
				reconcileRoutes(ctx, dockerClient, identifier, resolver, configGen, diagnosticsState, proxyID, baseDomain)
			}
		}
	}
}

func isProxyContainer(containerInfo *discovery.ContainerInfo, proxyID string) bool {
	if proxyID == "" || proxyID == "unknown" {
		proxyID = ""
	}

	name := strings.TrimPrefix(containerInfo.Name, "/")
	proxyName := strings.TrimPrefix(proxyID, "/")
	if containerInfo.ID == proxyID || name == proxyName {
		return true
	}

	if name == "devcontainer-nginx" {
		return true
	}

	if service, ok := containerInfo.Labels["com.docker.compose.service"]; ok && service == "devcontainer-nginx" {
		return true
	}

	return false
}
