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
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	VerifySSL          bool   `json:"verifySSL"`
	verifySSLSet       bool   `json:"-"`
}

func (r *TunnelRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
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
		VerifySSL          *bool  `json:"verifySSL"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = TunnelRequest{
		Profile:            raw.Profile,
		Region:             raw.Region,
		Stack:              raw.Stack,
		Stage:              raw.Stage,
		App:                raw.App,
		Alb:                raw.Alb,
		Name:               raw.Name,
		CredentialCommands: raw.CredentialCommands,
		TunnelCommand:      raw.TunnelCommand,
		Host:               raw.Host,
		TargetPort:         raw.TargetPort,
		Alias:              raw.Alias,
		LocalPort:          raw.LocalPort,
		UpstreamProtocol:   raw.UpstreamProtocol,
		VerifySSL:          true,
	}
	if raw.VerifySSL != nil {
		r.VerifySSL = *raw.VerifySSL
		r.verifySSLSet = true
	} else {
		r.verifySSLSet = false
		r.VerifySSL = true
	}
	return nil
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
	VerifySSL          bool      `json:"verifySSL"`
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
	cfg.BootstrapCommand = normalizeBootstrapCommand(cfg.BootstrapCommand)
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

func normalizeBootstrapCommand(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
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
		log.Printf("Tunnel route candidate: hostname=%q target=%s:%d protocol=%q verifySSL=%v", route.Hostname, route.TargetHost, route.TargetPort, route.Protocol, route.VerifySSL)
	}
	if len(routes) == 0 {
		return
	}

	merged := append([]nginx.Route{}, configGen.CurrentRoutes()...)
	seen := make(map[string]int, len(merged))
	for i, route := range merged {
		seen[route.Hostname] = i
	}
	for _, route := range routes {
		if i, ok := seen[route.Hostname]; ok {
			merged[i] = route
			continue
		}
		merged = append(merged, route)
		seen[route.Hostname] = len(merged) - 1
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
			VerifySSL:     tunnel.VerifySSL,
			VerifyName:    tunnel.Alb,
		})
	}
	return out
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
			killProcessTree(cmd.Process.Pid)
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
	entry.LastOutput = "stopping tunnel..."
	tm.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		killProcessTree(cmd.Process.Pid)
	}

	tm.mu.Lock()
	if current, ok := tm.tunnels[id]; ok {
		current.Status = "stopped"
		current.LastOutput = "tunnel stopped by user"
		delete(tm.process, id)
	}
	tm.mu.Unlock()

	return nil
}

func childrenOfPID(pid int) ([]int, error) {
	output, err := exec.Command("ps", "-eo", "pid=,ppid=").Output()
	if err != nil {
		return nil, err
	}

	var children []int
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		childPID, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parentPID, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		if parentPID == pid {
			children = append(children, childPID)
		}
	}
	return children, nil
}

func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}

	seen := map[int]bool{pid: true}
	stack := []int{pid}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		children, err := childrenOfPID(current)
		if err != nil {
			continue
		}
		for _, childPID := range children {
			if !seen[childPID] {
				seen[childPID] = true
				stack = append(stack, childPID)
			}
		}
	}

	for processPID := range seen {
		if processPID == pid {
			_ = syscall.Kill(-pid, syscall.SIGTERM)
		}
		_ = syscall.Kill(processPID, syscall.SIGTERM)
	}

	time.Sleep(200 * time.Millisecond)

	for processPID := range seen {
		if processPID == pid {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		if err := syscall.Kill(processPID, 0); err != nil && errors.Is(err, syscall.ESRCH) {
			continue
		}
		_ = syscall.Kill(processPID, syscall.SIGKILL)
	}
}

func (tm *TunnelManager) stopProcessByID(id string) {
	tm.mu.Lock()
	cmd, ok := tm.process[id]
	tm.mu.Unlock()
	if !ok || cmd == nil || cmd.Process == nil {
		return
	}

	killProcessTree(cmd.Process.Pid)

	tm.mu.Lock()
	if current, exists := tm.process[id]; exists && current == cmd {
		delete(tm.process, id)
	}
	tm.mu.Unlock()
}

func (tm *TunnelManager) markTunnelExit(id string, cmd *exec.Cmd, err error) {
	_ = id
	_ = cmd
	_ = err
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
	if !request.verifySSLSet {
		request.VerifySSL = true
	}
	log.Printf("Normalized tunnel request: name=%q alias=%q verifySSL=%v verifySSLSet=%v", request.Name, request.Alias, request.VerifySSL, request.verifySSLSet)

	log.Printf("Creating tunnel request: name=%q alias=%q alb=%q localPort=%d upstreamProtocol=%q verifySSL=%v verifySSLSet=%v", request.Name, request.Alias, request.Alb, request.LocalPort, request.UpstreamProtocol, request.VerifySSL, request.verifySSLSet)

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
		VerifySSL:          request.VerifySSL,
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
		tm.mu.Lock()
		current, ok := tm.tunnels[id]
		if !ok {
			tm.mu.Unlock()
			return
		}
		if current.Status == "stopping" || current.Status == "stopped" {
			delete(tm.process, id)
			current.Status = "stopped"
			current.LastOutput = "tunnel stopped by user"
			tm.mu.Unlock()
			return
		}
		output := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if output == "" {
			output = err.Error()
		}
		log.Printf("Tunnel exited: id=%s name=%q error=%v output=%q", id, entry.Name, err, output)
		delete(tm.process, id)
		current.Status = "failed"
		current.LastOutput = output
		tm.mu.Unlock()
	}()

	tm.mu.Lock()
	tm.process[id] = cmd
	entry.Status = "running"
	entry.LastOutput = strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if entry.LastOutput == "" {
		entry.LastOutput = fmt.Sprintf("SSM tunnel active for %s via %s:%d on local %s", entry.Alias, entry.Alb, entry.TargetPort, entry.LocalEndpoint)
	}
	tm.mu.Unlock()
	log.Printf("Tunnel launched: id=%s name=%q alias=%q status=%s output=%q", id, entry.Name, entry.Alias, entry.Status, entry.LastOutput)
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


func tunnelEntryStatus(entry *TunnelEntry) string {
	if entry == nil {
		return "pending"
	}
	status := strings.TrimSpace(entry.Status)
	if status != "" {
		if status == "failed" {
			return status
		}
		if status == "running" || status == "starting" || status == "pending" || status == "stopping" || status == "stopped" {
			return status
		}
	}
	output := strings.TrimSpace(entry.LastOutput)
	if output == "" {
		return "pending"
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "cannot perform start session") || strings.Contains(lower, "broken pipe") || strings.Contains(lower, "sessionmanagerplugin is not found") || strings.Contains(lower, "aws bootstrap command failed") || strings.Contains(lower, "no running ec2 instance found") {
		return "failed"
	}
	return "running"
}

func tunnelDiagnostics(tunnelManager *TunnelManager, baseDomain string) []ContainerDiagnostic {
	if tunnelManager == nil {
		return nil
	}

	entries := tunnelManager.Snapshot()
	out := make([]ContainerDiagnostic, 0, len(entries))
	for _, entry := range entries {
		status := tunnelEntryStatus(&entry)
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
		if output := strings.TrimSpace(entry.LastOutput); output != "" {
			metaParts = append(metaParts, "lastOutput="+output)
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
