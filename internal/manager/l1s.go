package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/primal-host/avalauncher/internal/docker"
)

// L1 represents an L1 row from the database.
type L1 struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	SubnetID     string    `json:"subnet_id"`
	BlockchainID string    `json:"blockchain_id"`
	VM           string    `json:"vm"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// L1Detail includes the L1 plus its validators.
type L1Detail struct {
	L1
	Validators []L1Validator `json:"validators"`
}

// L1WithCount includes the L1 plus a validator count.
type L1WithCount struct {
	L1
	ValidatorCount int `json:"validator_count"`
}

// L1Validator represents a validator assignment row.
type L1Validator struct {
	ID       int64  `json:"id"`
	NodeID   int64  `json:"node_id"`
	NodeName string `json:"node_name"`
	Weight   int64  `json:"weight"`
	TxID     string `json:"tx_id"`
}

// L1DashboardItem is the L1 representation for the dashboard status endpoint.
type L1DashboardItem struct {
	L1
	Validators []L1Validator `json:"validators"`
}

// CreateL1Request holds parameters for creating an L1.
type CreateL1Request struct {
	Name         string `json:"name"`
	VM           string `json:"vm"`
	SubnetID     string `json:"subnet_id"`
	BlockchainID string `json:"blockchain_id"`
}

// AddValidatorRequest holds parameters for adding a validator to an L1.
type AddValidatorRequest struct {
	NodeID int64 `json:"node_id"`
	Weight int64 `json:"weight"`
}

// CreateL1 creates a new L1 record.
func (m *Manager) CreateL1(ctx context.Context, req CreateL1Request) (*L1, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.VM == "" {
		req.VM = "subnet-evm"
	}

	// Check name uniqueness.
	var exists bool
	if err := m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM l1s WHERE name=$1)", req.Name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check name: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("L1 %q already exists", req.Name)
	}

	status := "pending"
	if req.SubnetID != "" {
		status = "configured"
	}

	var l1 L1
	err := m.pool.QueryRow(ctx, `
		INSERT INTO l1s (name, vm, subnet_id, blockchain_id, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, subnet_id, blockchain_id, vm, status, created_at, updated_at`,
		req.Name, req.VM, req.SubnetID, req.BlockchainID, status,
	).Scan(&l1.ID, &l1.Name, &l1.SubnetID, &l1.BlockchainID, &l1.VM, &l1.Status, &l1.CreatedAt, &l1.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert L1: %w", err)
	}

	m.logEvent(ctx, "l1.created", l1.Name, fmt.Sprintf("L1 created (vm=%s, status=%s)", l1.VM, l1.Status), nil)
	return &l1, nil
}

// ListL1s returns all L1s with validator counts.
func (m *Manager) ListL1s(ctx context.Context) ([]L1WithCount, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT l.id, l.name, l.subnet_id, l.blockchain_id, l.vm, l.status,
		       l.created_at, l.updated_at, COUNT(v.id)::int AS validator_count
		FROM l1s l
		LEFT JOIN l1_validators v ON v.l1_id = l.id
		GROUP BY l.id
		ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var l1s []L1WithCount
	for rows.Next() {
		var l L1WithCount
		if err := rows.Scan(&l.ID, &l.Name, &l.SubnetID, &l.BlockchainID, &l.VM, &l.Status,
			&l.CreatedAt, &l.UpdatedAt, &l.ValidatorCount); err != nil {
			return nil, err
		}
		l1s = append(l1s, l)
	}
	if l1s == nil {
		l1s = []L1WithCount{}
	}
	return l1s, rows.Err()
}

// GetL1 returns an L1 with its validators.
func (m *Manager) GetL1(ctx context.Context, id int64) (*L1Detail, error) {
	var d L1Detail
	err := m.pool.QueryRow(ctx, `
		SELECT id, name, subnet_id, blockchain_id, vm, status, created_at, updated_at
		FROM l1s WHERE id=$1`, id).
		Scan(&d.ID, &d.Name, &d.SubnetID, &d.BlockchainID, &d.VM, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := m.pool.Query(ctx, `
		SELECT v.id, v.node_id, n.name, v.weight, v.tx_id
		FROM l1_validators v
		JOIN nodes n ON v.node_id = n.id
		WHERE v.l1_id = $1
		ORDER BY v.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var v L1Validator
		if err := rows.Scan(&v.ID, &v.NodeID, &v.NodeName, &v.Weight, &v.TxID); err != nil {
			return nil, err
		}
		d.Validators = append(d.Validators, v)
	}
	if d.Validators == nil {
		d.Validators = []L1Validator{}
	}
	return &d, rows.Err()
}

// DeleteL1 removes an L1 if it has no validators.
func (m *Manager) DeleteL1(ctx context.Context, id int64) error {
	var name string
	if err := m.pool.QueryRow(ctx, "SELECT name FROM l1s WHERE id=$1", id).Scan(&name); err != nil {
		return fmt.Errorf("L1 not found")
	}

	var count int64
	if err := m.pool.QueryRow(ctx, "SELECT count(*) FROM l1_validators WHERE l1_id=$1", id).Scan(&count); err != nil {
		return fmt.Errorf("check validators: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("L1 has %d validator(s) — remove them first", count)
	}

	if _, err := m.pool.Exec(ctx, "DELETE FROM l1s WHERE id=$1", id); err != nil {
		return fmt.Errorf("delete L1: %w", err)
	}

	m.logEvent(ctx, "l1.deleted", name, "L1 deleted", nil)
	return nil
}

// AddValidator assigns a node as a validator for an L1.
func (m *Manager) AddValidator(ctx context.Context, l1ID int64, req AddValidatorRequest) (*L1Validator, error) {
	if req.Weight <= 0 {
		req.Weight = 100
	}

	// Verify L1 exists.
	var l1Name, subnetID string
	if err := m.pool.QueryRow(ctx, "SELECT name, subnet_id FROM l1s WHERE id=$1", l1ID).Scan(&l1Name, &subnetID); err != nil {
		return nil, fmt.Errorf("L1 not found")
	}

	// Verify node exists.
	var nodeName string
	var hostID int64
	if err := m.pool.QueryRow(ctx, "SELECT name, host_id FROM nodes WHERE id=$1", req.NodeID).Scan(&nodeName, &hostID); err != nil {
		return nil, fmt.Errorf("node not found")
	}

	// Check for duplicate assignment.
	var exists bool
	if err := m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM l1_validators WHERE l1_id=$1 AND node_id=$2)", l1ID, req.NodeID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check duplicate: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("node %q is already a validator for L1 %q", nodeName, l1Name)
	}

	var v L1Validator
	err := m.pool.QueryRow(ctx, `
		INSERT INTO l1_validators (l1_id, node_id, weight)
		VALUES ($1, $2, $3)
		RETURNING id, node_id, weight, tx_id`,
		l1ID, req.NodeID, req.Weight,
	).Scan(&v.ID, &v.NodeID, &v.Weight, &v.TxID)
	if err != nil {
		return nil, fmt.Errorf("insert validator: %w", err)
	}
	v.NodeName = nodeName

	m.logEvent(ctx, "l1.validator.added", l1Name, fmt.Sprintf("Validator added: node %s (weight %d)", nodeName, req.Weight), nil)

	// Reconfigure node container if L1 has a subnet_id.
	if subnetID != "" {
		go m.reconfigureNode(req.NodeID)
	}

	return &v, nil
}

// RemoveValidator removes a node's validator assignment from an L1.
func (m *Manager) RemoveValidator(ctx context.Context, l1ID, nodeID int64) error {
	var l1Name, subnetID string
	if err := m.pool.QueryRow(ctx, "SELECT name, subnet_id FROM l1s WHERE id=$1", l1ID).Scan(&l1Name, &subnetID); err != nil {
		return fmt.Errorf("L1 not found")
	}

	tag, err := m.pool.Exec(ctx, "DELETE FROM l1_validators WHERE l1_id=$1 AND node_id=$2", l1ID, nodeID)
	if err != nil {
		return fmt.Errorf("delete validator: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("validator assignment not found")
	}

	m.logEvent(ctx, "l1.validator.removed", l1Name, "Validator removed", nil)

	// Reconfigure node container if L1 has a subnet_id.
	if subnetID != "" {
		go m.reconfigureNode(nodeID)
	}

	return nil
}

// ListValidators returns all validators for an L1.
func (m *Manager) ListValidators(ctx context.Context, l1ID int64) ([]L1Validator, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT v.id, v.node_id, n.name, v.weight, v.tx_id
		FROM l1_validators v
		JOIN nodes n ON v.node_id = n.id
		WHERE v.l1_id = $1
		ORDER BY v.id`, l1ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vals []L1Validator
	for rows.Next() {
		var v L1Validator
		if err := rows.Scan(&v.ID, &v.NodeID, &v.NodeName, &v.Weight, &v.TxID); err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	if vals == nil {
		vals = []L1Validator{}
	}
	return vals, rows.Err()
}

// ListL1sForDashboard returns all L1s with their validators for the dashboard.
func (m *Manager) ListL1sForDashboard(ctx context.Context) ([]L1DashboardItem, error) {
	// Fetch all L1s.
	rows, err := m.pool.Query(ctx, `
		SELECT id, name, subnet_id, blockchain_id, vm, status, created_at, updated_at
		FROM l1s ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []L1DashboardItem
	idxMap := make(map[int64]int) // l1_id -> index in items
	for rows.Next() {
		var item L1DashboardItem
		if err := rows.Scan(&item.ID, &item.Name, &item.SubnetID, &item.BlockchainID,
			&item.VM, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Validators = []L1Validator{}
		idxMap[item.ID] = len(items)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return items, nil
	}

	// Fetch all validators.
	vrows, err := m.pool.Query(ctx, `
		SELECT v.id, v.l1_id, v.node_id, n.name, v.weight, v.tx_id
		FROM l1_validators v
		JOIN nodes n ON v.node_id = n.id
		ORDER BY v.id`)
	if err != nil {
		return nil, err
	}
	defer vrows.Close()

	for vrows.Next() {
		var v L1Validator
		var l1ID int64
		if err := vrows.Scan(&v.ID, &l1ID, &v.NodeID, &v.NodeName, &v.Weight, &v.TxID); err != nil {
			return nil, err
		}
		if idx, ok := idxMap[l1ID]; ok {
			items[idx].Validators = append(items[idx].Validators, v)
		}
	}

	return items, vrows.Err()
}

// subnetIDsForNode returns all distinct subnet_ids from L1s that this node validates.
func (m *Manager) subnetIDsForNode(ctx context.Context, nodeID int64) ([]string, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT DISTINCT l.subnet_id
		FROM l1_validators v
		JOIN l1s l ON v.l1_id = l.id
		WHERE v.node_id = $1 AND l.subnet_id != ''`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// reconfigureNode recreates a node's container with updated TrackSubnets.
func (m *Manager) reconfigureNode(nodeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	node, err := m.GetNode(ctx, nodeID)
	if err != nil {
		slog.Error("reconfigure: get node", "error", err, "node_id", nodeID)
		return
	}

	dc := m.clientFor(node.HostID)
	if dc == nil {
		slog.Error("reconfigure: no client for host", "host_id", node.HostID, "node", node.Name)
		return
	}

	subnetIDs, err := m.subnetIDsForNode(ctx, nodeID)
	if err != nil {
		slog.Error("reconfigure: get subnet ids", "error", err, "node", node.Name)
		return
	}

	m.logEvent(ctx, "node.reconfiguring", node.Name,
		fmt.Sprintf("Reconfiguring with subnets: %s", strings.Join(subnetIDs, ",")), nil)

	// Set status to creating (shows yellow pulse in dashboard).
	m.pool.Exec(ctx, "UPDATE nodes SET status='creating', updated_at=now() WHERE id=$1", nodeID)

	setFailed := func(msg string) {
		m.pool.Exec(ctx, "UPDATE nodes SET status='failed', updated_at=now() WHERE id=$1", nodeID)
		m.logEvent(ctx, "node.failed", node.Name, msg, nil)
	}

	// Stop container if running.
	if node.ContainerID != "" {
		_ = dc.ContainerStop(ctx, node.ContainerID, 30)
		if err := dc.ContainerRemove(ctx, node.ContainerID, false); err != nil {
			if !strings.Contains(err.Error(), "No such container") {
				slog.Error("reconfigure: remove container", "error", err, "node", node.Name)
				setFailed(fmt.Sprintf("Container remove failed: %v", err))
				return
			}
		}
	}

	// Build new container config with TrackSubnets.
	params := &docker.AvagoParams{
		Name:         node.Name,
		Image:        node.Image,
		NetworkName:  m.avaxDockerNet,
		NetworkID:    m.avagoNetwork,
		StakingPort:  node.StakingPort,
		TrackSubnets: subnetIDs,
	}
	cc, hc, nc := params.BuildContainerConfig()

	// Create container.
	containerName := params.ContainerName()
	containerID, err := dc.ContainerCreate(ctx, containerName, cc, hc, nc)
	if err != nil {
		slog.Error("reconfigure: create container", "error", err, "node", node.Name)
		setFailed(fmt.Sprintf("Container create failed: %v", err))
		return
	}

	// Update container_id.
	m.pool.Exec(ctx, "UPDATE nodes SET container_id=$1, updated_at=now() WHERE id=$2", containerID, nodeID)

	// Start container.
	if err := dc.ContainerStart(ctx, containerID); err != nil {
		slog.Error("reconfigure: start container", "error", err, "node", node.Name)
		setFailed(fmt.Sprintf("Container start failed: %v", err))
		return
	}

	m.pool.Exec(ctx, "UPDATE nodes SET status='running', updated_at=now() WHERE id=$1", nodeID)
	m.logEvent(ctx, "node.reconfigured", node.Name,
		fmt.Sprintf("Node reconfigured with %d subnet(s)", len(subnetIDs)), nil)
	slog.Info("node reconfigured", "node", node.Name, "subnets", subnetIDs, "container", containerID[:12])
}
