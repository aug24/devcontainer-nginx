package main

import (
	"reflect"
	"testing"
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
