package discovery

import "testing"

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
