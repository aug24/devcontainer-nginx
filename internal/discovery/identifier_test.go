package discovery

import (
	"reflect"
	"testing"
)

func TestExtractApplicationPortFromDevcontainerMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     int
	}{
		{
			name:     "object",
			metadata: `{"forwardPorts":[3000]}`,
			want:     3000,
		},
		{
			name:     "array",
			metadata: `[{"name":"feature"},{"forwardPorts":[5173]}]`,
			want:     5173,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NewIdentifier().ExtractApplicationPort(nil, nil, map[string]string{
				"devcontainer.metadata": test.metadata,
			})
			if got != test.want {
				t.Fatalf("ExtractApplicationPort() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestExtractPlainContainerFallbackRoutes(t *testing.T) {
	id := NewIdentifier()

	t.Run("single-port container", func(t *testing.T) {
		got := id.ExtractRouteTargets("zealous_chebyshev", nil, []int{80}, nil)
		want := []RouteTarget{{ProjectName: "zealous-chebyshev", ApplicationPort: 80}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ExtractRouteTargets() = %#v, want %#v", got, want)
		}
	})

	t.Run("multi-port container", func(t *testing.T) {
		got := id.ExtractRouteTargets("zealous_chebyshev", nil, []int{80, 8080}, nil)
		want := []RouteTarget{
			{ProjectName: "zealous-chebyshev", ApplicationPort: 8080},
			{ProjectName: "80.zealous-chebyshev", ApplicationPort: 80},
			{ProjectName: "8080.zealous-chebyshev", ApplicationPort: 8080},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ExtractRouteTargets() = %#v, want %#v", got, want)
		}
	})

	t.Run("multi-published-host-ports", func(t *testing.T) {
		published := map[string][]PortBinding{
			"80/tcp": {
				{HostPort: "8081"},
				{HostPort: "8082"},
			},
		}
		got := id.ExtractRouteTargets("hello-world", nil, []int{80}, published)
		want := []RouteTarget{
			{ProjectName: "8081.hello-world", ApplicationPort: 8081},
			{ProjectName: "8082.hello-world", ApplicationPort: 8082},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ExtractRouteTargets() = %#v, want %#v", got, want)
		}
	})
}
