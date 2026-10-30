package discovery

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

// ContainerInfo holds metadata about a discovered container.
type ContainerInfo struct {
	ID              string
	Name            string
	Image           string
	Labels          map[string]string
	ExposedPorts    []int
	PublishedPorts  map[string][]PortBinding
	Networks        []string
	NetworkSettings map[string]NetworkInfo
	Running         bool
}

// PortBinding represents a port binding.
type PortBinding struct {
	HostIP   string
	HostPort string
}

// NetworkInfo represents network information.
type NetworkInfo struct {
	Gateway   string
	IPAddress string
}

// DockerClient wraps the Docker API client.
type DockerClient struct {
	client *client.Client
}

// NewDockerClient creates a new Docker client.
func NewDockerClient(ctx context.Context) (*DockerClient, error) {
	c, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Verify connection
	_, err = c.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping Docker daemon: %w", err)
	}

	log.Println("Connected to Docker daemon")
	return &DockerClient{client: c}, nil
}

// ListContainers returns all running containers.
func (dc *DockerClient) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	containers, err := dc.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		info := dc.parseContainer(c)
		result = append(result, info)
	}

	return result, nil
}

// parseContainer converts a Docker container to ContainerInfo.
func (dc *DockerClient) parseContainer(c types.Container) ContainerInfo {
	// Remove leading slash from name
	name := strings.TrimPrefix(c.Names[0], "/")

	// Parse exposed ports (deduplicated; Docker may report one entry per bind address)
	exposedPorts := []int{}
	seenExposed := make(map[int]bool)
	for _, p := range c.Ports {
		if p.Type == "tcp" && p.PrivatePort > 0 {
			port := int(p.PrivatePort)
			if !seenExposed[port] {
				exposedPorts = append(exposedPorts, port)
				seenExposed[port] = true
			}
		}
	}

	// Parse published ports
	publishedPorts := make(map[string][]PortBinding)
	for _, p := range c.Ports {
		if p.PublicPort > 0 {
			key := fmt.Sprintf("%d/tcp", p.PrivatePort)
			binding := PortBinding{
				HostIP:   p.IP,
				HostPort: fmt.Sprintf("%d", p.PublicPort),
			}
			publishedPorts[key] = append(publishedPorts[key], binding)
		}
	}

	log.Printf("parseContainer %s: exposed=%v published=%v labels=%v\n", name, exposedPorts, publishedPorts, c.Labels)

	// Get networks from inspect
	networks := []string{}
	for netName := range c.NetworkSettings.Networks {
		networks = append(networks, netName)
	}

	// Get image name
	imageName := c.Image
	if len(c.ImageID) > 0 {
		imageName = c.ImageID
	}

	return ContainerInfo{
		ID:             c.ID,
		Name:           name,
		Image:          imageName,
		Labels:         c.Labels,
		ExposedPorts:   exposedPorts,
		PublishedPorts: publishedPorts,
		Networks:       networks,
		Running:        true,
	}
}

// InspectContainer gets detailed info about a specific container.
func (dc *DockerClient) InspectContainer(ctx context.Context, containerID string) (*ContainerInfo, error) {
	resp, err := dc.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	// Remove leading slash from name
	name := strings.TrimPrefix(resp.Name, "/")

	// Parse exposed ports
	exposedPorts := []int{}
	for portStr := range resp.Config.ExposedPorts {
		// portStr format: "3000/tcp", "8000/udp", etc.
		portStrValue := string(portStr)
		if strings.Contains(portStrValue, "/tcp") {
			portNumStr := strings.Split(portStrValue, "/")[0]
			var port int
			fmt.Sscanf(portNumStr, "%d", &port)
			if port > 0 {
				exposedPorts = append(exposedPorts, port)
			}
		}
	}

	// Parse published ports
	publishedPorts := make(map[string][]PortBinding)
	if resp.HostConfig.PortBindings != nil {
		for portKey, bindings := range resp.HostConfig.PortBindings {
			key := portKey.Port()
			if key == "" {
				key = fmt.Sprintf("%s/tcp", portKey.Port())
			}
			for _, binding := range bindings {
				b := PortBinding{
					HostIP:   binding.HostIP,
					HostPort: binding.HostPort,
				}
				publishedPorts[key] = append(publishedPorts[key], b)
			}
		}
	}

	log.Printf("InspectContainer %s: exposed=%v published=%v labels=%v\n", name, exposedPorts, publishedPorts, resp.Config.Labels)

	// Get networks
	networks := []string{}
	networkSettings := make(map[string]NetworkInfo)
	if resp.NetworkSettings.Networks != nil {
		for netName, netInfo := range resp.NetworkSettings.Networks {
			networks = append(networks, netName)
			networkSettings[netName] = NetworkInfo{
				Gateway:   netInfo.Gateway,
				IPAddress: netInfo.IPAddress,
			}
		}
	}

	return &ContainerInfo{
		ID:              resp.ID,
		Name:            name,
		Image:           resp.Image,
		Labels:          resp.Config.Labels,
		ExposedPorts:    exposedPorts,
		PublishedPorts:  publishedPorts,
		Networks:        networks,
		NetworkSettings: networkSettings,
		Running:         resp.State.Running,
	}, nil
}

// WatchEvents returns a channel of Docker events.
// Yields container start, stop, die, destroy, rename events
// and network connect, disconnect events.
func (dc *DockerClient) WatchEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	msgChan, errChan := dc.client.Events(ctx, types.EventsOptions{})
	return msgChan, errChan
}

// ConnectToNetwork connects a container to a network.
func (dc *DockerClient) ConnectToNetwork(ctx context.Context, containerID, networkName string) error {
	return dc.client.NetworkConnect(ctx, networkName, containerID, nil)
}

// DisconnectFromNetwork disconnects a container from a network.
func (dc *DockerClient) DisconnectFromNetwork(ctx context.Context, containerID, networkName string) error {
	return dc.client.NetworkDisconnect(ctx, networkName, containerID, false)
}

// Close closes the Docker client connection.
func (dc *DockerClient) Close() error {
	return dc.client.Close()
}

// GetProxyContainerInfo returns info about this container.
func (dc *DockerClient) GetProxyContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	return dc.InspectContainer(ctx, containerID)
}
