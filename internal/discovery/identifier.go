package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

// DevContainerMetadata holds extracted metadata from a Dev Container.
type DevContainerMetadata struct {
	ContainerID     string
	ContainerName   string
	ProjectName     string
	ApplicationPort int
	Networks        []string
	Labels          map[string]string
}

// RouteTarget represents one hostname+port route discovered for a container.
type RouteTarget struct {
	ProjectName     string
	ApplicationPort int
}

// IsValid checks if metadata is valid for routing.
func (m *DevContainerMetadata) IsValid() bool {
	return m.ProjectName != "" && m.ApplicationPort > 0 && len(m.Networks) > 0
}

// Identifier identifies Dev Containers and extracts their metadata.
type Identifier struct {
	maxHostnameLen int
	hostnameRegex  *regexp.Regexp
}

// NewIdentifier creates a new identifier.
func NewIdentifier() *Identifier {
	return &Identifier{
		maxHostnameLen: 63,
		hostnameRegex:  regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`),
	}
}

// IsDevContainer determines if a container is a Dev Container.
// Uses heuristics based on existing Docker/Dev Container metadata:
// - Explicit Dev Container labels (devcontainer.*)
// - Docker Compose managed containers (com.docker.compose.*)
// - Reasonable container naming patterns
// - Does NOT require specific labels; looks for reasonable indicators
func (i *Identifier) IsDevContainer(containerName string, labels map[string]string) bool {
	// Remove leading slash from container name
	name := strings.TrimPrefix(containerName, "/")

	// Check for explicit Dev Container metadata
	for key := range labels {
		if strings.HasPrefix(key, "devcontainer.") {
			log.Printf("Container %s identified as Dev Container via devcontainer.* label\n", containerName)
			return true
		}
		// Docker Compose managed containers
		if strings.HasPrefix(key, "com.docker.compose.") {
			log.Printf("Container %s identified as Dev Container via com.docker.compose.* label\n", containerName)
			return true
		}
	}

	// Heuristic: reasonable development naming patterns suggest a dev container
	// Skip obvious system containers
	systemPatterns := []string{
		"nginx", "postgres", "mysql", "redis", "mongo", "elasticsearch",
		"prometheus", "grafana", "kibana", "consul", "vault",
		"docker", "containerd", "k8s", "kubernetes",
	}
	nameLower := strings.ToLower(name)
	for _, pattern := range systemPatterns {
		if strings.Contains(nameLower, pattern) {
			return false
		}
	}

	// If it has a reasonable name (not a UUID, not a hash), treat it as potential dev container
	// This allows containers created ad-hoc (e.g., docker run --name myapp) to be proxied
	if len(name) > 0 && !isLikelyHash(name) {
		log.Printf("Container %s appears to be a development container based on naming\n", containerName)
		return true
	}

	return false
}

// isLikelyHash checks if a string looks like a container ID hash or similar auto-generated ID
func isLikelyHash(s string) bool {
	// Check if string is mostly hexadecimal (likely a hash)
	if len(s) >= 12 {
		hexCount := 0
		for _, c := range s {
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
				hexCount++
			}
		}
		if float64(hexCount)/float64(len(s)) > 0.8 {
			return true
		}
	}
	return false
}

// ExtractProjectName extracts project name from container metadata.
// Priority:
// 1. Explicit hostname labels
// 2. Docker Compose project name
// 3. Sanitized container name
func (i *Identifier) ExtractProjectName(
	containerName string,
	labels map[string]string,
) string {
	// Try hostname labels first
	hostnameLabels := []string{
		"devcontainer.hostname",
		"application.name",
		"app.name",
	}

	for _, label := range hostnameLabels {
		if name, ok := labels[label]; ok && name != "" {
			if sanitized := i.SanitizeHostname(name); sanitized != "" {
				log.Printf("Extracted project name from label %s: %s\n", label, sanitized)
				return sanitized
			}
		}
	}

	// Try Docker Compose project name
	if composeProject, ok := labels["com.docker.compose.project"]; ok && composeProject != "" {
		if sanitized := i.SanitizeHostname(composeProject); sanitized != "" {
			log.Printf("Extracted project name from Compose label: %s\n", sanitized)
			return sanitized
		}
	}

	// Fall back to container name
	if sanitized := i.SanitizeHostname(containerName); sanitized != "" {
		log.Printf("Using container name as project name: %s\n", sanitized)
		return sanitized
	}

	return ""
}

// SanitizeHostname sanitizes a string to be a valid DNS hostname label.
// DNS labels:
// - must start with alphanumeric
// - may contain hyphens
// - must end with alphanumeric
// - max 63 characters
// - only lowercase letters, digits, and hyphens
func (i *Identifier) SanitizeHostname(name string) string {
	// Remove leading slash if present
	name = strings.TrimPrefix(name, "/")

	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace underscores with hyphens
	name = strings.ReplaceAll(name, "_", "-")

	// Remove invalid characters (keep only alphanumeric and hyphen)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	name = re.ReplaceAllString(name, "")

	// Remove leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Check length
	if len(name) == 0 || len(name) > i.maxHostnameLen {
		return ""
	}

	// Validate against pattern
	if !i.hostnameRegex.MatchString(name) {
		return ""
	}

	return name
}

// ExtractApplicationPort determines which port to proxy to.
// Strategy:
// 1. Explicit port label
// 2. Dev Container forwarded ports metadata
// 3. Single exposed port
// 4. Common web server ports (3000, 5173, 8000, 8080, 80)
// 5. Return 0 if still ambiguous
func (i *Identifier) ExtractApplicationPort(
	exposedPorts []int,
	publishedPorts map[string][]PortBinding,
	labels map[string]string,
) int {
	// Check for explicit port label
	portLabels := []string{
		"devcontainer.application.port",
		"application.port",
		"app.port",
	}

	for _, label := range portLabels {
		if portStr, ok := labels[label]; ok && portStr != "" {
			var port int
			_, err := fmt.Sscanf(portStr, "%d", &port)
			if err == nil && port > 0 && port <= 65535 {
				log.Printf("Using port from label %s: %d\n", label, port)
				return port
			}
		}
	}

	// Check forwarded ports in existing Dev Container metadata labels.
	forwardedPorts := extractForwardedPorts(labels)
	if len(forwardedPorts) > 0 {
		if len(forwardedPorts) == 1 {
			log.Printf("Using single forwarded port: %d\n", forwardedPorts[0])
			return forwardedPorts[0]
		}
		for _, port := range []int{3000, 5173, 8000, 8080, 80} {
			for _, forwarded := range forwardedPorts {
				if port == forwarded {
					log.Printf("Using common web forwarded port: %d from %v\n", port, forwardedPorts)
					return port
				}
			}
		}
		log.Printf("Using first forwarded port: %d from %v\n", forwardedPorts[0], forwardedPorts)
		return forwardedPorts[0]
	}

	if len(exposedPorts) == 0 {
		// No exposed ports; try published ports
		if len(publishedPorts) > 0 {
			hostPorts := make(map[int]bool)
			for _, bindings := range publishedPorts {
				for _, b := range bindings {
					var port int
					fmt.Sscanf(b.HostPort, "%d", &port)
					if port > 0 {
						hostPorts[port] = true
					}
				}
			}
			ports := make([]int, 0, len(hostPorts))
			for port := range hostPorts {
				ports = append(ports, port)
			}
			if len(ports) == 1 {
				log.Printf("Using published port: %d\n", ports[0])
				return ports[0]
			}

			for _, port := range []int{3000, 5173, 8000, 8080, 80} {
				for _, published := range ports {
					if port == published {
						log.Printf("Using common web published port: %d from %v\n", port, ports)
						return port
					}
				}
			}
			if len(ports) > 0 {
				log.Printf("Using first published port: %d from %v\n", ports[0], ports)
				return ports[0]
			}
		}
		return 0
	}

	// If exactly one exposed port, use it
	if len(exposedPorts) == 1 {
		port := exposedPorts[0]
		log.Printf("Using single exposed port: %d\n", port)
		return port
	}

	// Multiple exposed ports: try to pick sensible web application port
	// Common order: 3000 (Node), 5173 (Vite), 8000 (Django), 8080 (Java), 80 (generic)
	commonPorts := []int{3000, 5173, 8000, 8080, 80}
	for _, port := range commonPorts {
		for _, exposed := range exposedPorts {
			if port == exposed {
				log.Printf("Using common web port: %d from %v\n", port, exposedPorts)
				return port
			}
		}
	}

	// Ambiguous: multiple ports and no clear indicator
	log.Printf("WARNING: Multiple exposed ports %v; unable to determine app port\n", exposedPorts)
	return 0
}

func (i *Identifier) ExtractRouteTargets(
	containerName string,
	labels map[string]string,
	exposedPorts []int,
	publishedPorts map[string][]PortBinding,
) []RouteTarget {
	var targets []RouteTarget

	forwardedPortLabels := extractForwardedPortLabels(labels)
	if len(forwardedPortLabels) > 1 {
		for _, entry := range forwardedPortLabels {
			sanitized := i.SanitizeHostname(entry.Label)
			if sanitized == "" {
				log.Printf("WARNING: Ignoring invalid portsAttributes label %q for port %d\n", entry.Label, entry.Port)
				continue
			}
			targets = append(targets, RouteTarget{
				ProjectName:     sanitized,
				ApplicationPort: entry.Port,
			})
		}
		if len(targets) > 0 {
			log.Printf("Using %d named routes from portsAttributes\n", len(targets))
			return targets
		}
	}

	projectName := i.ExtractProjectName(containerName, labels)
	if projectName == "" {
		return nil
	}

	publishedHostPorts := collectPublishedHostPorts(publishedPorts)
	if len(publishedHostPorts) > 0 {
		if !hasDevcontainerMetadata(labels) {
			if len(publishedHostPorts) > 1 {
				out := make([]RouteTarget, 0, len(publishedHostPorts))
				for _, hostPort := range publishedHostPorts {
					out = append(out, RouteTarget{
						ProjectName:     fmt.Sprintf("%d.%s", hostPort, projectName),
						ApplicationPort: hostPort,
					})
				}
				log.Printf("Using %d published host-port routes for %s: %v\n", len(out), containerName, out)
				return out
			}
		}

		port := i.ExtractApplicationPort(exposedPorts, publishedPorts, labels)
		if port > 0 {
			if len(publishedHostPorts) > 1 || publishedHostPorts[0] != port {
				out := make([]RouteTarget, 0, len(publishedHostPorts))
				for _, hostPort := range publishedHostPorts {
					out = append(out, RouteTarget{
						ProjectName:     fmt.Sprintf("%d.%s", hostPort, projectName),
						ApplicationPort: port,
					})
				}
				log.Printf("Using %d published host-port routes for %s: %v\n", len(out), containerName, out)
				return out
			}
		}
	}

	if !hasDevcontainerMetadata(labels) {
		fallbackTargets := i.ExtractPlainContainerRouteTargets(containerName, exposedPorts, publishedPorts)
		if len(fallbackTargets) > 0 {
			log.Printf("Using plain container fallback routes for %s: %v\n", containerName, fallbackTargets)
			return fallbackTargets
		}
	}

	port := i.ExtractApplicationPort(exposedPorts, publishedPorts, labels)
	if port == 0 {
		return nil
	}

	return []RouteTarget{{
		ProjectName:     projectName,
		ApplicationPort: port,
	}}
}

func hasDevcontainerMetadata(labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, "devcontainer.") || strings.HasPrefix(key, "com.docker.compose.") {
			return true
		}
		if strings.HasSuffix(key, ".devcontainer.model") || strings.HasSuffix(key, ".devcontainer.initial.model") || strings.HasSuffix(key, ".devcontainer.metadata") || strings.HasSuffix(key, ".devcontainer.presentable.name") {
			return true
		}
	}
	return false
}

func (i *Identifier) ExtractPlainContainerRouteTargets(
	containerName string,
	exposedPorts []int,
	publishedPorts map[string][]PortBinding,
) []RouteTarget {
	baseName := i.ExtractProjectName(containerName, nil)
	if baseName == "" {
		return nil
	}

	containerPorts := collectUniqueContainerPorts(exposedPorts, publishedPorts)
	if len(containerPorts) == 0 {
		return nil
	}
	sort.Ints(containerPorts)

	publishedHostPorts := collectPublishedHostPorts(publishedPorts)
	if len(publishedHostPorts) > 1 {
		out := make([]RouteTarget, 0, len(publishedHostPorts))
		for _, hostPort := range publishedHostPorts {
			out = append(out, RouteTarget{
				ProjectName:     fmt.Sprintf("%d.%s", hostPort, baseName),
				ApplicationPort: hostPort,
			})
		}
		return out
	}

	if len(containerPorts) == 1 {
		return []RouteTarget{{
			ProjectName:     baseName,
			ApplicationPort: containerPorts[0],
		}}
	}

	defaultPort := choosePreferredPort(containerPorts)
	if defaultPort == 0 {
		defaultPort = containerPorts[0]
	}

	targets := []RouteTarget{{
		ProjectName:     baseName,
		ApplicationPort: defaultPort,
	}}
	for _, port := range containerPorts {
		targets = append(targets, RouteTarget{
			ProjectName:     fmt.Sprintf("%d.%s", port, baseName),
			ApplicationPort: port,
		})
	}
	return targets
}

func choosePreferredPort(ports []int) int {
	if len(ports) == 0 {
		return 0
	}

	preferred := []int{3000, 5173, 8000, 8080, 8081, 8082, 80, 443, 8443, 9000}
	for _, candidate := range preferred {
		for _, port := range ports {
			if port == candidate {
				return port
			}
		}
	}

	copyPorts := append([]int(nil), ports...)
	sort.Ints(copyPorts)
	return copyPorts[0]
}

func collectUniqueContainerPorts(exposedPorts []int, publishedPorts map[string][]PortBinding) []int {
	seen := make(map[int]bool)
	out := make([]int, 0, len(exposedPorts))
	for _, port := range exposedPorts {
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	if len(out) > 0 {
		return out
	}
	for key, bindings := range publishedPorts {
		if len(bindings) == 0 {
			continue
		}
		port := parsePortKey(key)
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func collectPublishedHostPorts(publishedPorts map[string][]PortBinding) []int {
	seen := make(map[int]bool)
	out := make([]int, 0)
	for _, bindings := range publishedPorts {
		for _, binding := range bindings {
			var port int
			_, err := fmt.Sscanf(binding.HostPort, "%d", &port)
			if err != nil || port <= 0 || seen[port] {
				continue
			}
			seen[port] = true
			out = append(out, port)
		}
	}
	sort.Ints(out)
	return out
}

func parsePortKey(key string) int {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	key = strings.TrimSuffix(key, "/tcp")
	key = strings.TrimSuffix(key, "/udp")
	var port int
	_, err := fmt.Sscanf(key, "%d", &port)
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func ParseDevcontainerMetadata(raw string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var single map[string]any
	if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
		return []map[string]any{single}, nil
	}

	var multi []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &multi); err == nil {
		return multi, nil
	}

	return nil, fmt.Errorf("invalid devcontainer metadata JSON")
}

func devcontainerMetadataKeys(labels map[string]string) []string {
	keys := []string{}
	seen := map[string]bool{}
	for _, key := range []string{
		"devcontainer.model",
		"devcontainer.initial.model",
		"devcontainer.metadata",
		"devcontainer.presentable.name",
	} {
		if _, ok := labels[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	for key := range labels {
		if strings.HasSuffix(key, ".devcontainer.model") || strings.HasSuffix(key, ".devcontainer.initial.model") || strings.HasSuffix(key, ".devcontainer.metadata") || strings.HasSuffix(key, ".devcontainer.presentable.name") {
			if !seen[key] {
				keys = append(keys, key)
				seen[key] = true
			}
		}
	}
	return keys
}

type portLabel struct {
	Port  int
	Label string
}

func extractForwardedPorts(labels map[string]string) []int {
	seen := make(map[int]bool)
	ports := make([]int, 0)
	for _, key := range devcontainerMetadataKeys(labels) {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}
		docs, err := ParseDevcontainerMetadata(raw)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			for _, port := range parseForwardedPorts(doc) {
				if !seen[port] {
					seen[port] = true
					ports = append(ports, port)
				}
			}
		}
	}
	sort.Ints(ports)
	return ports
}

func extractForwardedPortLabels(labels map[string]string) []portLabel {
	seen := make(map[int]bool)
	out := make([]portLabel, 0)
	for _, key := range devcontainerMetadataKeys(labels) {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}
		docs, err := ParseDevcontainerMetadata(raw)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			for port, label := range parsePortsAttributes(doc) {
				if !seen[port] {
					seen[port] = true
					out = append(out, portLabel{Port: port, Label: label})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

func parseForwardedPorts(doc map[string]any) []int {
	forwardedRaw, ok := doc["forwardPorts"]
	if !ok {
		return nil
	}

	switch value := forwardedRaw.(type) {
	case []any:
		seen := make(map[int]bool)
		ports := make([]int, 0, len(value))
		for _, item := range value {
			switch v := item.(type) {
			case float64:
				port := int(v)
				if port > 0 && port <= 65535 && !seen[port] {
					seen[port] = true
					ports = append(ports, port)
				}
			case string:
				var port int
				if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 && port <= 65535 && !seen[port] {
					seen[port] = true
					ports = append(ports, port)
				}
			}
		}
		return ports
	case []int:
		return value
	case []float64:
		ports := make([]int, 0, len(value))
		for _, v := range value {
			port := int(v)
			if port > 0 && port <= 65535 {
				ports = append(ports, port)
			}
		}
		return ports
	}
	return nil
}

func parsePortsAttributes(doc map[string]any) map[int]string {
	attrsRaw, ok := doc["portsAttributes"]
	if !ok {
		return nil
	}

	attrs, ok := attrsRaw.(map[string]any)
	if !ok {
		return nil
	}

	out := make(map[int]string, len(attrs))
	for portKey, value := range attrs {
		var port int
		if _, err := fmt.Sscanf(portKey, "%d", &port); err != nil || port <= 0 || port > 65535 {
			continue
		}
		valueMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		labelRaw, ok := valueMap["label"]
		if !ok {
			continue
		}
		label, ok := labelRaw.(string)
		if !ok || label == "" {
			continue
		}
		out[port] = label
	}
	return out
}
