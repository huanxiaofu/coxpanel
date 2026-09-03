package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/coxpanel/backend/internal/models"
)

// UserRepo 用户与邀请码数据访问。
type UserRepo struct{ db *sql.DB }

func NewUserRepo(db *sql.DB) *UserRepo { return &UserRepo{db: db} }

// CreateUser 创建用户。
func (r *UserRepo) CreateUser(ctx context.Context, username, hash, email, role string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, email, role) VALUES ($1,$2,$3,$4) RETURNING id`,
		username, hash, nullStr(email), role).Scan(&id)
	return id, err
}

// GetByUsername 按用户名取用户。
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var u models.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, email, role, traffic_limit_bytes, expire_at, created_at
		FROM users WHERE username=$1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.Role, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUser 按 ID 取用户。
func (r *UserRepo) GetUser(ctx context.Context, id int64) (*models.User, error) {
	var u models.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, email, role, traffic_limit_bytes, expire_at, created_at
		FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.Role, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers 列出用户。
func (r *UserRepo) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, username, password_hash, email, role, traffic_limit_bytes, expire_at, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.Role, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---- 邀请码 ----

// InviteCode 邀请码记录。
type InviteCode struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	MaxUses     int        `json:"maxUses"`
	UsedCount   int        `json:"usedCount"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	NodeGroupID *int64     `json:"nodeGroupId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// CreateInvite 创建邀请码。
func (r *UserRepo) CreateInvite(ctx context.Context, code string, maxUses int, expiresAt *time.Time, nodeGroupID *int64, createdBy int64) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO invite_codes (code, max_uses, expires_at, node_group_id, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		code, maxUses, expiresAt, nodeGroupID, createdBy).Scan(&id)
	return id, err
}

// UseInvite 消费邀请码（原子：计数 +1，超出/过期返回错误）。
func (r *UserRepo) UseInvite(ctx context.Context, code string) (*InviteCode, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var ic InviteCode
	err = tx.QueryRowContext(ctx, `SELECT id, code, max_uses, used_count, expires_at, node_group_id, created_at FROM invite_codes WHERE code=$1 FOR UPDATE`, code).
		Scan(&ic.ID, &ic.Code, &ic.MaxUses, &ic.UsedCount, &ic.ExpiresAt, &ic.NodeGroupID, &ic.CreatedAt)
	if err != nil {
		return nil, err
	}
	if ic.ExpiresAt != nil && time.Now().After(*ic.ExpiresAt) {
		return nil, sql.ErrNoRows
	}
	if ic.UsedCount >= ic.MaxUses {
		return nil, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invite_codes SET used_count = used_count + 1 WHERE id=$1`, ic.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	ic.UsedCount++
	return &ic, nil
}

// ListInvites 列出邀请码。
func (r *UserRepo) ListInvites(ctx context.Context) ([]InviteCode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, code, max_uses, used_count, expires_at, node_group_id, created_at FROM invite_codes ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InviteCode
	for rows.Next() {
		var ic InviteCode
		if err := rows.Scan(&ic.ID, &ic.Code, &ic.MaxUses, &ic.UsedCount, &ic.ExpiresAt, &ic.NodeGroupID, &ic.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ic)
	}
	return out, rows.Err()
}

// ---- 订阅 ----

// SubscriptionRepo 订阅数据访问。
type SubscriptionRepo struct{ db *sql.DB }

func NewSubscriptionRepo(db *sql.DB) *SubscriptionRepo { return &SubscriptionRepo{db: db} }

// Create 创建订阅。
func (r *SubscriptionRepo) Create(ctx context.Context, s *models.Subscription) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO subscriptions (user_id, name, token, format, node_group_id, template_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		s.UserID, s.Name, s.Token, s.Format, s.NodeGroupID, s.TemplateID).Scan(&id)
	return id, err
}

// GetByToken 按 token 取订阅。
func (r *SubscriptionRepo) GetByToken(ctx context.Context, token string) (*models.Subscription, error) {
	var s models.Subscription
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, token, format, node_group_id, template_id, created_at, updated_at
		FROM subscriptions WHERE token=$1`, token).
		Scan(&s.ID, &s.UserID, &s.Name, &s.Token, &s.Format, &s.NodeGroupID, &s.TemplateID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetByID 按 ID 取订阅。
func (r *SubscriptionRepo) GetByID(ctx context.Context, id int64) (*models.Subscription, error) {
	var s models.Subscription
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, token, format, node_group_id, template_id, created_at, updated_at
		FROM subscriptions WHERE id=$1`, id).
		Scan(&s.ID, &s.UserID, &s.Name, &s.Token, &s.Format, &s.NodeGroupID, &s.TemplateID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListByUser 列用户订阅。
func (r *SubscriptionRepo) ListByUser(ctx context.Context, userID int64) ([]models.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, token, format, node_group_id, template_id, created_at, updated_at
		FROM subscriptions WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Subscription
	for rows.Next() {
		var s models.Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.Token, &s.Format, &s.NodeGroupID, &s.TemplateID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete 删除订阅。
func (r *SubscriptionRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=$1`, id)
	return err
}

// OverrideRow 覆写记录。
type OverrideRow struct {
	NodeID      int64
	DisplayName string
	SortOrder   int
	Icon        string
	Params      json.RawMessage
	ProxyGroup  string
}

// ListOverrides 取订阅的覆写（map[节点ID]覆写）。
func (r *SubscriptionRepo) ListOverrides(ctx context.Context, subID int64) (map[int64]OverrideRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT node_id, COALESCE(display_name,''), sort_order, COALESCE(icon,''), params, COALESCE(proxy_group,'')
		FROM subscription_node_overrides WHERE subscription_id=$1`, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]OverrideRow{}
	for rows.Next() {
		var o OverrideRow
		var params []byte
		if err := rows.Scan(&o.NodeID, &o.DisplayName, &o.SortOrder, &o.Icon, &params, &o.ProxyGroup); err != nil {
			return nil, err
		}
		o.Params = json.RawMessage(params)
		out[o.NodeID] = o
	}
	return out, rows.Err()
}

// SaveOverride 写入/更新单节点覆写。
func (r *SubscriptionRepo) SaveOverride(ctx context.Context, subID, nodeID int64, displayName string, sortOrder int, icon, proxyGroup string, params json.RawMessage) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO subscription_node_overrides (subscription_id, node_id, display_name, sort_order, icon, params, proxy_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (subscription_id, node_id) DO UPDATE
		SET display_name=EXCLUDED.display_name, sort_order=EXCLUDED.sort_order,
		    icon=EXCLUDED.icon, params=EXCLUDED.params, proxy_group=EXCLUDED.proxy_group,
		    updated_at=now()`,
		subID, nodeID, nullStr(displayName), sortOrder, nullStr(icon), nullJSON(params), nullStr(proxyGroup))
	return err
}

// ---- 节点组 ----

// GroupRepo 节点组数据访问。
type GroupRepo struct{ db *sql.DB }

func NewGroupRepo(db *sql.DB) *GroupRepo { return &GroupRepo{db: db} }

// NodeGroup 节点组。
type NodeGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// List 列出节点组。
func (r *GroupRepo) List(ctx context.Context) ([]NodeGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name FROM node_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeGroup
	for rows.Next() {
		var g NodeGroup
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Create 创建节点组。
func (r *GroupRepo) Create(ctx context.Context, name string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO node_groups (name) VALUES ($1) RETURNING id`, name).Scan(&id)
	return id, err
}

// NodeIDs 取组的节点 ID 列表。
func (r *GroupRepo) NodeIDs(ctx context.Context, groupID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT node_id FROM node_group_members WHERE group_id=$1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AddNode 组加节点。
func (r *GroupRepo) AddNode(ctx context.Context, groupID, nodeID int64) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO node_group_members (group_id, node_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, groupID, nodeID)
	return err
}

// RemoveNode 组删节点。
func (r *GroupRepo) RemoveNode(ctx context.Context, groupID, nodeID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM node_group_members WHERE group_id=$1 AND node_id=$2`, groupID, nodeID)
	return err
}

// NodeIDByGroup 别名（保持 json 引用）。
var _ = json.Valid
