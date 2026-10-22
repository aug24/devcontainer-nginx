package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dev-tools/devcontainer-nginx/internal/discovery"
	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

func main() {
	logDir := "/var/log/devcontainer-nginx"
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory %s: %v\n", logDir, err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(filepath.Join(logDir, "devcontainer-nginx.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

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

	dockerClient, err := discovery.NewDockerClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()

	configGen := nginx.NewConfigGenerator(*nginxConfigPath, *tlsCert, *tlsKey)
	homepageState := &DiagnosticsState{}
	tunnelManager := NewTunnelManager()
	startHomepageServer(ctx, "/etc/nginx/nginx.conf", *nginxConfigPath, configGen, homepageState, tunnelManager, baseDomain)

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

	log.Println("Performing initial reconciliation")
	reconcileRoutes(ctx, dockerClient, identifier, resolver, configGen, homepageState, proxyID, *baseDomain, tunnelManager)

	go watchEvents(ctx, dockerClient, identifier, resolver, configGen, homepageState, proxyID, *baseDomain, tunnelManager, time.Duration(*debounceMs)*time.Millisecond)

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
	homepageState *DiagnosticsState,
	proxyID string,
	baseDomain string,
	tunnelManager *TunnelManager,
) {
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		log.Printf("ERROR: Failed to list containers: %v\n", err)
		return
	}

	log.Printf("Discovered %d running containers\n", len(containers))

	var routes []nginx.Route
	var homepageRows []ContainerDiagnostic
	for _, container := range containers {
		// Inspect provides richer/normalized metadata than list output.
		containerInfo, err := dockerClient.InspectContainer(ctx, container.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v\n", container.Name, err)
			homepageRows = append(homepageRows, ContainerDiagnostic{
				Name:   container.Name,
				Status: "skipped",
				Reason: fmt.Sprintf("inspect failed: %v", err),
			})
			continue
		}

		if isProxyContainer(containerInfo, proxyID) {
			homepageRows = append(homepageRows, ContainerDiagnostic{
				Name:     containerInfo.Name,
				Status:   "skipped",
				Reason:   "proxy container",
				Metadata: summarizeContainerMetadata(containerInfo),
			})
			continue
		}

		containerRoutes, diag := diagnoseContainer(containerInfo, identifier, resolver, baseDomain)
		homepageRows = append(homepageRows, diag)
		routes = append(routes, containerRoutes...)
	}

	if tunnelManager != nil {
		routes = append(routes, tunnelManager.NginxRoutes(baseDomain)...)
	}

	homepageState.Set(homepageRows)

	// Always expose a stable homepage hostname for nginx config inspection.
	routes = append(routes, nginx.Route{
		Hostname:   "devcontainer-nginx." + baseDomain,
		TargetHost: "127.0.0.1",
		TargetPort: 18080,
	})

	// Generate and update config
	if err := configGen.ApplyRoutes(routes); err != nil {
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

func startHomepageServer(ctx context.Context, nginxBasePath, generatedConfigPath string, configGen *nginx.ConfigGenerator, homepageState *DiagnosticsState, tunnelManager *TunnelManager, baseDomain *string) {
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

	mux.HandleFunc("/logs", tunnelManager.Logs)

	mux.HandleFunc("/homepage/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "%d", homepageState.Revision())
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		rows := homepageState.Snapshot()
		rows = append(rows, tunnelDiagnostics(tunnelManager, *baseDomain)...)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderHomepageHTML(rows, homepageState.Revision())))
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

	mux.HandleFunc("/aws-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(renderAWSConfigPage(tunnelManager.AWSConfigSnapshot())))
			return
		}
		if r.Method == http.MethodPost {
			defer r.Body.Close()
			var cfg AWSConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := tunnelManager.SetAWSConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tunnelManager.AWSConfigSnapshot())
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/aws-config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tunnelManager.AWSConfigSnapshot())
		case http.MethodPost:
			defer r.Body.Close()
			var cfg AWSConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := tunnelManager.SetAWSConfig(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tunnelManager.AWSConfigSnapshot())

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/tunnels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderTunnelPage(tunnelManager.AWSConfigSnapshot(), tunnelManager.Snapshot())))
	})

	mux.HandleFunc("/api/tunnels", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tunnelManager.Snapshot())
		case http.MethodPost:
			defer r.Body.Close()
			var request TunnelRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(request.Profile) == "" {
				request.Profile = tunnelManager.AWSConfigSnapshot().Profile
			}
			if strings.TrimSpace(request.Region) == "" {
				request.Region = tunnelManager.AWSConfigSnapshot().Region
			}
			entry, err := tunnelManager.Create(request)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := tunnelManager.Start(entry.ID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			tunnelManager.EnsureConfig(*baseDomain, configGen)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entry)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/tunnels/stop/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/tunnels/stop/")
		if id == "" {
			http.Error(w, "missing tunnel id", http.StatusBadRequest)
			return
		}
		if err := tunnelManager.Stop(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		tunnelManager.EnsureConfig(*baseDomain, configGen)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/tunnels/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/tunnels/")
		if id == "" {
			http.Error(w, "missing tunnel id", http.StatusBadRequest)
			return
		}
		if !tunnelManager.Remove(id) {
			http.Error(w, "tunnel not found", http.StatusNotFound)
			return
		}
		tunnelManager.EnsureConfig(*baseDomain, configGen)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/shell-command-generator", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderCommandGeneratorPage()))
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
			log.Printf("Homepage server shutdown error: %v", err)
		}
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Homepage server error: %v", err)
		}
	}()
}

func watchEvents(
	ctx context.Context,
	dockerClient *discovery.DockerClient,
	identifier *discovery.Identifier,
	resolver *discovery.NetworkResolver,
	configGen *nginx.ConfigGenerator,
	homepageState *DiagnosticsState,
	proxyID string,
	baseDomain string,
	tunnelManager *TunnelManager,
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
				reconcileRoutes(ctx, dockerClient, identifier, resolver, configGen, homepageState, proxyID, baseDomain, tunnelManager)
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

func (tunnelManager *TunnelManager) Logs(w http.ResponseWriter, r *http.Request) {
	logPath := filepath.Join("/var/log/devcontainer-nginx", "devcontainer-nginx.log")
	count := 40
	if q := r.URL.Query().Get("n"); q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed > 0 {
			count = parsed
		}
	}
	if count > 200 {
		count = 200
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read %s: %v", logPath, err), http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	out := strings.Join(lines, "\n")
	if out != "" {
		out += "\n"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(out))
}
