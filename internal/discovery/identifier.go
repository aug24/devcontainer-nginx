package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
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

// ExtractRouteTargets returns one or more routes for a container.
// Single-port containers use the container name as the hostname.
// Multi-port containers use portsAttributes labels when available.
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

	port := i.ExtractApplicationPort(exposedPorts, publishedPorts, labels)
	if port == 0 {
		return nil
	}

	return []RouteTarget{{
		ProjectName:     projectName,
		ApplicationPort: port,
	}}
}

func devcontainerModelLabelKeys(labels map[string]string) []string {
	keys := []string{
		"devcontainer.model",
		"devcontainer.initial.model",
		"devcontainer.metadata",
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		seen[key] = true
	}

	for key := range labels {
		if strings.HasSuffix(key, ".devcontainer.model") || strings.HasSuffix(key, ".devcontainer.initial.model") || strings.HasSuffix(key, ".devcontainer.metadata") {
			if !seen[key] {
				keys = append(keys, key)
				seen[key] = true
			}
		}
	}

	return keys
}

func devcontainerPresentableName(labels map[string]string) string {
	for _, key := range []string{
		"devcontainer.presentable.name",
	} {
		if name := labels[key]; name != "" {
			return name
		}
	}
	for key, value := range labels {
		if value == "" {
			continue
		}
		if key == "devcontainer.presentable.name" || strings.HasSuffix(key, ".devcontainer.presentable.name") {
			return value
		}
	}
	return ""
}

// Metadata labels can be object-shaped legacy values or spec-compliant arrays of
// JSON snippets, so accept both shapes when reading them.
func ParseDevcontainerMetadata(raw string) ([]map[string]any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}

	switch metadata := value.(type) {
	case map[string]any:
		return []map[string]any{metadata}, nil
	case []any:
		documents := make([]map[string]any, 0, len(metadata))
		for _, item := range metadata {
			document, ok := item.(map[string]any)
			if ok {
				documents = append(documents, document)
			}
		}
		return documents, nil
	default:
		return nil, fmt.Errorf("devcontainer metadata must be a JSON object or array")
	}
}

func extractForwardedPorts(labels map[string]string) []int {
	labelKeys := devcontainerModelLabelKeys(labels)

	seen := make(map[int]bool)
	ports := make([]int, 0)

	for _, key := range labelKeys {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}

		documents, err := ParseDevcontainerMetadata(raw)
		if err != nil {
			continue
		}

		for _, doc := range documents {
			forwarded, ok := doc["forwardPorts"]
			if !ok {
				continue
			}

			list, ok := forwarded.([]any)
			if !ok {
				continue
			}

			for _, item := range list {
				var port int
				switch v := item.(type) {
				case float64:
					port = int(v)
				case string:
					p, err := strconv.Atoi(v)
					if err != nil {
						continue
					}
					port = p
				default:
					continue
				}
				if port > 0 && port <= 65535 && !seen[port] {
					seen[port] = true
					ports = append(ports, port)
				}
			}
		}
	}

	return ports
}

type forwardedPortLabel struct {
	Port  int
	Label string
}

func extractForwardedPortLabels(labels map[string]string) []forwardedPortLabel {
	labelKeys := devcontainerModelLabelKeys(labels)

	seenPorts := make(map[int]bool)
	var result []forwardedPortLabel

	for _, key := range labelKeys {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}

		documents, err := ParseDevcontainerMetadata(raw)
		if err != nil {
			continue
		}

		for _, doc := range documents {
			forwarded := parseForwardedPortsFromDoc(doc)
			if len(forwarded) == 0 {
				continue
			}

			attrLabels := parsePortsAttributesFromDoc(doc)
			if len(attrLabels) == 0 {
				continue
			}

			for _, port := range forwarded {
				label, ok := attrLabels[port]
				if !ok || strings.TrimSpace(label) == "" || seenPorts[port] {
					continue
				}
				seenPorts[port] = true
				result = append(result, forwardedPortLabel{
					Port:  port,
					Label: label,
				})
			}
		}
	}

	return result
}

func parseForwardedPortsFromDoc(doc map[string]any) []int {
	forwardedRaw, ok := doc["forwardPorts"]
	if !ok {
		return nil
	}

	list, ok := forwardedRaw.([]any)
	if !ok {
		return nil
	}

	seen := make(map[int]bool)
	ports := make([]int, 0, len(list))
	for _, item := range list {
		port, ok := parsePortAny(item)
		if !ok || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}

	return ports
}

func parsePortsAttributesFromDoc(doc map[string]any) map[int]string {
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
		port, err := strconv.Atoi(portKey)
		if err != nil || port <= 0 || port > 65535 {
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
		if !ok {
			continue
		}
		out[port] = label
	}

	return out
}

func parsePortAny(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		port := int(v)
		if !isValidPort(port) {
			return 0, false
		}
		return port, true
	case string:
		port, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		if !isValidPort(port) {
			return 0, false
		}
		return port, true
	default:
		return 0, false
	}
}

func isValidPort(port int) bool {
	return port > 0 && port <= 65535
}

// ExtractMetadata extracts metadata from a container.
func (i *Identifier) ExtractMetadata(
	containerID string,
	containerName string,
	labels map[string]string,
	exposedPorts []int,
	publishedPorts map[string][]PortBinding,
	networks []string,
) *DevContainerMetadata {
	// Check if this looks like a Dev Container
	if !i.IsDevContainer(containerName, labels) {
		return nil
	}

	// Extract project name
	projectName := i.ExtractProjectName(containerName, labels)
	if projectName == "" {
		log.Printf("Could not extract project name from %s\n", containerName)
		return nil
	}

	// Determine application port
	appPort := i.ExtractApplicationPort(exposedPorts, publishedPorts, labels)
	if appPort == 0 {
		log.Printf("WARNING: Container %s: could not determine application port; skipping\n", containerName)
		return nil
	}

	// Check networks
	if len(networks) == 0 {
		log.Printf("WARNING: Container %s: no networks; skipping\n", containerName)
		return nil
	}

	// Sort for consistency
	sort.Ints(exposedPorts)
	sort.Strings(networks)

	return &DevContainerMetadata{
		ContainerID:     containerID,
		ContainerName:   containerName,
		ProjectName:     projectName,
		ApplicationPort: appPort,
		Networks:        networks,
		Labels:          labels,
	}
}
