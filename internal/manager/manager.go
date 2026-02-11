package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/primal-host/avalauncher/internal/docker"
)

// Manager handles node lifecycle, health polling, and event logging.
type Manager struct {
	dc             *docker.Client
	pool           *pgxpool.Pool
	avagoImage     string
	avagoNetwork   string // avalanche network id (mainnet, fuji, local)
	avaxDockerNet  string // docker network name
	healthInterval time.Duration
	localHostLabel string // resolved Docker host name

	stopPoller chan struct{}
	pollerWg   sync.WaitGroup
}

// New creates a Manager, ensures the Docker network, upserts the local host
// row, and runs startup reconciliation.
func New(ctx context.Context, dc *docker.Client, pool *pgxpool.Pool, avagoImage, avagoNetwork, avaxDockerNet string, healthInterval time.Duration) (*Manager, error) {
	m := &Manager{
		dc:             dc,
		pool:           pool,
		avagoImage:     avagoImage,
		avagoNetwork:   avagoNetwork,
		avaxDockerNet:  avaxDockerNet,
		healthInterval: healthInterval,
		stopPoller:     make(chan struct{}),
	}

	if err := dc.EnsureNetwork(ctx, avaxDockerNet); err != nil {
		return nil, fmt.Errorf("ensure network: %w", err)
	}

	// Resolve local host label from Docker daemon.
	if name, err := dc.HostName(ctx); err == nil && name != "" {
		m.localHostLabel = name
	} else {
		m.localHostLabel = "local"
	}

	// Upsert the "local" host row.
	_, err := pool.Exec(ctx, `
		INSERT INTO hosts (name, ssh_addr, status)
		VALUES ('local', '', 'online')
		ON CONFLICT (name) DO UPDATE SET status = 'online', updated_at = now()`)
	if err != nil {
		return nil, fmt.Errorf("upsert local host: %w", err)
	}

	if err := m.reconcile(ctx); err != nil {
		slog.Warn("reconciliation error", "error", err)
	}

	return m, nil
}

// Node represents a node row from the database.
type Node struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	HostID       int64     `json:"host_id"`
	Image        string    `json:"image"`
	NodeID       string    `json:"node_id,omitempty"`
	ContainerID  string    `json:"container_id,omitempty"`
	HTTPPort     int       `json:"http_port"`
	StakingPort  int       `json:"staking_port"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateNodeRequest holds parameters for creating a new node.
type CreateNodeRequest struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	StakingPort int    `json:"staking_port"`
	ExposeHTTP  bool   `json:"expose_http"`
}

// CreateNode validates inputs, pulls the image, creates and starts a container,
// and inserts a node row. Image pull happens in a background goroutine.
func (m *Manager) CreateNode(ctx context.Context, req CreateNodeRequest) (*Node, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.StakingPort == 0 {
		req.StakingPort = 9651
	}
	if req.Image == "" {
		req.Image = m.avagoImage
	}

	// Check name uniqueness.
	var exists bool
	err := m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM nodes WHERE name=$1)", req.Name).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check name: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("node %q already exists", req.Name)
	}

	// Check staking port conflicts.
	err = m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM nodes WHERE staking_port=$1 AND status NOT IN ('stopped','failed'))", req.StakingPort).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check port: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("staking port %d already in use", req.StakingPort)
	}

	// Get local host ID.
	var hostID int64
	err = m.pool.QueryRow(ctx, "SELECT id FROM hosts WHERE name='local'").Scan(&hostID)
	if err != nil {
		return nil, fmt.Errorf("get local host: %w", err)
	}

	// Insert node in creating state.
	var node Node
	err = m.pool.QueryRow(ctx, `
		INSERT INTO nodes (name, host_id, image, staking_port, status)
		VALUES ($1, $2, $3, $4, 'creating')
		RETURNING id, name, host_id, image, node_id, container_id, http_port, staking_port, status, created_at, updated_at`,
		req.Name, hostID, req.Image, req.StakingPort,
	).Scan(&node.ID, &node.Name, &node.HostID, &node.Image, &node.NodeID,
		&node.ContainerID, &node.HTTPPort, &node.StakingPort, &node.Status,
		&node.CreatedAt, &node.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert node: %w", err)
	}

	m.logEvent(ctx, "node.creating", node.Name, "Creating node", nil)

	// Pull + create + start in background.
	go m.provisionNode(node.ID, req)

	return &node, nil
}

// provisionNode pulls the image, creates and starts the container.
func (m *Manager) provisionNode(nodeID int64, req CreateNodeRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	setStatus := func(status, msg string) {
		_, err := m.pool.Exec(ctx, "UPDATE nodes SET status=$1, updated_at=now() WHERE id=$2", status, nodeID)
		if err != nil {
			slog.Error("update node status", "error", err, "node_id", nodeID)
		}
		m.logEvent(ctx, "node."+status, req.Name, msg, nil)
	}

	// Pull image.
	slog.Info("pulling image", "image", req.Image, "node", req.Name)
	reader, err := m.dc.PullImage(ctx, req.Image)
	if err != nil {
		slog.Error("pull image failed", "error", err, "node", req.Name)
		setStatus("failed", fmt.Sprintf("Image pull failed: %v", err))
		return
	}
	// Consume pull output to completion.
	io.Copy(io.Discard, reader)
	reader.Close()
	slog.Info("image pulled", "image", req.Image, "node", req.Name)

	// Build container config.
	params := &docker.AvagoParams{
		Name:        req.Name,
		Image:       req.Image,
		NetworkName: m.avaxDockerNet,
		NetworkID:   m.avagoNetwork,
		StakingPort: req.StakingPort,
		ExposeHTTP:  req.ExposeHTTP,
	}
	cc, hc, nc := params.BuildContainerConfig()

	// Create container.
	containerName := params.ContainerName()
	containerID, err := m.dc.ContainerCreate(ctx, containerName, cc, hc, nc)
	if err != nil {
		slog.Error("create container failed", "error", err, "node", req.Name)
		setStatus("failed", fmt.Sprintf("Container create failed: %v", err))
		return
	}

	// Update container_id.
	_, err = m.pool.Exec(ctx, "UPDATE nodes SET container_id=$1, updated_at=now() WHERE id=$2", containerID, nodeID)
	if err != nil {
		slog.Error("update container_id", "error", err, "node_id", nodeID)
	}

	// Start container.
	if err := m.dc.ContainerStart(ctx, containerID); err != nil {
		slog.Error("start container failed", "error", err, "node", req.Name)
		setStatus("failed", fmt.Sprintf("Container start failed: %v", err))
		return
	}

	setStatus("running", "Node started")
	slog.Info("node started", "node", req.Name, "container", containerID[:12])
}

// ListNodes returns all nodes.
func (m *Manager) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT id, name, host_id, image, node_id, container_id, http_port, staking_port, status, created_at, updated_at
		FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.HostID, &n.Image, &n.NodeID,
			&n.ContainerID, &n.HTTPPort, &n.StakingPort, &n.Status,
			&n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetNode returns a single node by ID.
func (m *Manager) GetNode(ctx context.Context, id int64) (*Node, error) {
	var n Node
	err := m.pool.QueryRow(ctx, `
		SELECT id, name, host_id, image, node_id, container_id, http_port, staking_port, status, created_at, updated_at
		FROM nodes WHERE id=$1`, id).
		Scan(&n.ID, &n.Name, &n.HostID, &n.Image, &n.NodeID,
			&n.ContainerID, &n.HTTPPort, &n.StakingPort, &n.Status,
			&n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// StartNode starts a stopped node's container.
func (m *Manager) StartNode(ctx context.Context, id int64) error {
	node, err := m.GetNode(ctx, id)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}
	if node.ContainerID == "" {
		return fmt.Errorf("node %q has no container", node.Name)
	}
	if node.Status == "running" {
		return fmt.Errorf("node %q is already running", node.Name)
	}

	if err := m.dc.ContainerStart(ctx, node.ContainerID); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	_, err = m.pool.Exec(ctx, "UPDATE nodes SET status='running', updated_at=now() WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	m.logEvent(ctx, "node.started", node.Name, "Node started", nil)
	return nil
}

// StopNode stops a running node's container.
func (m *Manager) StopNode(ctx context.Context, id int64) error {
	node, err := m.GetNode(ctx, id)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}
	if node.ContainerID == "" {
		return fmt.Errorf("node %q has no container", node.Name)
	}
	if node.Status == "stopped" {
		return fmt.Errorf("node %q is already stopped", node.Name)
	}

	if err := m.dc.ContainerStop(ctx, node.ContainerID, 30); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}

	_, err = m.pool.Exec(ctx, "UPDATE nodes SET status='stopped', updated_at=now() WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	m.logEvent(ctx, "node.stopped", node.Name, "Node stopped", nil)
	return nil
}

// DeleteNode stops and removes a node's container and DB row.
func (m *Manager) DeleteNode(ctx context.Context, id int64, removeVolumes bool) error {
	node, err := m.GetNode(ctx, id)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	if node.ContainerID != "" {
		// Stop if running (ignore errors — may already be stopped).
		_ = m.dc.ContainerStop(ctx, node.ContainerID, 10)
		if err := m.dc.ContainerRemove(ctx, node.ContainerID, removeVolumes); err != nil {
			// If container not found, that's fine.
			if !strings.Contains(err.Error(), "No such container") {
				return fmt.Errorf("remove container: %w", err)
			}
		}
	}

	_, err = m.pool.Exec(ctx, "DELETE FROM nodes WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("delete node row: %w", err)
	}

	detail := map[string]any{"remove_volumes": removeVolumes}
	m.logEvent(ctx, "node.deleted", node.Name, "Node deleted", detail)
	return nil
}

// NodeLogs returns a reader for the node's container logs.
func (m *Manager) NodeLogs(ctx context.Context, id int64, tail string) (io.ReadCloser, error) {
	node, err := m.GetNode(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	if node.ContainerID == "" {
		return nil, fmt.Errorf("node %q has no container", node.Name)
	}
	if tail == "" {
		tail = "100"
	}
	return m.dc.ContainerLogs(ctx, node.ContainerID, tail)
}

// Event represents an audit event row.
type Event struct {
	ID        int64          `json:"id"`
	EventType string         `json:"event_type"`
	Target    string         `json:"target"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// ListEvents returns recent events.
func (m *Manager) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.pool.Query(ctx, `
		SELECT id, event_type, target, message, details, created_at
		FROM events ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var details []byte
		if err := rows.Scan(&e.ID, &e.EventType, &e.Target, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(details) > 0 {
			json.Unmarshal(details, &e.Details)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// StartHealthPoller begins a background loop that checks running nodes.
func (m *Manager) StartHealthPoller() {
	m.pollerWg.Add(1)
	go func() {
		defer m.pollerWg.Done()
		ticker := time.NewTicker(m.healthInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopPoller:
				return
			case <-ticker.C:
				m.pollHealth()
			}
		}
	}()
	slog.Info("health poller started", "interval", m.healthInterval)
}

// StopHealthPoller stops the background health check loop.
func (m *Manager) StopHealthPoller() {
	close(m.stopPoller)
	m.pollerWg.Wait()
	slog.Info("health poller stopped")
}

func (m *Manager) pollHealth() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	nodes, err := m.ListNodes(ctx)
	if err != nil {
		slog.Error("poll health: list nodes", "error", err)
		return
	}

	for _, node := range nodes {
		if node.Status != "running" && node.Status != "unhealthy" {
			continue
		}
		if node.ContainerID == "" {
			continue
		}

		healthy := m.checkNodeHealth(ctx, node)
		newStatus := node.Status

		if healthy && node.Status == "unhealthy" {
			newStatus = "running"
		} else if !healthy && node.Status == "running" {
			// Check if container is actually running.
			info, err := m.dc.ContainerInspect(ctx, node.ContainerID)
			if err != nil || !info.State.Running {
				newStatus = "stopped"
			} else {
				newStatus = "unhealthy"
			}
		}

		if newStatus != node.Status {
			_, err := m.pool.Exec(ctx, "UPDATE nodes SET status=$1, updated_at=now() WHERE id=$2", newStatus, node.ID)
			if err != nil {
				slog.Error("update node health status", "error", err, "node", node.Name)
			}
			m.logEvent(ctx, "node.health", node.Name, fmt.Sprintf("Status changed: %s → %s", node.Status, newStatus), nil)
		}

		// Fetch node ID if we don't have it yet and the node is healthy.
		if healthy && node.NodeID == "" {
			m.fetchAndStoreNodeID(ctx, node)
		}
	}
}

func (m *Manager) checkNodeHealth(ctx context.Context, node Node) bool {
	containerName := "avax-" + node.Name
	url := fmt.Sprintf("http://%s:9650/ext/health", containerName)

	body := `{"jsonrpc":"2.0","id":1,"method":"health.health"}`
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result struct {
		Result struct {
			Healthy bool `json:"healthy"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	return result.Result.Healthy
}

func (m *Manager) fetchAndStoreNodeID(ctx context.Context, node Node) {
	containerName := "avax-" + node.Name
	url := fmt.Sprintf("http://%s:9650/ext/info", containerName)

	body := `{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}`
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			NodeID string `json:"nodeID"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}
	if result.Result.NodeID == "" {
		return
	}

	_, err = m.pool.Exec(ctx, "UPDATE nodes SET node_id=$1, updated_at=now() WHERE id=$2", result.Result.NodeID, node.ID)
	if err != nil {
		slog.Error("store node_id", "error", err, "node", node.Name)
		return
	}
	slog.Info("discovered node ID", "node", node.Name, "node_id", result.Result.NodeID)
	m.logEvent(ctx, "node.identified", node.Name, "Node ID: "+result.Result.NodeID, nil)
}

// reconcile syncs DB node statuses with actual Docker container states.
func (m *Manager) reconcile(ctx context.Context) error {
	slog.Info("running startup reconciliation")

	containers, err := m.dc.ListManagedContainers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	// Build lookup by container name.
	containerState := make(map[string]string) // name -> state
	for _, c := range containers {
		containerState[c.Name] = c.State
	}

	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	for _, node := range nodes {
		if node.ContainerID == "" {
			continue
		}
		containerName := "avax-" + node.Name
		state, found := containerState[containerName]

		var newStatus string
		if !found {
			// Container gone — mark as stopped.
			newStatus = "stopped"
		} else {
			switch state {
			case "running":
				newStatus = "running"
			case "exited", "dead":
				newStatus = "stopped"
			case "created", "restarting":
				newStatus = "creating"
			default:
				newStatus = "stopped"
			}
		}

		if newStatus != node.Status {
			slog.Info("reconcile", "node", node.Name, "old_status", node.Status, "new_status", newStatus)
			_, err := m.pool.Exec(ctx, "UPDATE nodes SET status=$1, updated_at=now() WHERE id=$2", newStatus, node.ID)
			if err != nil {
				slog.Error("reconcile update", "error", err, "node", node.Name)
			}
		}
	}

	return nil
}

// StatusSummary holds summary data for the dashboard.
type StatusSummary struct {
	Version string         `json:"version"`
	Counts  map[string]int64 `json:"counts"`
	Nodes   []NodeSummary  `json:"nodes,omitempty"`
}

// L1Summary is a brief L1 representation for node cards.
type L1Summary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	SubnetID string `json:"subnet_id"`
	VM       string `json:"vm"`
	Status   string `json:"status"`
}

// NodeSummary is a brief node representation for the dashboard.
type NodeSummary struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	HostName    string      `json:"host_name"`
	Image       string      `json:"image"`
	NodeID      string      `json:"node_id,omitempty"`
	StakingPort int         `json:"staking_port"`
	Status      string      `json:"status"`
	L1s         []L1Summary `json:"l1s"`
}

// LocalHostLabel returns the resolved Docker host display name.
func (m *Manager) LocalHostLabel() string {
	return m.localHostLabel
}

// ListL1sForNode returns L1s validated by the given node.
func (m *Manager) ListL1sForNode(ctx context.Context, nodeID int64) ([]L1Summary, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT l.id, l.name, l.subnet_id, l.vm, l.status
		FROM l1_validators v
		JOIN l1s l ON v.l1_id = l.id
		WHERE v.node_id = $1
		ORDER BY l.name`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var l1s []L1Summary
	for rows.Next() {
		var s L1Summary
		if err := rows.Scan(&s.ID, &s.Name, &s.SubnetID, &s.VM, &s.Status); err != nil {
			return nil, err
		}
		l1s = append(l1s, s)
	}
	if l1s == nil {
		l1s = []L1Summary{}
	}
	return l1s, rows.Err()
}

func (m *Manager) logEvent(ctx context.Context, eventType, target, message string, details map[string]any) {
	detailJSON := []byte("{}")
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailJSON = b
		}
	}
	_, err := m.pool.Exec(ctx, `
		INSERT INTO events (event_type, target, message, details)
		VALUES ($1, $2, $3, $4)`,
		eventType, target, message, detailJSON)
	if err != nil {
		slog.Error("log event", "error", err, "type", eventType, "target", target)
	}
}
