package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	LabelManagedBy = "managed-by"
	LabelNodeName  = "avalauncher.node-name"
	ManagedByValue = "avalauncher"
)

// Client wraps the Docker SDK client.
type Client struct {
	cli *client.Client
}

// New creates a Docker client. host may be empty for the default socket.
func New(host string) (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// NewSSH creates a Docker client that connects over SSH using connhelper.
func NewSSH(sshAddr string) (*Client, error) {
	helper, err := connhelper.GetConnectionHelper("ssh://" + sshAddr)
	if err != nil {
		return nil, fmt.Errorf("ssh connhelper: %w", err)
	}
	cli, err := client.NewClientWithOpts(
		client.WithHost(helper.Host),
		client.WithDialContext(helper.Dialer),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker ssh client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close releases Docker client resources.
func (c *Client) Close() error {
	return c.cli.Close()
}

// Ping checks Docker daemon connectivity.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// HostName returns the Docker host's hostname via daemon info.
func (c *Client) HostName(ctx context.Context) (string, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Name, nil
}

// HostInfo holds summary information about a Docker host.
type HostInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	CPUs          int    `json:"cpus"`
	MemoryMB      int64  `json:"memory_mb"`
	DockerVersion string `json:"docker_version"`
}

// HostInfo returns structured information about the Docker host.
func (c *Client) HostInfo(ctx context.Context) (*HostInfo, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &HostInfo{
		Hostname:      info.Name,
		OS:            info.OperatingSystem,
		Architecture:  info.Architecture,
		CPUs:          info.NCPU,
		MemoryMB:      info.MemTotal / (1024 * 1024),
		DockerVersion: info.ServerVersion,
	}, nil
}

// EnsureNetwork creates a bridge network if it doesn't exist.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	networks, err := c.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	for _, n := range networks {
		if n.Name == name {
			return nil
		}
	}
	_, err = c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("create network %s: %w", name, err)
	}
	slog.Info("created docker network", "name", name)
	return nil
}

// PullImage pulls a container image. The caller should read and close the
// returned reader to follow progress.
func (c *Client) PullImage(ctx context.Context, ref string) (io.ReadCloser, error) {
	return c.cli.ImagePull(ctx, ref, image.PullOptions{})
}

// ImageExists checks if an image is available locally.
func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, _, err := c.cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ContainerCreate creates a container with the given configs.
func (c *Client) ContainerCreate(ctx context.Context, name string, cc *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) (string, error) {
	resp, err := c.cli.ContainerCreate(ctx, cc, hc, nc, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container %s: %w", name, err)
	}
	return resp.ID, nil
}

// ContainerStart starts a created container.
func (c *Client) ContainerStart(ctx context.Context, id string) error {
	return c.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// ContainerStop stops a running container with a timeout.
func (c *Client) ContainerStop(ctx context.Context, id string, timeoutSec int) error {
	timeout := timeoutSec
	return c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

// ContainerRemove removes a container, optionally with its volumes.
func (c *Client) ContainerRemove(ctx context.Context, id string, removeVolumes bool) error {
	return c.cli.ContainerRemove(ctx, id, container.RemoveOptions{
		RemoveVolumes: removeVolumes,
		Force:         true,
	})
}

// ContainerInspect returns container details.
func (c *Client) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	return c.cli.ContainerInspect(ctx, id)
}

// ContainerLogs returns a reader for container log output.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
	})
}

// ManagedContainer holds summary info for a managed container.
type ManagedContainer struct {
	ID    string
	Name  string
	State string
}

// ListManagedContainers returns all containers with the managed-by=avalauncher label.
func (c *Client) ListManagedContainers(ctx context.Context) ([]ManagedContainer, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: newFilterArgs(LabelManagedBy, ManagedByValue),
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	result := make([]ManagedContainer, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = ctr.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		result = append(result, ManagedContainer{
			ID:    ctr.ID,
			Name:  name,
			State: ctr.State,
		})
	}
	return result, nil
}
