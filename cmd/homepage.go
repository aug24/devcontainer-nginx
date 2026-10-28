package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/dev-tools/devcontainer-nginx/internal/discovery"
	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

type ContainerDiagnostic struct {
	Name     string
	Status   string
	Reason   string
	Routes   string
	Metadata string
}

type DiagnosticsState struct {
	mu       sync.RWMutex
	rows     []ContainerDiagnostic
	revision uint64
}

func (ds *DiagnosticsState) Set(rows []ContainerDiagnostic) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if reflect.DeepEqual(ds.rows, rows) {
		return
	}

	ds.rows = make([]ContainerDiagnostic, len(rows))
	copy(ds.rows, rows)
	ds.revision++
}

func (ds *DiagnosticsState) Snapshot() []ContainerDiagnostic {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	rows := make([]ContainerDiagnostic, len(ds.rows))
	copy(rows, ds.rows)
	return rows
}

func (ds *DiagnosticsState) Revision() uint64 {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.revision
}

func diagnoseContainer(
	containerInfo *discovery.ContainerInfo,
	identifier *discovery.Identifier,
	resolver *discovery.NetworkResolver,
	baseDomain string,
) ([]nginx.Route, ContainerDiagnostic) {
	forwardedPorts, _ := extractDiagnosticsPortMetadata(containerInfo.Labels)
	forwardedSet := make(map[int]bool, len(forwardedPorts))
	for _, port := range forwardedPorts {
		forwardedSet[port] = true
	}

	diag := ContainerDiagnostic{
		Name:     containerInfo.Name,
		Status:   "skipped",
		Metadata: summarizeContainerMetadata(containerInfo),
	}

	if !identifier.IsDevContainer(containerInfo.Name, containerInfo.Labels) {
		diag.Reason = "not identified as a dev container"
		return nil, diag
	}

	if len(containerInfo.Networks) == 0 {
		diag.Reason = "no networks available"
		return nil, diag
	}

	targets := identifier.ExtractRouteTargets(
		containerInfo.Name,
		containerInfo.Labels,
		containerInfo.ExposedPorts,
		containerInfo.PublishedPorts,
	)
	if len(targets) == 0 {
		diag.Reason = "no route targets found"
		return nil, diag
	}

	var routes []nginx.Route
	var routeDetails []string
	for _, target := range targets {
		hostname := target.ProjectName + "." + baseDomain

		route := resolver.ResolveRoute(
			containerInfo.Name,
			containerInfo.ID,
			hostname,
			target.ApplicationPort,
			containerInfo.NetworkSettings,
			containerInfo.PublishedPorts,
		)
		if route == nil {
			if forwardedSet[target.ApplicationPort] {
				route = &discovery.ResolvedRoute{
					ContainerName: containerInfo.Name,
					ContainerID:   containerInfo.ID,
					Hostname:      hostname,
					TargetHost:    "host.docker.internal",
					TargetPort:    target.ApplicationPort,
				}
				log.Printf("Resolved route via forwarded port: %s -> host.docker.internal:%d\n", hostname, target.ApplicationPort)
			} else {
				continue
			}
		}

		routes = append(routes, nginx.Route{
			ContainerName: containerInfo.Name,
			Hostname:      route.Hostname,
			TargetHost:    route.TargetHost,
			TargetPort:    route.TargetPort,
		})
		routeDetails = append(routeDetails, fmt.Sprintf("%s -> %s:%d", route.Hostname, route.TargetHost, route.TargetPort))
	}

	if len(routes) == 0 {
		diag.Reason = "no resolvable route (no shared network, published port, or forwarded port)"
		return nil, diag
	}

	diag.Status = "routed"
	if len(routes) != len(targets) {
		diag.Status = "partial"
		diag.Reason = fmt.Sprintf("%d of %d routes resolved", len(routes), len(targets))
	}
	diag.Routes = strings.Join(routeDetails, "; ")
	return routes, diag
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
		if value := labels[key]; value != "" {
			return value
		}
	}
	for key, value := range labels {
		if value == "" {
			continue
		}
		if strings.HasSuffix(key, ".devcontainer.presentable.name") || key == "devcontainer.presentable.name" {
			return value
		}
	}
	return ""
}

func summarizeContainerMetadata(containerInfo *discovery.ContainerInfo) string {
	var parts []string

	if name := devcontainerPresentableName(containerInfo.Labels); name != "" {
		parts = append(parts, "presentable="+name)
	}
	if name := containerInfo.Labels["devcontainer.hostname"]; name != "" {
		parts = append(parts, "hostname="+name)
	}
	if containerInfo.Image != "" {
		parts = append(parts, "image="+containerInfo.Image)
	}

	forwardedPorts, portLabels := extractDiagnosticsPortMetadata(containerInfo.Labels)
	if len(forwardedPorts) > 0 {
		parts = append(parts, fmt.Sprintf("forwardPorts=%v", forwardedPorts))
	}
	if len(portLabels) > 0 {
		keys := make([]int, 0, len(portLabels))
		for port := range portLabels {
			keys = append(keys, port)
		}
		sort.Ints(keys)
		var items []string
		for _, port := range keys {
			items = append(items, fmt.Sprintf("%d=%s", port, portLabels[port]))
		}
		parts = append(parts, "portsAttributes="+strings.Join(items, ","))
	}

	if len(containerInfo.ExposedPorts) > 0 {
		parts = append(parts, fmt.Sprintf("exposed=%v", containerInfo.ExposedPorts))
	}
	if len(containerInfo.PublishedPorts) > 0 {
		parts = append(parts, fmt.Sprintf("published=%d", len(containerInfo.PublishedPorts)))
	}
	if len(containerInfo.Networks) > 0 {
		parts = append(parts, "networks="+strings.Join(containerInfo.Networks, ","))
	}

	return strings.Join(parts, "; ")
}

func extractDiagnosticsPortMetadata(labels map[string]string) ([]int, map[int]string) {
	labelKeys := devcontainerModelLabelKeys(labels)
	log.Println("Labels:", labelKeys)

	seenPorts := make(map[int]bool)
	portLabels := make(map[int]string)
	var forwardedPorts []int

	for _, key := range labelKeys {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}

		log.Println("Found Label:", key)
		log.Println("Raw Value:", raw)

		documents, err := discovery.ParseDevcontainerMetadata(raw)
		if err != nil {
			continue
		}

		for _, doc := range documents {
			for _, port := range parseDiagnosticsForwardedPorts(doc) {
				log.Println("Found Forwarded Port:", port)
				if !seenPorts[port] {
					seenPorts[port] = true
					forwardedPorts = append(forwardedPorts, port)
				}
			}

			for port, label := range parseDiagnosticsPortsAttributes(doc) {
				log.Println("Found Port Attribute:", port, "Label:", label)
				portLabels[port] = label
			}
		}
	}

	sort.Ints(forwardedPorts)
	return forwardedPorts, portLabels
}

func parseDiagnosticsForwardedPorts(doc map[string]any) []int {
	forwardedRaw, ok := doc["forwardPorts"]
	if !ok {
		return nil
	}

	list, ok := forwardedRaw.([]any)
	if !ok {
		return nil
	}

	seen := make(map[int]bool)
	var ports []int
	for _, item := range list {
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
}

func parseDiagnosticsPortsAttributes(doc map[string]any) map[int]string {
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

func loadHelpText(page string) string {
	candidates := []string{
		filepath.Join(".", "docs", "help", page+".txt"),
		filepath.Join("/usr/share/devcontainer-nginx", "docs", "help", page+".txt"),
	}
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err == nil {
			return strings.TrimSpace(string(content))
		}
	}
	return ""
}

func renderHelpSection(page string) string {
	text := loadHelpText(page)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return fmt.Sprintf("<div class=\"help-block\"><h3>How this page works</h3><pre>%s</pre></div>", html.EscapeString(text))
}

func renderHomepageHTML(rows []ContainerDiagnostic, revision uint64, endpointRows []PublishedEndpointRow) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Home</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head>")
	b.WriteString(fmt.Sprintf("<body data-page=\"homepage\" data-current-revision=\"%d\">", revision))
	b.WriteString(renderPageHeader("Home"))
	b.WriteString("<table><thead><tr><th>Endpoint</th><th>Service</th></tr></thead><tbody>")

	if len(endpointRows) == 0 {
		b.WriteString("<tr><td colspan=\"2\">No published endpoints are currently configured.</td></tr>")
	} else {
		for _, row := range endpointRows {
			host := html.EscapeString(row.Hostname)
			provider := html.EscapeString(row.Provider)
			status := html.EscapeString(row.Status)
			b.WriteString("<tr><td><a href=\"https://")
			b.WriteString(host)
			b.WriteString("\" target=\"_blank\" rel=\"noopener noreferrer\">")
			b.WriteString(host)
			b.WriteString(" ")
			b.WriteString(renderOpenInNewTabIcon())
			b.WriteString("</a></td><td>")
			b.WriteString(provider)
			b.WriteString(" — ")
			b.WriteString(status)
			b.WriteString("</td></tr>")
		}
	}
	b.WriteString("</tbody></table>")
	b.WriteString("<h2>Actions</h2><ul>")
	b.WriteString("<li><a href=\"/aws-config\">Configure AWS credentials in this container</a></li>")
	b.WriteString("<li><a href=\"/tunnels\">Start or stop tunnels to services</a></li>")
	b.WriteString("<li><a href=\"/shell-launch\">Open a shell in a new tab</a></li>")
	b.WriteString("</ul>")
	b.WriteString(renderHelpSection("homepage"))
	b.WriteString("<h2>Debugging Container Services</h2><ul>")
	b.WriteString("<li><a href=\"/logs\">View logs</a></li>")
	b.WriteString("<li><a href=\"/shell-command-generator\">Generate shell commands for bastion host access</a></li>")
	b.WriteString("<li><a href=\"/nginx.conf\">Base nginx.conf</a></li>")
	b.WriteString("<li><a href=\"/generated.conf\">Generated nginx config</a></li>")
	b.WriteString("<li><a href=\"/nginx/config\">Full nginx config</a></li>")
	b.WriteString("<li><a href=\"/containers\">Container config</a></li>")
	b.WriteString("<li><a href=\"/debug\">Debug page</a></li>")
	b.WriteString("</ul>")
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderContainerCell(name, metadata string) string {
	nameEscaped := html.EscapeString(name)
	if nameEscaped == "" {
		return ""
	}
	if strings.TrimSpace(metadata) == "" {
		return nameEscaped
	}
	popup := strings.ReplaceAll(html.EscapeString(metadata), "; ", "<br>")
	return fmt.Sprintf(`<span class="container-label" tabindex="0"><span class="container-name">%s</span><span class="container-popup">%s</span></span>`, nameEscaped, popup)
}

func renderPageHeader(title string) string {
	return fmt.Sprintf(`<div class="page-header"><h1><a href="/" class="page-header-link">%s</a>: %s</h1><a href="/" class="site-icon-link" aria-label="Home"><img src="/static/icon.png" alt="Devcontainer Nginx home" width="32" height="32"></a></div>`, html.EscapeString("Devcontainer Nginx"), html.EscapeString(title))
}

func renderStylesheetLink() string {
	return "<link rel=\"stylesheet\" href=\"/static/styles.css\">"
}

func renderRouteLinks(routes string) string {
	if routes == "" {
		return ""
	}

	parts := strings.Split(routes, "; ")
	links := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		host := part
		if idx := strings.Index(part, " -> "); idx >= 0 {
			host = part[:idx]
		}
		if host == "" {
			continue
		}
		escaped := html.EscapeString(host)
		links = append(links,
			fmt.Sprintf(`<a href="https://%s" target="_blank" rel="noopener noreferrer"><span class="proto-badge proto-https">HTTPS</span>%s</a>`, escaped, escaped),
			fmt.Sprintf(`<a href="http://%s" target="_blank" rel="noopener noreferrer"><span class="proto-badge proto-http">HTTP</span>%s</a>`, escaped, escaped),
		)
	}

	return strings.Join(links, "<br>")
}

func renderMetadataLines(metadata string) string {
	if metadata == "" {
		return ""
	}

	parts := strings.Split(metadata, "; ")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		lines = append(lines, html.EscapeString(part))
	}

	return strings.Join(lines, "<br>")
}

func renderFaviconLink() string {
	return `<link rel="icon" type="image/png" href="/favicon.ico">`
}

func renderContainersHTML(names []string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Containers</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"containers\">")
	b.WriteString(renderPageHeader("Containers"))
	if len(names) == 0 {
		b.WriteString("<p>No containers with active routes.</p>")
	} else {
		b.WriteString("<ul>")
		for _, name := range names {
			b.WriteString("<li><a href=\"/container/")
			b.WriteString(html.EscapeString(name))
			b.WriteString("\" target=\"_blank\" rel=\"noopener noreferrer\">/container/")
			b.WriteString(html.EscapeString(name))
			b.WriteString("</a></li>")
		}
		b.WriteString("</ul>")
	}
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

type PublishedEndpointRow struct {
	Hostname string
	Provider string
	Status   string
}

func isInternalHomepageRoute(hostname string) bool {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return false
	}
	base := strings.TrimPrefix(hostname, "http://")
	base = strings.TrimPrefix(base, "https://")
	return strings.EqualFold(base, "devcontainer-nginx") || strings.HasPrefix(strings.ToLower(base), "devcontainer-nginx.")
}

func formatHomepageStatus(route nginx.Route, provider, status string) string {
	provider = strings.TrimSpace(provider)
	status = strings.TrimSpace(status)
	if provider == "" {
		provider = strings.TrimSpace(route.Hostname)
	}

	switch strings.ToLower(status) {
	case "routed":
		if route.TargetPort > 0 {
			return fmt.Sprintf("routed to devcontainer %s on port %d", provider, route.TargetPort)
		}
		return fmt.Sprintf("routed to devcontainer %s", provider)
	case "partial":
		if route.TargetPort > 0 {
			return fmt.Sprintf("partially routed to devcontainer %s on port %d", provider, route.TargetPort)
		}
		return fmt.Sprintf("partially routed to devcontainer %s", provider)
	case "configured":
		if provider != "" {
			return fmt.Sprintf("tunnelled to load balancer %s", provider)
		}
		return "tunnelled to load balancer"
	case "running":
		if provider != "" {
			return fmt.Sprintf("tunnelled to load balancer %s", provider)
		}
		return "tunnelled to load balancer"
	case "pending":
		if provider != "" {
			return fmt.Sprintf("waiting for tunnel to load balancer %s", provider)
		}
		return "waiting for tunnel"
	default:
		return status
	}
}

func publishedEndpointRows(routes []nginx.Route, diagnostics []ContainerDiagnostic, tunnelManager *TunnelManager) []PublishedEndpointRow {
	if len(routes) == 0 {
		return nil
	}

	statusByIdentity := make(map[string]string)
	providerByIdentity := make(map[string]string)
	for _, row := range diagnostics {
		if row.Name == "" {
			continue
		}
		identity := strings.TrimPrefix(row.Name, "/")
		statusByIdentity[identity] = row.Status
		providerByIdentity[identity] = row.Name
	}
	if tunnelManager != nil {
		for _, entry := range tunnelManager.Snapshot() {
			name := strings.TrimSpace(entry.Alias)
			if name == "" {
				name = strings.TrimSpace(entry.Host)
			}
			if name == "" {
				name = entry.Name
			}
			status := tunnelEntryStatus(&entry)
			if name != "" {
				statusByIdentity[name] = status
				providerByIdentity[name] = name
			}
			if entry.ID != "" {
				identity := "aws-tunnel-" + entry.ID
				statusByIdentity[identity] = status
				providerByIdentity[identity] = name
			}
		}
	}

	seen := make(map[string]bool, len(routes))
	out := make([]PublishedEndpointRow, 0, len(routes))
	for _, route := range routes {
		hostname := strings.TrimSpace(route.Hostname)
		if hostname == "" {
			continue
		}
		if seen[hostname] {
			continue
		}
		if isInternalHomepageRoute(hostname) {
			continue
		}
		seen[hostname] = true

		provider := strings.TrimSpace(route.ContainerName)
		if provider == "" {
			provider = hostname
		}
		if resolvedProvider := providerByIdentity[strings.TrimPrefix(provider, "/")]; resolvedProvider != "" {
			provider = resolvedProvider
		}
		status := statusByIdentity[strings.TrimPrefix(provider, "/")]
		if status == "" {
			status = statusByIdentity[strings.TrimPrefix(route.ContainerName, "/")]
		}
		if status == "" {
			status = "configured"
		}
		status = formatHomepageStatus(route, provider, status)

		out = append(out, PublishedEndpointRow{
			Hostname: hostname,
			Provider: provider,
			Status:   status,
		})
	}
	return out
}

func renderOpenInNewTabIcon() string {
	return `<span class="new-tab-icon" aria-label="opens in new tab" title="opens in new tab">↗</span>`
}

func renderDebugPageHTML(rows []ContainerDiagnostic, revision uint64) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Debug</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head>")
	b.WriteString(fmt.Sprintf("<body data-page=\"debug\" data-current-revision=\"%d\">", revision))
	b.WriteString(renderPageHeader("Debug"))
	b.WriteString("<table><thead><tr><th>Process</th><th>Status</th><th>Reason</th><th>Routes</th></tr></thead><tbody>")

	if len(rows) == 0 {
		b.WriteString("<tr><td colspan=\"4\">No running containers were considered.</td></tr>")
	} else {
		for _, row := range rows {
			statusClass := "skip"
			switch row.Status {
			case "routed", "running":
				statusClass = "ok"
			case "partial":
				statusClass = "partial"
			}

			b.WriteString("<tr><td>")
			b.WriteString(renderContainerCell(row.Name, row.Metadata))
			b.WriteString("</td><td class=\"")
			b.WriteString(statusClass)
			b.WriteString("\">")
			b.WriteString(html.EscapeString(row.Status))
			b.WriteString("</td><td>")
			b.WriteString(html.EscapeString(row.Reason))
			b.WriteString("</td><td>")
			b.WriteString(renderRouteLinks(row.Routes))
			b.WriteString("</td></tr>")
		}
	}

	b.WriteString("</tbody></table>")
	b.WriteString("<h2>Actions</h2><ul>")
	b.WriteString("<li><a href=\"/aws-config\">Configure AWS credentials in this container</a></li>")
	b.WriteString("<li><a href=\"/tunnels\">Start or stop tunnels to services</a></li>")
	b.WriteString("<li><a href=\"/shell-launch\">Open a shell in a new tab</a></li>")
	b.WriteString("</ul>")
	b.WriteString(renderHelpSection("homepage"))
	b.WriteString("<h2>Debugging Container Services</h2><ul>")
	b.WriteString("<li><a href=\"/logs\">View logs</a></li>")
	b.WriteString("<li><a href=\"/shell-command-generator\">Generate shell commands for bastion host access</a></li>")
	b.WriteString("<li><a href=\"/nginx.conf\">Base nginx.conf</a></li>")
	b.WriteString("<li><a href=\"/generated.conf\">Generated nginx config</a></li>")
	b.WriteString("<li><a href=\"/nginx/config\">Full nginx config</a></li>")
	b.WriteString("<li><a href=\"/containers\">Container config</a></li>")
	b.WriteString("</ul>")
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}
