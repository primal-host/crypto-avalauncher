package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/primal-host/avalauncher/internal/docker"
)

// Host represents a host row from the database.
type Host struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	SSHAddr   string         `json:"ssh_addr"`
	Labels    map[string]any `json:"labels"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// AddHostRequest holds parameters for adding a remote host.
type AddHostRequest struct {
	Name    string `json:"name"`
	SSHAddr string `json:"ssh_addr"`
}

// AddHost validates the SSH connection, gathers host info, and inserts a row.
func (m *Manager) AddHost(ctx context.Context, req AddHostRequest) (*Host, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.SSHAddr == "" {
		return nil, fmt.Errorf("ssh_addr is required")
	}

	// Check name uniqueness.
	var exists bool
	if err := m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM hosts WHERE name=$1)", req.Name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check name: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("host %q already exists", req.Name)
	}

	// Connect via SSH.
	dc, err := docker.NewSSH(req.SSHAddr)
	if err != nil {
		return nil, fmt.Errorf("ssh connect: %w", err)
	}

	// Verify connectivity.
	if err := dc.Ping(ctx); err != nil {
		dc.Close()
		return nil, fmt.Errorf("docker ping: %w", err)
	}

	// Gather host info.
	info, err := dc.HostInfo(ctx)
	if err != nil {
		dc.Close()
		return nil, fmt.Errorf("host info: %w", err)
	}

	// Ensure the avax Docker network exists on the remote host.
	if err := dc.EnsureNetwork(ctx, m.avaxDockerNet); err != nil {
		dc.Close()
		return nil, fmt.Errorf("ensure network: %w", err)
	}

	// Build labels JSONB.
	labels := map[string]any{
		"hostname":       info.Hostname,
		"os":             info.OS,
		"arch":           info.Architecture,
		"cpus":           info.CPUs,
		"memory_mb":      info.MemoryMB,
		"docker_version": info.DockerVersion,
	}
	labelsJSON, _ := json.Marshal(labels)

	// Insert host row.
	var host Host
	var labelsRaw []byte
	err = m.pool.QueryRow(ctx, `
		INSERT INTO hosts (name, ssh_addr, status, labels)
		VALUES ($1, $2, 'online', $3)
		RETURNING id, name, ssh_addr, labels, status, created_at, updated_at`,
		req.Name, req.SSHAddr, labelsJSON,
	).Scan(&host.ID, &host.Name, &host.SSHAddr, &labelsRaw, &host.Status, &host.CreatedAt, &host.UpdatedAt)
	if err != nil {
		dc.Close()
		return nil, fmt.Errorf("insert host: %w", err)
	}
	json.Unmarshal(labelsRaw, &host.Labels)

	// Register the client.
	m.registerClient(host.ID, dc)

	m.logEvent(ctx, "host.added", host.Name, fmt.Sprintf("Host added: %s (%s)", info.Hostname, req.SSHAddr), labels)
	slog.Info("host added", "name", host.Name, "ssh", req.SSHAddr, "hostname", info.Hostname)

	return &host, nil
}

// RemoveHost removes a host if it has no nodes.
func (m *Manager) RemoveHost(ctx context.Context, id int64) error {
	if id == m.localHostID {
		return fmt.Errorf("cannot remove the local host")
	}

	// Check for nodes on this host.
	var count int64
	if err := m.pool.QueryRow(ctx, "SELECT count(*) FROM nodes WHERE host_id=$1", id).Scan(&count); err != nil {
		return fmt.Errorf("check nodes: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("host has %d node(s) — remove them first", count)
	}

	// Get host name for event logging.
	var name string
	if err := m.pool.QueryRow(ctx, "SELECT name FROM hosts WHERE id=$1", id).Scan(&name); err != nil {
		return fmt.Errorf("get host: %w", err)
	}

	// Close and unregister client.
	m.unregisterClient(id)

	// Delete DB row.
	_, err := m.pool.Exec(ctx, "DELETE FROM hosts WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("delete host: %w", err)
	}

	m.logEvent(ctx, "host.removed", name, "Host removed", nil)
	slog.Info("host removed", "name", name)
	return nil
}

// ListHosts returns all hosts with their labels.
func (m *Manager) ListHosts(ctx context.Context) ([]Host, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT id, name, ssh_addr, labels, status, created_at, updated_at
		FROM hosts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []Host
	for rows.Next() {
		var h Host
		var labelsRaw []byte
		if err := rows.Scan(&h.ID, &h.Name, &h.SSHAddr, &labelsRaw, &h.Status, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		if len(labelsRaw) > 0 {
			json.Unmarshal(labelsRaw, &h.Labels)
		}
		hosts = append(hosts, h)
	}
	if hosts == nil {
		hosts = []Host{}
	}
	return hosts, rows.Err()
}

// GetHost returns a single host by ID.
func (m *Manager) GetHost(ctx context.Context, id int64) (*Host, error) {
	var h Host
	var labelsRaw []byte
	err := m.pool.QueryRow(ctx, `
		SELECT id, name, ssh_addr, labels, status, created_at, updated_at
		FROM hosts WHERE id=$1`, id).
		Scan(&h.ID, &h.Name, &h.SSHAddr, &labelsRaw, &h.Status, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(labelsRaw) > 0 {
		json.Unmarshal(labelsRaw, &h.Labels)
	}
	return &h, nil
}

// HostLabelsMap returns a map of hostID -> hostname label from the DB.
func (m *Manager) HostLabelsMap(ctx context.Context) map[int64]string {
	result := make(map[int64]string)
	rows, err := m.pool.Query(ctx, "SELECT id, labels FROM hosts")
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var labelsRaw []byte
		if err := rows.Scan(&id, &labelsRaw); err != nil {
			continue
		}
		var labels map[string]any
		if json.Unmarshal(labelsRaw, &labels) == nil {
			if hostname, ok := labels["hostname"].(string); ok {
				result[id] = hostname
			}
		}
	}
	return result
}

// StartHostPoller begins a background loop that pings remote hosts.
func (m *Manager) StartHostPoller() {
	m.pollerWg.Add(1)
	go func() {
		defer m.pollerWg.Done()
		ticker := time.NewTicker(m.healthInterval * 2) // host checks at 2x node interval
		defer ticker.Stop()

		for {
			select {
			case <-m.stopPoller:
				return
			case <-ticker.C:
				m.pollHosts()
			}
		}
	}()
	slog.Info("host poller started")
}

func (m *Manager) pollHosts() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rows, err := m.pool.Query(ctx, "SELECT id, name, ssh_addr, status FROM hosts WHERE ssh_addr != ''")
	if err != nil {
		return
	}
	defer rows.Close()

	type hostRow struct {
		id      int64
		name    string
		sshAddr string
		status  string
	}
	var hosts []hostRow
	for rows.Next() {
		var h hostRow
		if err := rows.Scan(&h.id, &h.name, &h.sshAddr, &h.status); err != nil {
			continue
		}
		hosts = append(hosts, h)
	}
	rows.Close()

	for _, h := range hosts {
		dc := m.clientFor(h.id)

		if dc != nil {
			// Try ping.
			if err := dc.Ping(ctx); err == nil {
				// Host is reachable.
				if h.status != "online" {
					m.pool.Exec(ctx, "UPDATE hosts SET status='online', updated_at=now() WHERE id=$1", h.id)
					m.logEvent(ctx, "host.online", h.name, "Host reconnected", nil)
					slog.Info("host reconnected", "host", h.name)
				}
				continue
			}
		}

		// Unreachable — attempt reconnect.
		if h.status != "unreachable" {
			m.pool.Exec(ctx, "UPDATE hosts SET status='unreachable', updated_at=now() WHERE id=$1", h.id)
			m.logEvent(ctx, "host.unreachable", h.name, "Host unreachable", nil)
			slog.Warn("host unreachable", "host", h.name)
		}

		// Try to reconnect.
		m.unregisterClient(h.id)
		newDC, err := docker.NewSSH(h.sshAddr)
		if err != nil {
			continue
		}
		if err := newDC.Ping(ctx); err != nil {
			newDC.Close()
			continue
		}

		m.registerClient(h.id, newDC)
		m.pool.Exec(ctx, "UPDATE hosts SET status='online', updated_at=now() WHERE id=$1", h.id)
		m.logEvent(ctx, "host.online", h.name, "Host reconnected", nil)
		slog.Info("host reconnected", "host", h.name)
	}
}
