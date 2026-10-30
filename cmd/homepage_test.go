package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	nginx "github.com/dev-tools/devcontainer-nginx/internal/nginx"
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
	}}, 42, []PublishedEndpointRow{{
		Hostname: "frontend.local.dev",
		Provider: "frontend",
		Status:   "routed",
	}})

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
	if !strings.Contains(html, "frontend.local.dev") {
		t.Fatalf("homepage HTML missing endpoint table row")
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

func TestTunnelAPIRoutesMatchDedicatedHandlers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tunnels", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/tunnels/stop/", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/tunnels/start/", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/tunnels/", func(w http.ResponseWriter, r *http.Request) {})

	cases := []struct {
		path       string
		wantPattern string
	}{
		{path: "/api/tunnels", wantPattern: "/api/tunnels"},
		{path: "/api/tunnels/start/abc123", wantPattern: "/api/tunnels/start/"},
		{path: "/api/tunnels/stop/abc123", wantPattern: "/api/tunnels/stop/"},
	}

	for _, tc := range cases {
		req, err := http.NewRequest(http.MethodPost, tc.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("NewRequest(%q): %v", tc.path, err)
		}
		_, pattern := mux.Handler(req)
		if pattern != tc.wantPattern {
			t.Fatalf("handler for %q = %q, want %q", tc.path, pattern, tc.wantPattern)
		}
	}
}

func TestTunnelPageAcceptsFormPost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tunnels", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			w.WriteHeader(http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	req, err := http.NewRequest(http.MethodPost, "/tunnels", strings.NewReader("name=demo&stack=frontend&stage=CODE&app=demo&alb=demo-alb"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("POST /tunnels returned %d, want %d", resp.Code, http.StatusSeeOther)
	}
}

func TestAWSConfigFormUsesPost(t *testing.T) {
	html := renderAWSConfigPage(AWSConfig{BootstrapCommand: "aws configure set --profile frontend region us-east-1"})
	if !strings.Contains(html, "<form id=\"aws-form\" method=\"post\" action=\"/aws-config\">") {
		t.Fatalf("AWS config form missing explicit POST action: %s", html)
	}
	if !strings.Contains(html, "Paste and Save") {
		t.Fatalf("AWS config page missing paste-and-save action: %s", html)
	}
	if !strings.Contains(html, "<button type=\"submit\">Save</button>") {
		t.Fatalf("AWS config page missing renamed save button: %s", html)
	}
	if strings.Contains(html, "Save AWS config") {
		t.Fatalf("AWS config page still contains old button label: %s", html)
	}
}

func TestRenderShellPageUsesEscapedTerminalNewlines(t *testing.T) {
	html := renderShellPage("frontend", "eu-west-1")
	if !strings.Contains(html, "term.write('\\r\\n');") {
		t.Fatalf("shell page missing escaped CRLF terminal writing: %s", html)
	}
	if strings.Contains(html, "term.write('\r\n');") {
		t.Fatalf("shell page contains raw CRLFs in JS literal: %s", html)
	}
	if !strings.Contains(html, "/shell/ws?profile=") {
		t.Fatalf("shell page missing websocket route: %s", html)
	}
}

func TestFormatHomepageStatusUsesHostnamePortForRoutedEndpoints(t *testing.T) {
	route := nginx.Route{
		Hostname:      "8082.hello-world.local.dev-gutools.co.uk",
		ContainerName: "hello-world",
		TargetPort:    8081,
	}

	got := formatHomepageStatus(route, "hello-world", "routed")
	want := "routed to container hello-world on port 8082"
	if got != want {
		t.Fatalf("formatHomepageStatus() = %q, want %q", got, want)
	}
}
