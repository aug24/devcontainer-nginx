package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

type AWSConfig struct {
	Profile          string `json:"profile"`
	Region           string `json:"region"`
	BootstrapCommand string `json:"bootstrapCommand"`
	Output           string `json:"output"`
	ExitCode         int    `json:"exitCode"`
}

type TunnelRequest struct {
	Profile            string `json:"profile"`
	Region             string `json:"region"`
	Stack              string `json:"stack"`
	Stage              string `json:"stage"`
	App                string `json:"app"`
	Alb                string `json:"alb"`
	Name               string `json:"name"`
	CredentialCommands string `json:"credentialCommands"`
	TunnelCommand      string `json:"tunnelCommand"`
	Host               string `json:"host"`
	TargetPort         int    `json:"targetPort"`
	Alias              string `json:"alias"`
	LocalPort          int    `json:"localPort"`
	UpstreamProtocol   string `json:"upstreamProtocol"`
}

type TunnelEntry struct {
	ID                 string    `json:"id"`
	Profile            string    `json:"profile"`
	Region             string    `json:"region"`
	Stack              string    `json:"stack"`
	Stage              string    `json:"stage"`
	App                string    `json:"app"`
	Alb                string    `json:"alb"`
	Name               string    `json:"name"`
	CredentialCommands string    `json:"credentialCommands"`
	TunnelCommand      string    `json:"tunnelCommand"`
	Host               string    `json:"host"`
	TargetPort         int       `json:"targetPort"`
	Alias              string    `json:"alias"`
	LocalPort          int       `json:"localPort"`
	UpstreamProtocol   string    `json:"upstreamProtocol"`
	Status             string    `json:"status"`
	LocalEndpoint      string    `json:"localEndpoint"`
	LastOutput         string    `json:"lastOutput"`
	CreatedAt          time.Time `json:"createdAt"`
}

type TunnelManager struct {
	mu      sync.Mutex
	config  AWSConfig
	tunnels map[string]*TunnelEntry
	process map[string]*exec.Cmd
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		config: AWSConfig{
			Profile: "",
			Region:  "us-east-1",
		},
		tunnels: make(map[string]*TunnelEntry),
		process: make(map[string]*exec.Cmd),
	}
}

func (tm *TunnelManager) SetAWSConfig(cfg AWSConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if strings.TrimSpace(cfg.Profile) == "" {
		cfg.Profile = ""
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}
	cfg.BootstrapCommand = strings.TrimSpace(cfg.BootstrapCommand)
	cfg.Output = strings.TrimSpace(cfg.Output)
	cfg.ExitCode = 0
	if cfg.BootstrapCommand == "" {
		tm.config = cfg
		return nil
	}

	if err := os.MkdirAll("/root/.aws", 0o700); err != nil {
		return fmt.Errorf("failed to prepare aws config directory: %w", err)
	}

	cmd := exec.Command("bash", "-lc", cfg.BootstrapCommand)
	cmd.Env = append(os.Environ(),
		"HOME=/root",
		"AWS_CONFIG_FILE=/root/.aws/config",
		"AWS_SHARED_CREDENTIALS_FILE=/root/.aws/credentials",
	)
	output, err := cmd.CombinedOutput()
	trimmedOutput := strings.TrimSpace(string(output))
	cfg.Output = trimmedOutput
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			cfg.ExitCode = ee.ExitCode()
		} else {
			cfg.ExitCode = 1
		}
		return fmt.Errorf("aws bootstrap command failed (exit %d): %w: %s", cfg.ExitCode, err, trimmedOutput)
	}
	cfg.ExitCode = 0
	cfg.BootstrapCommand = ""
	if err := os.Chmod("/root/.aws", 0o700); err != nil {
		return fmt.Errorf("failed to set aws config directory permissions: %w", err)
	}
	for _, path := range []string{"/root/.aws/config", "/root/.aws/credentials"} {
		if _, statErr := os.Stat(path); statErr == nil {
			if err := os.Chmod(path, 0o600); err != nil {
				return fmt.Errorf("failed to set aws config file permissions for %s: %w", path, err)
			}
		}
	}
	if os.Geteuid() == 0 {
		for _, path := range []string{"/root/.aws", "/root/.aws/config", "/root/.aws/credentials"} {
			if _, statErr := os.Stat(path); statErr == nil {
				if err := os.Chown(path, 0, 0); err != nil {
					return fmt.Errorf("failed to set aws config ownership for %s: %w", path, err)
				}
			}
		}
	}
	tm.config = cfg
	return nil
}

func (tm *TunnelManager) AWSConfigSnapshot() AWSConfig {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	cfg := tm.config
	if strings.TrimSpace(cfg.Profile) == "" {
		cfg.Profile = ""
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}
	return cfg
}

func (tm *TunnelManager) Snapshot() []TunnelEntry {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]TunnelEntry, 0, len(tm.tunnels))
	for _, tunnel := range tm.tunnels {
		out = append(out, *tunnel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (tm *TunnelManager) EnsureConfig(baseDomain string, configGen *nginx.ConfigGenerator) {
	routes := tm.NginxRoutes(baseDomain)
	log.Printf("Tunnel config refresh: baseDomain=%q, tunnelCount=%d, routes=%d", baseDomain, len(tm.tunnels), len(routes))
	for _, route := range routes {
		log.Printf("Tunnel route candidate: hostname=%q target=%s:%d", route.Hostname, route.TargetHost, route.TargetPort)
	}
	if len(routes) == 0 {
		return
	}

	merged := append([]nginx.Route{}, configGen.CurrentRoutes()...)
	seen := make(map[string]struct{}, len(merged))
	for _, route := range merged {
		seen[route.Hostname] = struct{}{}
	}
	for _, route := range routes {
		if _, ok := seen[route.Hostname]; !ok {
			merged = append(merged, route)
		}
	}

	if err := configGen.ApplyRoutes(merged); err != nil {
		log.Printf("failed to write merged tunnel config: %v", err)
	}
	if ok := configGen.ValidateConfig(); !ok {
		log.Printf("Tunnel config validation failed for %d merged routes", len(merged))
	}
	if ok := configGen.ReloadNginx(); !ok {
		log.Printf("Tunnel config reload failed for %d merged routes", len(merged))
	}
}

func (tm *TunnelManager) NginxRoutes(baseDomain string) []nginx.Route {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]nginx.Route, 0, len(tm.tunnels))
	for _, tunnel := range tm.tunnels {
		hostname := strings.TrimSpace(tunnel.Alias)
		if hostname == "" {
			hostname = tunnel.Host
		}
		if !strings.Contains(hostname, ".") && strings.TrimSpace(baseDomain) != "" {
			hostname = hostname + "." + strings.TrimSpace(baseDomain)
		}
		out = append(out, nginx.Route{
			ContainerName: "aws-tunnel-" + tunnel.ID,
			Hostname:      hostname,
			TargetHost:    "127.0.0.1",
			TargetPort:    tunnel.LocalPort,
			Protocol:      tunnel.UpstreamProtocol,
		})
	}
	return out
}

func renderTunnelConfig(routes []nginx.Route, baseDomain string, configGen *nginx.ConfigGenerator) string {
	_ = baseDomain
	lines := []string{
		"# Auto-generated tunnel configuration",
		"# Managed by devcontainer-nginx",
		"",
		"server {",
		"    listen 80 default_server;",
		"    server_name _;",
		"    location = /health {",
		"        add_header Content-Type text/plain;",
		"        return 200 'ok';",
		"    }",
		"    location / {",
		"        add_header Content-Type text/plain;",
		"        return 503 'No route configured';",
		"    }",
		"}",
		"",
		"server {",
		"    listen 443 ssl default_server;",
		"    server_name _;",
		fmt.Sprintf("    ssl_certificate %s;", configGen.CertPath()),
		fmt.Sprintf("    ssl_certificate_key %s;", configGen.KeyPath()),
		"    http2 on;",
		"    ssl_protocols TLSv1.2 TLSv1.3;",
		"    ssl_ciphers HIGH:!aNULL:!MD5;",
		"    ssl_prefer_server_ciphers on;",
		"    location = /health {",
		"        add_header Content-Type text/plain;",
		"        return 200 'ok';",
		"    }",
		"    location / {",
		"        add_header Content-Type text/plain;",
		"        return 503 'No route configured';",
		"    }",
		"}",
		"",
	}
	for _, route := range routes {
		upstreamName := "aws_tunnel_" + strings.NewReplacer(".", "_", "-", "_").Replace(route.Hostname)
		lines = append(lines,
			fmt.Sprintf("upstream %s {", upstreamName),
			fmt.Sprintf("    server 127.0.0.1:%d;", route.TargetPort),
			"}",
			"",
			"server {",
			"    listen 80;",
			fmt.Sprintf("    server_name %s;", route.Hostname),
			"    location / {",
			fmt.Sprintf("        proxy_pass https://%s;", upstreamName),
			"        proxy_ssl_server_name on;",
			"        proxy_ssl_verify off;",
			"        proxy_set_header Host $host;",
			"        proxy_set_header X-Real-IP $remote_addr;",
			"        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
			"        proxy_set_header X-Forwarded-Proto $scheme;",
			"    }",
			"}",
			"",
			"server {",
			"    listen 443 ssl;",
			fmt.Sprintf("    server_name %s;", route.Hostname),
			fmt.Sprintf("    ssl_certificate %s;", configGen.CertPath()),
			fmt.Sprintf("    ssl_certificate_key %s;", configGen.KeyPath()),
			"    location / {",
			fmt.Sprintf("        proxy_pass https://%s;", upstreamName),
			"        proxy_ssl_server_name on;",
			"        proxy_ssl_verify off;",
			"        proxy_set_header Host $host;",
			"        proxy_set_header X-Real-IP $remote_addr;",
			"        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
			"        proxy_set_header X-Forwarded-Proto $scheme;",
			"    }",
			"}",
			"",
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (tm *TunnelManager) Remove(id string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	_, exists := tm.tunnels[id]
	if !exists {
		return false
	}
	if cmd, ok := tm.process[id]; ok {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		delete(tm.process, id)
	}
	delete(tm.tunnels, id)
	return true
}

func (tm *TunnelManager) Stop(id string) error {
	tm.mu.Lock()
	cmd, ok := tm.process[id]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel %s not found", id)
	}
	entry, exists := tm.tunnels[id]
	if !exists {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel %s not found", id)
	}
	entry.Status = "stopping"
	if cmd.Process != nil {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			_ = cmd.Process.Kill()
		}
	}
	delete(tm.process, id)
	tm.mu.Unlock()

	if entry != nil {
		entry.Status = "stopped"
		entry.LastOutput = "tunnel stopped by user"
	}

	return nil
}

func (tm *TunnelManager) Create(request TunnelRequest) (*TunnelEntry, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Profile = strings.TrimSpace(request.Profile)
	request.Region = strings.TrimSpace(request.Region)
	request.Stack = strings.TrimSpace(request.Stack)
	request.Stage = strings.TrimSpace(request.Stage)
	request.App = strings.TrimSpace(request.App)
	request.Alb = strings.TrimSpace(request.Alb)
	request.Alias = strings.TrimSpace(request.Alias)
	if request.Name == "" {
		request.Name = request.Alias
	}
	if request.Name == "" {
		request.Name = request.Alb
	}
	if request.Name == "" {
		return nil, errors.New("name is required")
	}
	if request.Profile == "" {
		request.Profile = tm.config.Profile
	}
	if request.Region == "" {
		request.Region = tm.config.Region
	}
	if request.Stack == "" {
		return nil, errors.New("stack is required")
	}
	if request.Stage == "" {
		return nil, errors.New("stage is required")
	}
	if request.App == "" {
		return nil, errors.New("app is required")
	}
	if request.Alb == "" {
		return nil, errors.New("alb is required")
	}
	if request.LocalPort == 0 {
		request.LocalPort = 40000 + len(tm.tunnels)
	}
	request.Alias = request.Name
	request.Host = ""
	request.TargetPort = 443
	request.TunnelCommand = "/usr/local/bin/host-tunnel"
	request.CredentialCommands = ""
	if strings.TrimSpace(request.UpstreamProtocol) == "" {
		request.UpstreamProtocol = "https"
	}

	log.Printf("Creating tunnel request: name=%q alias=%q alb=%q localPort=%d", request.Name, request.Alias, request.Alb, request.LocalPort)

	tm.mu.Lock()
	defer tm.mu.Unlock()
	entry := &TunnelEntry{
		ID:                 fmt.Sprintf("tunnel-%d", time.Now().UnixNano()),
		Profile:            request.Profile,
		Region:             request.Region,
		Stack:              request.Stack,
		Stage:              request.Stage,
		App:                request.App,
		Alb:                request.Alb,
		Name:               request.Name,
		CredentialCommands: request.CredentialCommands,
		TunnelCommand:      request.TunnelCommand,
		Host:               request.Host,
		TargetPort:         request.TargetPort,
		Alias:              request.Alias,
		LocalPort:          request.LocalPort,
		UpstreamProtocol:   request.UpstreamProtocol,
		Status:             "pending",
		LocalEndpoint:      fmt.Sprintf("127.0.0.1:%d", request.LocalPort),
		CreatedAt:          time.Now(),
	}
	tm.tunnels[entry.ID] = entry
	log.Printf("Tunnel created: id=%s name=%q alias=%q localEndpoint=%s", entry.ID, entry.Name, entry.Alias, entry.LocalEndpoint)
	return entry, nil
}

func (tm *TunnelManager) Start(id string) error {
	tm.mu.Lock()
	entry, ok := tm.tunnels[id]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel %s not found", id)
	}
	tm.mu.Unlock()

	if strings.TrimSpace(entry.TunnelCommand) == "" {
		return errors.New("tunnel command is required")
	}

	log.Printf("Starting tunnel: id=%s name=%q alias=%q profile=%q region=%q localPort=%d", id, entry.Name, entry.Alias, entry.Profile, entry.Region, entry.LocalPort)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(entry.TunnelCommand,
		"--profile", entry.Profile,
		"--region", entry.Region,
		"--stack", entry.Stack,
		"--stage", entry.Stage,
		"--app", entry.App,
		"--alb", entry.Alb,
		"--local-port", fmt.Sprintf("%d", entry.LocalPort),
		"--name", entry.Name,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		log.Printf("Tunnel start failed: id=%s name=%q error=%v", id, entry.Name, err)
		tm.mu.Lock()
		entry.Status = "failed"
		entry.LastOutput = err.Error()
		tm.mu.Unlock()
		return fmt.Errorf("tunnel command failed to start: %w", err)
	}

	go func() {
		err := cmd.Wait()
		if err == nil {
			return
		}
		output := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if output == "" {
			output = err.Error()
		}
		log.Printf("Tunnel exited: id=%s name=%q error=%v output=%q", id, entry.Name, err, output)
		tm.mu.Lock()
		if current, ok := tm.tunnels[id]; ok {
			current.Status = "failed"
			current.LastOutput = output
		}
		tm.mu.Unlock()
	}()

	tm.mu.Lock()
	tm.process[id] = cmd
	entry.Status = "starting"
	entry.LastOutput = strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if entry.LastOutput == "" {
		entry.LastOutput = "tunnel command launched"
	}
	tm.mu.Unlock()
	log.Printf("Tunnel launched: id=%s name=%q alias=%q output=%q", id, entry.Name, entry.Alias, entry.LastOutput)
	return nil
}

func (tm *TunnelManager) StartAll() error {
	tm.mu.Lock()
	ids := make([]string, 0, len(tm.tunnels))
	for id := range tm.tunnels {
		ids = append(ids, id)
	}
	tm.mu.Unlock()
	for _, id := range ids {
		if err := tm.Start(id); err != nil {
			return err
		}
	}
	return nil
}

func decodeTunnelRequest(r any) (TunnelRequest, error) {
	var req TunnelRequest
	if data, ok := r.([]byte); ok {
		if err := json.Unmarshal(data, &req); err != nil {
			return TunnelRequest{}, err
		}
		return req, nil
	}
	if data, ok := r.(map[string]any); ok {
		if value, found := data["name"]; found {
			if v, ok := value.(string); ok { req.Name = v }
		}
		if value, found := data["profile"]; found {
			if v, ok := value.(string); ok { req.Profile = v }
		}
		if value, found := data["region"]; found {
			if v, ok := value.(string); ok { req.Region = v }
		}
		if value, found := data["stack"]; found {
			if v, ok := value.(string); ok { req.Stack = v }
		}
		if value, found := data["stage"]; found {
			if v, ok := value.(string); ok { req.Stage = v }
		}
		if value, found := data["app"]; found {
			if v, ok := value.(string); ok { req.App = v }
		}
		if value, found := data["alb"]; found {
			if v, ok := value.(string); ok { req.Alb = v }
		}
		if value, found := data["credentialCommands"]; found {
			if v, ok := value.(string); ok { req.CredentialCommands = v }
		}
		if value, found := data["tunnelCommand"]; found {
			if v, ok := value.(string); ok { req.TunnelCommand = v }
		}
		if value, found := data["host"]; found {
			if v, ok := value.(string); ok { req.Host = v }
		}
		if value, found := data["targetPort"]; found {
			switch v := value.(type) {
			case float64:
				req.TargetPort = int(v)
			case string:
				if parsed := strings.TrimSpace(v); parsed != "" {
					if n, err := fmt.Sscanf(parsed, "%d", &req.TargetPort); err == nil && n == 1 {
						// handled later
					}
				}
			}
		}
		if value, found := data["alias"]; found {
			if v, ok := value.(string); ok { req.Alias = v }
		}
		if value, found := data["localPort"]; found {
			switch v := value.(type) {
			case float64:
				req.LocalPort = int(v)
			case string:
				if parsed := strings.TrimSpace(v); parsed != "" {
					if n, err := fmt.Sscanf(parsed, "%d", &req.LocalPort); err == nil && n == 1 {
						// handled later
					}
				}
			}
		}
		if value, found := data["upstreamProtocol"]; found {
			if v, ok := value.(string); ok { req.UpstreamProtocol = v }
		}
		return req, nil
	}
	return TunnelRequest{}, errors.New("unsupported tunnel request type")
}

func tunnelDiagnostics(tunnelManager *TunnelManager, baseDomain string) []ContainerDiagnostic {
	if tunnelManager == nil {
		return nil
	}

	entries := tunnelManager.Snapshot()
	out := make([]ContainerDiagnostic, 0, len(entries))
	for _, entry := range entries {
		status := strings.TrimSpace(entry.Status)
		if status == "" {
			status = "pending"
		}
		hostName := strings.TrimSpace(entry.Alias)
		if hostName == "" {
			hostName = entry.Host
		}
		if !strings.Contains(hostName, ".") && strings.TrimSpace(baseDomain) != "" {
			hostName = hostName + "." + strings.TrimSpace(baseDomain)
		}
		reason := "aws tunnel"
		if entry.Alb != "" {
			reason = fmt.Sprintf("aws tunnel -> %s:%d", entry.Alb, entry.TargetPort)
		}
		routeText := fmt.Sprintf("%s -> %s:%d", hostName, entry.Alb, entry.TargetPort)
		if strings.Contains(hostName, ".") {
			routeText = fmt.Sprintf("%s -> %s:%d", hostName, "127.0.0.1", entry.LocalPort)
		}
		metaParts := []string{
			"aws=tunnel",
			fmt.Sprintf("profile=%s", entry.Profile),
			fmt.Sprintf("region=%s", entry.Region),
			fmt.Sprintf("alb=%s:%d", entry.Alb, entry.TargetPort),
			fmt.Sprintf("local=%s", entry.LocalEndpoint),
		}
		out = append(out, ContainerDiagnostic{
			Name:     entry.Alias,
			Status:   status,
			Reason:   reason,
			Routes:   routeText,
			Metadata: strings.Join(metaParts, "; "),
		})
	}
	return out
}

