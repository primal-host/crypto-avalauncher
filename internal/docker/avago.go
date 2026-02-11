package docker

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// AvagoParams defines parameters for creating an AvalancheGo container.
type AvagoParams struct {
	Name        string // node name (used in container name and volume names)
	Image       string // Docker image reference
	NetworkName string // Docker network to attach to (e.g. "avax")
	NetworkID   string // Avalanche network: mainnet, fuji, local
	StakingPort   int      // host port for P2P staking (9651)
	ExposeHTTP    bool     // whether to publish HTTP API port to host
	TrackSubnets  []string // L1 subnet IDs for AVAGO_TRACK_SUBNETS
}

// ContainerName returns the Docker container name for this node.
func (p *AvagoParams) ContainerName() string {
	return "avax-" + p.Name
}

// VolumeDB returns the database volume name.
func (p *AvagoParams) VolumeDB() string {
	return "avax-" + p.Name + "-db"
}

// VolumeStaking returns the staking volume name.
func (p *AvagoParams) VolumeStaking() string {
	return "avax-" + p.Name + "-staking"
}

// VolumeLogs returns the logs volume name.
func (p *AvagoParams) VolumeLogs() string {
	return "avax-" + p.Name + "-logs"
}

// BuildContainerConfig returns Docker container, host, and networking configs
// for an AvalancheGo node.
func (p *AvagoParams) BuildContainerConfig() (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	env := []string{
		"AVAGO_NETWORK_ID=" + p.NetworkID,
		"AVAGO_HTTP_HOST=0.0.0.0",
		"AVAGO_HTTP_ALLOWED_HOSTS=*",
	}
	if p.NetworkID == "local" {
		// Single-node local network: disable sybil protection so the node
		// self-registers as a validator and consensus starts immediately.
		// Empty bootstrap IPs/IDs prevent peer discovery attempts.
		env = append(env,
			"AVAGO_SYBIL_PROTECTION_ENABLED=false",
			"AVAGO_BOOTSTRAP_IPS=",
			"AVAGO_BOOTSTRAP_IDS=",
			"AVAGO_PUBLIC_IP=127.0.0.1",
		)
	} else {
		env = append(env, "AVAGO_PUBLIC_IP_RESOLUTION_SERVICE=opendns")
	}
	if len(p.TrackSubnets) > 0 {
		env = append(env, "AVAGO_TRACK_SUBNETS="+strings.Join(p.TrackSubnets, ","))
	}

	exposedPorts := nat.PortSet{
		"9650/tcp": struct{}{},
		"9651/tcp": struct{}{},
	}

	portBindings := nat.PortMap{
		"9651/tcp": []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", p.StakingPort)},
		},
	}
	if p.ExposeHTTP {
		portBindings["9650/tcp"] = []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: "9650"},
		}
	}

	cc := &container.Config{
		Image:        p.Image,
		Env:          env,
		ExposedPorts: exposedPorts,
		Labels: map[string]string{
			LabelManagedBy: ManagedByValue,
			LabelNodeName:  p.Name,
		},
	}

	hc := &container.HostConfig{
		PortBindings: portBindings,
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: p.VolumeDB(), Target: "/root/.avalanchego/db"},
			{Type: mount.TypeVolume, Source: p.VolumeStaking(), Target: "/root/.avalanchego/staking"},
			{Type: mount.TypeVolume, Source: p.VolumeLogs(), Target: "/root/.avalanchego/logs"},
		},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}

	nc := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			p.NetworkName: {},
		},
	}

	return cc, hc, nc
}
