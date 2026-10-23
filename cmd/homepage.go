package main

import (
	"fmt"
	"html"
	"log"
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

func renderHomepageHTML(rows []ContainerDiagnostic, revision uint64) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Home</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:8px;vertical-align:top;text-align:left}th{background:#f5f5f5}.ok{color:#0a7}.skip{color:#b50}.partial{color:#a60}.proto-badge{display:inline-block;font-size:0.7em;font-weight:700;padding:1px 5px;border-radius:3px;margin-right:4px;vertical-align:middle;letter-spacing:.04em}.proto-https{background:#1a6fa8;color:#fff}.proto-http{background:#888;color:#fff}.container-label{position:relative;display:inline-block;cursor:pointer}.container-name{display:inline-block}.container-popup{position:absolute;left:0;top:calc(100% + 8px);background:#1f2937;color:#fff;padding:8px 10px;border-radius:6px;font-size:12px;line-height:1.5;white-space:pre-line;max-width:min(420px,70vw);width:max-content;max-height:60vh;overflow:auto;visibility:hidden;opacity:0;pointer-events:none;transition:opacity .12s ease,visibility .12s ease;z-index:9999;box-shadow:0 4px 18px rgba(0,0,0,.25)}.container-popup:before{content:\"\";position:absolute;left:12px;bottom:100%;border:6px solid transparent;border-bottom-color:#1f2937}.container-label.open .container-popup{visibility:visible;opacity:1;pointer-events:auto}.container-popup a{color:#dbeafe}</style>")
	b.WriteString("<script>(function(){document.querySelectorAll('.container-label').forEach(label=>{const toggle=function(e){if(e&&e.stopPropagation)e.stopPropagation();const isOpen=label.classList.contains('open');document.querySelectorAll('.container-label').forEach(other=>{if(other!==label)other.classList.remove('open');});if(isOpen){label.classList.remove('open');return;}label.classList.add('open');};label.addEventListener('click', toggle);label.addEventListener('keydown', function(e){if(e.key==='Enter'||e.key===' '){e.preventDefault();toggle(e);}});document.addEventListener('click', function(e){if(!label.contains(e.target))label.classList.remove('open');});});})();</script>")
	b.WriteString("</head><body>")
	b.WriteString(renderPageHeader("Home"))
	b.WriteString("<table><thead><tr><th>Container</th><th>Status</th><th>Reason</th><th>Routes</th></tr></thead><tbody>")

	if len(rows) == 0 {
		b.WriteString("<tr><td colspan=\"4\">No running containers were considered.</td></tr>")
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
	b.WriteString("<script>(function(){document.querySelectorAll('.container-label').forEach(label=>{const toggle=function(e){if(e&&e.stopPropagation)e.stopPropagation();const isOpen=label.classList.contains('open');document.querySelectorAll('.container-label').forEach(other=>{if(other!==label)other.classList.remove('open');});if(isOpen){label.classList.remove('open');return;}label.classList.add('open');};label.addEventListener('click', toggle);label.addEventListener('keydown', function(e){if(e.key==='Enter'||e.key===' '){e.preventDefault();toggle(e);}});document.addEventListener('click', function(e){if(!label.contains(e.target))label.classList.remove('open');});});})();</script>")
	b.WriteString(fmt.Sprintf("<script>(function(){const current=%d;async function check(){try{const res=await fetch('/homepage/version',{cache:'no-store'});if(!res.ok)return;const next=Number((await res.text()).trim());if(Number.isFinite(next)&&next>current){location.reload();}}catch(_e){}}setInterval(check,2000);})();</script>", revision))

	b.WriteString("<h2>Useful links</h2><ul>")
	b.WriteString("<li><a href=\"/nginx.conf\">/nginx.conf</a></li>")
	b.WriteString("<li><a href=\"/generated.conf\">/generated.conf</a></li>")
	b.WriteString("<li><a href=\"/nginx/config\">/nginx/config</a></li>")
	b.WriteString("<li><a href=\"/containers\">/containers</a></li>")
	b.WriteString("<li><a href=\"/aws-config\">/aws-config</a></li>")
	b.WriteString("<li><a href=\"/logs\">/logs</a></li>")
	b.WriteString("<li><a href=\"/tunnels\">/tunnels</a></li>")
	b.WriteString("<li><a href=\"/shell-command-generator\">/shell-command-generator</a></li>")
	b.WriteString("</ul></body></html>")
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
	return fmt.Sprintf("<h1><a href=\"/\" style=\"color:inherit;text-decoration:none;\">%s</a>: %s</h1>", html.EscapeString("Devcontainer Nginx"), html.EscapeString(title))
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

func renderContainersHTML(names []string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Containers</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}ul{line-height:1.8}</style>")
	b.WriteString("</head><body>")
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
	b.WriteString("</body></html>")
	return b.String()
}
