package main

import (
	"reflect"
	"strings"
	"testing"
	"os"
	"path/filepath"
)

func TestExtractDiagnosticsPortMetadata(t *testing.T) {
	tests := []struct {
		name       string
		metadata   string
		wantPorts  []int
		wantLabels map[int]string
	}{
		{
			name:       "object",
			metadata:   `{"forwardPorts":[3000],"portsAttributes":{"3000":{"label":"App"}}}`,
			wantPorts:  []int{3000},
			wantLabels: map[int]string{3000: "App"},
		},
		{
			name:       "array",
			metadata:   `[{"name":"feature"},{"forwardPorts":[5173],"portsAttributes":{"5173":{"label":"Web"}}}]`,
			wantPorts:  []int{5173},
			wantLabels: map[int]string{5173: "Web"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ports, labels := extractDiagnosticsPortMetadata(map[string]string{
				"devcontainer.metadata": test.metadata,
			})

			if !reflect.DeepEqual(ports, test.wantPorts) {
				t.Errorf("ports = %v, want %v", ports, test.wantPorts)
			}
			if !reflect.DeepEqual(labels, test.wantLabels) {
				t.Errorf("labels = %v, want %v", labels, test.wantLabels)
			}
		})
	}
}

func TestTunnelDiagnosticsIncludesTunnelRows(t *testing.T) {
	tm := &TunnelManager{
		tunnels: map[string]*TunnelEntry{
			"t-1": {
				ID:            "t-1",
				Alias:         "frontend",
				Profile:       "ops",
				Region:        "eu-west-1",
				Alb:           "frontend-alb",
				TargetPort:    443,
				LocalEndpoint: "127.0.0.1:40000",
				Status:        "running",
				LastOutput:    "tunnel active",
			},
		},
	}

	rows := tunnelDiagnostics(tm, "local.dev")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Name != "frontend" {
		t.Fatalf("rows[0].Name = %q, want %q", rows[0].Name, "frontend")
	}
	if rows[0].Status != "running" {
		t.Fatalf("rows[0].Status = %q, want %q", rows[0].Status, "running")
	}
	if !strings.Contains(rows[0].Routes, "frontend.local.dev") {
		t.Fatalf("rows[0].Routes = %q, want hostname to include base domain", rows[0].Routes)
	}
}

func TestRenderHomepageHTMLUsesSharedStaticAssets(t *testing.T) {
	html := renderHomepageHTML([]ContainerDiagnostic{{
		Name:   "frontend",
		Status: "routed",
		Reason: "container",
		Routes: "frontend.local.dev -> 127.0.0.1:3000",
	}}, 42)

	if !strings.Contains(html, "data-page=\"homepage\"") {
		t.Fatalf("homepage HTML missing page marker")
	}
	if !strings.Contains(html, "/static/styles.css") {
		t.Fatalf("homepage HTML missing stylesheet reference")
	}
	if !strings.Contains(html, "/static/app.js") {
		t.Fatalf("homepage HTML missing shared script reference")
	}
	if strings.Contains(html, "<style>") {
		t.Fatalf("homepage HTML still contains inline styles")
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("homepage HTML still contains inline scripts")
	}
}

func TestStatusPaletteUsesGreenForRunning(t *testing.T) {
	cssPath := filepath.Join("..", "static", "styles.css")
	css, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("read css: %v", err)
	}
	content := string(css)

	if !strings.Contains(content, "--ok-dark: #085a39;") {
		t.Fatalf("running green palette missing from stylesheet: %s", content)
	}
	if !strings.Contains(content, ".status-ok {\n  color: var(--ok-dark);") {
		t.Fatalf("status-ok selector missing darker green color: %s", content)
	}
	if !strings.Contains(content, ".status-failed {\n  color: var(--danger);") {
		t.Fatalf("status-failed selector missing red color: %s", content)
	}
}
