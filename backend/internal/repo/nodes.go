// Package repo 封装数据库查询。
package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/coxpanel/backend/internal/models"
)

// NodeRepo 节点数据访问。
type NodeRepo struct{ db *sql.DB }

func NewNodeRepo(db *sql.DB) *NodeRepo { return &NodeRepo{db: db} }

// scanNode 统一扫描节点行。
func scanNode(sc interface{ Scan(...any) error }) (*models.Node, error) {
	var n models.Node
	var extParams []byte
	var publicIP, easyIP, sshHost, sshUser, coreVersion, extProtocol sql.NullString
	var sshPort64 int64
	var lastSeenT sql.NullTime
	err := sc.Scan(&n.ID, &n.Name, &n.Type, &publicIP, &easyIP, &sshHost, &sshUser, &sshPort64, &coreVersion, &n.Status, &lastSeenT, &extProtocol, &extParams, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	n.PublicIP = publicIP.String
	n.EasyIP = easyIP.String
	n.SSHHost = sshHost.String
	n.SSHUser = sshUser.String
	n.CoreVersion = coreVersion.String
	n.ExtProtocol = extProtocol.String
	n.SSHPort = int(sshPort64)
	if lastSeenT.Valid {
		n.LastSeenAt = &lastSeenT.Time
	}
	if len(extParams) > 0 {
		n.ExtParams = json.RawMessage(extParams)
	}
	return &n, nil
}

// Create 创建节点。
func (r *NodeRepo) Create(ctx context.Context, n *models.Node) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO nodes (name, type, public_ip, easy_ip, ssh_host, ssh_user, ssh_port, ext_protocol, ext_params)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		n.Name, n.Type, nullStr(n.PublicIP), nullStr(n.EasyIP), nullStr(n.SSHHost),
		nullStr(n.SSHUser), n.SSHPort, nullStr(n.ExtProtocol), nullJSON(n.ExtParams)).Scan(&id)
	return id, err
}

// List 列出全部节点。
func (r *NodeRepo) List(ctx context.Context) ([]models.Node, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, type, public_ip, easy_ip, ssh_host, ssh_user, ssh_port, core_version, status, last_seen_at, ext_protocol, ext_params, created_at, updated_at
		FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// Get 取单个节点。
func (r *NodeRepo) Get(ctx context.Context, id int64) (*models.Node, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, type, public_ip, easy_ip, ssh_host, ssh_user, ssh_port, core_version, status, last_seen_at, ext_protocol, ext_params, created_at, updated_at
		FROM nodes WHERE id=$1`, id)
	return scanNode(row)
}

// Update 更新节点。
func (r *NodeRepo) Update(ctx context.Context, id int64, n *models.Node) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE nodes SET name=$2, public_ip=$3, easy_ip=$4, ssh_host=$5, ssh_user=$6, ssh_port=$7, ext_protocol=$8, ext_params=$9, updated_at=now()
		WHERE id=$1`,
		id, n.Name, nullStr(n.PublicIP), nullStr(n.EasyIP), nullStr(n.SSHHost), nullStr(n.SSHUser), n.SSHPort, nullStr(n.ExtProtocol), nullJSON(n.ExtParams))
	return err
}

// Delete 删除节点。
func (r *NodeRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=$1`, id)
	return err
}

// UpdateStatus 更新节点状态（心跳时用）。
func (r *NodeRepo) UpdateStatus(ctx context.Context, id int64, status, coreVersion string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET status=$2, core_version=$3, last_seen_at=now() WHERE id=$1`, id, status, coreVersion)
	return err
}

// ---- 入站 ----

// CreateInbound 创建入站。
func (r *NodeRepo) CreateInbound(ctx context.Context, ib *models.Inbound) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO inbounds (node_id, name, protocol, role, listen_addr, listen_port, config, min_client_ver)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		ib.NodeID, ib.Name, ib.Protocol, ib.Role, ib.ListenAddr, ib.ListenPort, nullJSON(ib.Config), ib.MinClientVer).Scan(&id)
	return id, err
}

// ListInbounds 列出入站。
func (r *NodeRepo) ListInbounds(ctx context.Context, nodeID int64) ([]models.Inbound, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, node_id, name, protocol, role, listen_addr, listen_port, config, min_client_ver, created_at, updated_at FROM inbounds WHERE node_id=$1 ORDER BY id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Inbound
	for rows.Next() {
		var ib models.Inbound
		var cfg []byte
		if err := rows.Scan(&ib.ID, &ib.NodeID, &ib.Name, &ib.Protocol, &ib.Role, &ib.ListenAddr, &ib.ListenPort, &cfg, &ib.MinClientVer, &ib.CreatedAt, &ib.UpdatedAt); err != nil {
			return nil, err
		}
		if len(cfg) > 0 {
			ib.Config = json.RawMessage(cfg)
		}
		out = append(out, ib)
	}
	return out, rows.Err()
}

// DeleteInbound 删除入站。
func (r *NodeRepo) DeleteInbound(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM inbounds WHERE id=$1`, id)
	return err
}

// ---- 工具 ----

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return []byte(b)
}

// Now 便捷取当前时间（保留 time 引用）。
var Now = time.Now
