package nginx

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ConfigGenerator generates and manages Nginx configuration.
type ConfigGenerator struct {
	configPath string
	certPath   string
	keyPath    string

	mu     sync.RWMutex
	routes []Route
}

// NewConfigGenerator creates a new config generator.
func NewConfigGenerator(configPath, certPath, keyPath string) *ConfigGenerator {
	return &ConfigGenerator{
		configPath: configPath,
		certPath:   certPath,
		keyPath:    keyPath,
	}
}

// Route represents a route to be proxied.
type Route struct {
	ContainerName string
	Hostname      string
	TargetHost    string
	TargetPort    int
}

// GenerateConfig generates Nginx configuration from routes.
func (cg *ConfigGenerator) GenerateConfig(routes []Route) string {
	cg.mu.Lock()
	cg.routes = make([]Route, len(routes))
	copy(cg.routes, routes)
	cg.mu.Unlock()

	return cg.renderConfig(routes)
}

// renderConfig renders the full nginx config from a slice of routes.
func (cg *ConfigGenerator) renderConfig(routes []Route) string {
	lines := []string{
		"# Auto-generated Nginx configuration",
		"# DO NOT EDIT manually - this file is regenerated",
		"",
	}

	lines = append(lines, cg.generateDefaultServerBlock()...)
	lines = append(lines, "")

	if len(routes) == 0 {
		lines = append(lines, "# No routes configured")
		return strings.Join(lines, "\n")
	}

	// Deduplicate routes by hostname
	seen := make(map[string]bool)
	var uniqueRoutes []Route

	for _, route := range routes {
		if seen[route.Hostname] {
			log.Printf("WARNING: Duplicate hostname %s; skipping duplicate\n", route.Hostname)
			continue
		}
		seen[route.Hostname] = true
		uniqueRoutes = append(uniqueRoutes, route)
	}

	// Generate server blocks
	for _, route := range uniqueRoutes {
		lines = append(lines, cg.generateServerBlock(route)...)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// ListContainers returns the sorted unique container names that currently have routes.
func (cg *ConfigGenerator) ListContainers() []string {
	cg.mu.RLock()
	defer cg.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string
	for _, r := range cg.routes {
		if r.ContainerName != "" && !seen[r.ContainerName] {
			seen[r.ContainerName] = true
			names = append(names, r.ContainerName)
		}
	}
	return names
}

// ContainerRoutes returns the nginx server-block config for all routes belonging to containerName.
// Returns an empty string if no routes are found for that container.
func (cg *ConfigGenerator) ContainerRoutes(containerName string) string {
	cg.mu.RLock()
	defer cg.mu.RUnlock()

	var lines []string
	for _, r := range cg.routes {
		if r.ContainerName == containerName {
			lines = append(lines, cg.generateServerBlock(r)...)
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (cg *ConfigGenerator) generateDefaultServerBlock() []string {
	return []string{
		"# Default HTTP server keeps port 80 listener active even when no routes exist",
		"server {",
		"    listen 80 default_server;",
		"    server_name _;",
		"",
		"    location = /health {",
		"        add_header Content-Type text/plain;",
		"        return 200 'ok';",
		"    }",
		"",
		"    location = /_devcontainer-nginx/config {",
		"        proxy_pass http://127.0.0.1:18080/nginx/config;",
		"        proxy_set_header Host $host;",
		"    }",
		"",
		"    location / {",
		"        add_header Content-Type text/plain;",
		"        return 503 'No route configured';",
		"    }",
		"}",
		"",
		"# Default HTTPS server keeps TLS listener active even when no routes exist",
		"server {",
		"    listen 443 ssl default_server;",
		"    server_name _;",
		"",
		fmt.Sprintf("    ssl_certificate %s;", cg.certPath),
		fmt.Sprintf("    ssl_certificate_key %s;", cg.keyPath),
		"    http2 on;",
		"",
		"    ssl_protocols TLSv1.2 TLSv1.3;",
		"    ssl_ciphers HIGH:!aNULL:!MD5;",
		"    ssl_prefer_server_ciphers on;",
		"",
		"    location = /health {",
		"        add_header Content-Type text/plain;",
		"        return 200 'ok';",
		"    }",
		"",
		"    location = /_devcontainer-nginx/config {",
		"        proxy_pass http://127.0.0.1:18080/nginx/config;",
		"        proxy_set_header Host $host;",
		"    }",
		"",
		"    location / {",
		"        add_header Content-Type text/plain;",
		"        return 503 'No route configured';",
		"    }",
		"}",
	}
}

// generateServerBlock generates HTTP and HTTPS server blocks for a single route.
func (cg *ConfigGenerator) generateServerBlock(route Route) []string {
	upstreamName := cg.sanitizeUpstreamName(route.Hostname)

	proxyLocation := []string{
		"    location / {",
		fmt.Sprintf("        proxy_pass http://%s;", upstreamName),
		"",
		"        # Proxy headers",
		"        proxy_http_version 1.1;",
		"        proxy_set_header Host $host;",
		"        proxy_set_header X-Real-IP $remote_addr;",
		"        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
		"        proxy_set_header X-Forwarded-Proto $scheme;",
		"",
		"        # WebSocket support",
		"        proxy_set_header Upgrade $http_upgrade;",
		"        proxy_set_header Connection \"upgrade\";",
		"",
		"        # Timeouts",
		"        proxy_connect_timeout 60s;",
		"        proxy_send_timeout 60s;",
		"        proxy_read_timeout 60s;",
		"    }",
	}

	lines := []string{
		fmt.Sprintf("# Route: %s -> %s:%d", route.Hostname, route.TargetHost, route.TargetPort),
		fmt.Sprintf("upstream %s {", upstreamName),
		fmt.Sprintf("    server %s:%d;", route.TargetHost, route.TargetPort),
		"}",
		"",
		"# HTTP (port 80)",
		"server {",
		"    listen 80;",
		fmt.Sprintf("    server_name %s;", route.Hostname),
		"",
	}
	lines = append(lines, proxyLocation...)
	lines = append(lines,
		"",
		"    location = /_devcontainer-nginx/config {",
		"        proxy_pass http://127.0.0.1:18080/nginx/config;",
		"        proxy_set_header Host $host;",
		"    }",
		"}",
		"",
		"# HTTPS (port 443)",
		"server {",
		"    listen 443 ssl;",
		fmt.Sprintf("    server_name %s;", route.Hostname),
		"",
		fmt.Sprintf("    ssl_certificate %s;", cg.certPath),
		fmt.Sprintf("    ssl_certificate_key %s;", cg.keyPath),
		"    http2 on;",
		"",
		"    # TLS configuration",
		"    ssl_protocols TLSv1.2 TLSv1.3;",
		"    ssl_ciphers HIGH:!aNULL:!MD5;",
		"    ssl_prefer_server_ciphers on;",
		"",
	)
	lines = append(lines, proxyLocation...)
	lines = append(lines,
		"",
		"    location = /_devcontainer-nginx/config {",
		"        proxy_pass http://127.0.0.1:18080/nginx/config;",
		"        proxy_set_header Host $host;",
		"    }",
		"}",
	)

	return lines
}

// sanitizeUpstreamName sanitizes hostname for use as upstream name.
func (cg *ConfigGenerator) sanitizeUpstreamName(hostname string) string {
	// Replace dots with underscores and hyphens remain
	return "upstream_" + strings.ReplaceAll(hostname, ".", "_")
}

// WriteConfig writes configuration to file atomically.
// Uses temp file + rename pattern to ensure atomicity.
func (cg *ConfigGenerator) WriteConfig(content string) error {
	configDir := filepath.Dir(cg.configPath)
	configFilename := filepath.Base(cg.configPath)

	// Create temp file in same directory for atomic rename
	tmpFile, err := ioutil.TempFile(configDir, fmt.Sprintf(".%s.", configFilename))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()
	tmpPath := tmpFile.Name()

	// Write content to temp file
	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, cg.configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	log.Printf("Wrote Nginx configuration to %s\n", cg.configPath)
	return nil
}

// ValidateConfig validates configuration using nginx -t.
func (cg *ConfigGenerator) ValidateConfig() bool {
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("ERROR: Nginx configuration invalid:\n%s\n", string(output))
		return false
	}

	log.Println("Nginx configuration is valid")
	return true
}

// ReloadNginx gracefully reloads Nginx.
func (cg *ConfigGenerator) ReloadNginx() bool {
	cmd := exec.Command("nginx", "-s", "reload")
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("ERROR: Failed to reload Nginx:\n%s\n", string(output))
		return false
	}

	log.Println("Nginx reloaded successfully")
	return true
}

// ReadCurrentConfig reads the current configuration file.
func (cg *ConfigGenerator) ReadCurrentConfig() (string, error) {
	content, err := ioutil.ReadFile(cg.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read configuration: %w", err)
	}
	return string(content), nil
}
