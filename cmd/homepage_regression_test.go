package main

import (
	"testing"

	"github.com/dev-tools/devcontainer-nginx/internal/discovery"
)

func TestDiagnoseContainerAllowsPublishedPortFallbackWithoutSharedNetwork(t *testing.T) {
	id := discovery.NewIdentifier()
	resolver := discovery.NewNetworkResolver("proxy")
	resolver.SetProxyNetworks(map[string]discovery.NetworkInfo{})

	container := &discovery.ContainerInfo{
		ID:     "abc123",
		Name:   "hello-world",
		Labels: map[string]string{"devcontainer.metadata": `{"forwardPorts":[8081]}`},
		ExposedPorts: []int{8081},
		PublishedPorts: map[string][]discovery.PortBinding{
			"8081/tcp": {{HostPort: "8082"}},
		},
		Networks: []string{},
		NetworkSettings: map[string]discovery.NetworkInfo{},
	}

	routes, diag := diagnoseContainer(container, id, resolver, "local.dev-gutools.co.uk")
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if diag.Status != "routed" {
		t.Fatalf("diag.Status = %q, want %q", diag.Status, "routed")
	}
	if routes[0].Hostname != "8082.hello-world.local.dev-gutools.co.uk" {
		t.Fatalf("routes[0].Hostname = %q, want %q", routes[0].Hostname, "8082.hello-world.local.dev-gutools.co.uk")
	}
	if routes[0].TargetPort != 8082 {
		t.Fatalf("routes[0].TargetPort = %d, want 8082", routes[0].TargetPort)
	}
}

