package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
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
	mu   sync.RWMutex
	rows []ContainerDiagnostic
}

func (ds *DiagnosticsState) Set(rows []ContainerDiagnostic) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.rows = make([]ContainerDiagnostic, len(rows))
	copy(ds.rows, rows)
}

func (ds *DiagnosticsState) Snapshot() []ContainerDiagnostic {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	rows := make([]ContainerDiagnostic, len(ds.rows))
	copy(rows, ds.rows)
	return rows
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

func summarizeContainerMetadata(containerInfo *discovery.ContainerInfo) string {
	var parts []string

	if name := containerInfo.Labels["com.intellij.devcontainer.presentable.name"]; name != "" {
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
	labelKeys := []string{
		"com.intellij.devcontainer.model",
		"com.intellij.devcontainer.initial.model",
	}

	seenPorts := make(map[int]bool)
	portLabels := make(map[int]string)
	var forwardedPorts []int

	for _, key := range labelKeys {
		raw, ok := labels[key]
		if !ok || raw == "" {
			continue
		}

		var doc map[string]any
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			continue
		}

		for _, port := range parseDiagnosticsForwardedPorts(doc) {
			if !seenPorts[port] {
				seenPorts[port] = true
				forwardedPorts = append(forwardedPorts, port)
			}
		}

		for port, label := range parseDiagnosticsPortsAttributes(doc) {
			portLabels[port] = label
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

func renderDiagnosticsHTML(rows []ContainerDiagnostic) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>devcontainer-nginx diagnostics</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:8px;vertical-align:top;text-align:left}th{background:#f5f5f5}.ok{color:#0a7}.skip{color:#b50}.partial{color:#a60}</style>")
	b.WriteString("</head><body>")
	b.WriteString("<h1>devcontainer-nginx diagnostics</h1>")
	b.WriteString("<table><thead><tr><th>Container</th><th>Status</th><th>Reason</th><th>Routes</th><th>Metadata</th></tr></thead><tbody>")

	if len(rows) == 0 {
		b.WriteString("<tr><td colspan=\"5\">No running containers were considered.</td></tr>")
	} else {
		for _, row := range rows {
			statusClass := "skip"
			switch row.Status {
			case "routed":
				statusClass = "ok"
			case "partial":
				statusClass = "partial"
			}

			b.WriteString("<tr><td>")
			b.WriteString(html.EscapeString(row.Name))
			b.WriteString("</td><td class=\"")
			b.WriteString(statusClass)
			b.WriteString("\">")
			b.WriteString(html.EscapeString(row.Status))
			b.WriteString("</td><td>")
			b.WriteString(html.EscapeString(row.Reason))
			b.WriteString("</td><td>")
			b.WriteString(renderRouteLinks(row.Routes))
			b.WriteString("</td><td>")
			b.WriteString(renderMetadataLines(row.Metadata))
			b.WriteString("</td></tr>")
		}
	}

	b.WriteString("</tbody></table>")
	b.WriteString("<h2>Useful links</h2><ul>")
	b.WriteString("<li><a href=\"/nginx.conf\">/nginx.conf</a></li>")
	b.WriteString("<li><a href=\"/generated.conf\">/generated.conf</a></li>")
	b.WriteString("<li><a href=\"/nginx/config\">/nginx/config</a></li>")
	b.WriteString("<li><a href=\"/containers\">/containers</a></li>")
	b.WriteString("</ul></body></html>")
	return b.String()
}

func renderRouteLinks(routes string) string {
	if routes == "" {
		return ""
	}

	parts := strings.Split(routes, "; ")
	links := make([]string, 0, len(parts))
	for _, part := range parts {
		host := part
		if idx := strings.Index(part, " -> "); idx >= 0 {
			host = part[:idx]
		}
		if host == "" {
			continue
		}
		escaped := html.EscapeString(host)
		links = append(links, fmt.Sprintf(`<a href="https://%s" target="_blank" rel="noopener noreferrer">%s</a>`, escaped, escaped))
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

func renderContainersHTML(names []string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>devcontainer-nginx containers</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}ul{line-height:1.8}</style>")
	b.WriteString("</head><body>")
	b.WriteString("<h1>Containers</h1>")
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
	b.WriteString("</body></html>")
	return b.String()
}
