package repo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/coxpanel/backend/internal/models"
)

var (
	ErrInvalidInvite        = errors.New("invalid invite")
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrNodeNotAuthorized    = errors.New("node not authorized")
	ErrNodeGroupNotFound    = errors.New("node group not found")
	ErrInviteGroupRequired  = errors.New("invite group is required")
	ErrNoAuthorizedNodes    = errors.New("no authorized nodes")
	ErrUnsupportedProtocol  = errors.New("unsupported protocol")
)

// UserRepo 用户与邀请码数据访问。
type UserRepo struct{ db *sql.DB }

func NewUserRepo(db *sql.DB) *UserRepo { return &UserRepo{db: db} }

// CreateUser 创建用户。
func (r *UserRepo) CreateUser(ctx context.Context, username, hash, email, role string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, email, role, is_active) VALUES ($1,$2,$3,$4,TRUE) RETURNING id`,
		username, hash, nullStr(email), role).Scan(&id)
	return id, err
}

// GetByUsername 按用户名取用户。
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var u models.User
	var scannedEmail sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, email, role, is_active, traffic_limit_bytes, expire_at, created_at
		FROM users WHERE username=$1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &scannedEmail, &u.Role, &u.Active, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = scannedEmail.String
	return &u, nil
}

// GetUser 按 ID 取用户。
func (r *UserRepo) GetUser(ctx context.Context, id int64) (*models.User, error) {
	var u models.User
	var email sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, email, role, is_active, traffic_limit_bytes, expire_at, created_at
		FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &email, &u.Role, &u.Active, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	return &u, nil
}

// ListUsers 列出用户。
func (r *UserRepo) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, username, password_hash, email, role, is_active, traffic_limit_bytes, expire_at, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		var email sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &email, &u.Role, &u.Active, &u.TrafficLimitBytes, &u.ExpireAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Email = email.String
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUserCredential returns the owner-specific credential for one inbound.
// The context-free method is retained for the subscription handler interface;
// callers that already have a request context should use the context variant.
func (r *UserRepo) GetUserCredential(userID, inboundID int64) (string, error) {
	return r.GetUserCredentialContext(context.Background(), userID, inboundID)
}

func (r *UserRepo) GetUserCredentialContext(ctx context.Context, userID, inboundID int64) (string, error) {
	var credential string
	err := r.db.QueryRowContext(ctx, `
		SELECT uc.credential
		FROM user_credentials uc
		JOIN users u ON u.id=uc.user_id
		JOIN inbounds i ON i.id=uc.inbound_id
		WHERE uc.user_id=$1 AND uc.inbound_id=$2
		  AND u.is_active=TRUE
		  AND (u.expire_at IS NULL OR u.expire_at > now())
		  AND i.role='entry'
		  AND EXISTS (
			SELECT 1
			FROM user_node_groups ug
			JOIN node_group_members ngm ON ngm.group_id=ug.group_id
			WHERE ug.user_id=uc.user_id AND ngm.node_id=i.node_id
		  )`, userID, inboundID).Scan(&credential)
	return credential, err
}

// EnsureUserCredential issues and persists one credential when the owner
// first requests an inbound. A unique constraint makes concurrent requests
// converge on the same stored value.
func (r *UserRepo) EnsureUserCredential(ctx context.Context, userID, inboundID int64, protocol string) (string, error) {
	if userID <= 0 || inboundID <= 0 {
		return "", fmt.Errorf("%w: invalid credential owner or inbound", ErrUnsupportedProtocol)
	}
	var rawConfig []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT i.config
		FROM inbounds i
		WHERE i.id=$1 AND i.role='entry' AND i.protocol=$2
		  AND EXISTS (
			SELECT 1
			FROM user_node_groups ug
			JOIN node_group_members ngm ON ngm.group_id=ug.group_id
			WHERE ug.user_id=$3 AND ngm.node_id=i.node_id
		  )`, inboundID, protocol, userID).Scan(&rawConfig)
	if err != nil {
		return "", err
	}
	credential, err := newUserCredential(protocol, credentialMethod(rawConfig))
	if err != nil {
		return "", err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_credentials (user_id, inbound_id, credential)
		SELECT $1, $2, $3
		WHERE EXISTS (
			SELECT 1 FROM users
			WHERE id=$1 AND is_active=TRUE
			  AND (expire_at IS NULL OR expire_at > now())
		)
		AND EXISTS (
			SELECT 1
			FROM inbounds i
			WHERE i.id=$2 AND i.role='entry' AND i.protocol=$4
			  AND EXISTS (
				SELECT 1
				FROM user_node_groups ug
				JOIN node_group_members ngm ON ngm.group_id=ug.group_id
				WHERE ug.user_id=$1 AND ngm.node_id=i.node_id
			  )
		)
		ON CONFLICT (user_id, inbound_id) DO NOTHING`, userID, inboundID, credential, protocol)
	if err != nil {
		return "", err
	}
	return r.GetUserCredentialContext(ctx, userID, inboundID)
}

// ReconcileUserCredentialsForNode creates credentials for every active,
// unexpired user authorized for an entry inbound on the node.
func (r *UserRepo) ReconcileUserCredentialsForNode(ctx context.Context, nodeID int64) error {
	if nodeID <= 0 {
		return errors.New("invalid node id")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT u.id, i.id, i.protocol, i.config
		FROM users u
		JOIN user_node_groups ung ON ung.user_id=u.id
		JOIN node_group_members ngm ON ngm.group_id=ung.group_id
		JOIN inbounds i ON i.node_id=ngm.node_id
		WHERE ngm.node_id=$1
		  AND u.is_active=TRUE
		  AND (u.expire_at IS NULL OR u.expire_at > now())
		  AND i.role='entry'
		ORDER BY i.id, u.id`, nodeID)
	if err != nil {
		return err
	}
	type pendingCredential struct {
		userID    int64
		inboundID int64
		protocol  string
		config    []byte
	}
	pending := make([]pendingCredential, 0)
	for rows.Next() {
		var item pendingCredential
		if err := rows.Scan(&item.userID, &item.inboundID, &item.protocol, &item.config); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, item := range pending {
		credential, err := newUserCredential(item.protocol, credentialMethod(item.config))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_credentials (user_id, inbound_id, credential)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, inbound_id) DO NOTHING`, item.userID, item.inboundID, credential); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListUserCredentialsForNode returns current credentials for active,
// unexpired users authorized for entry inbounds on the node.
func (r *UserRepo) ListUserCredentialsForNode(ctx context.Context, nodeID int64) ([]models.UserCredential, error) {
	if err := r.ReconcileUserCredentialsForNode(ctx, nodeID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT uc.user_id, uc.inbound_id, u.username, i.protocol, uc.credential
		FROM user_credentials uc
		JOIN users u ON u.id=uc.user_id
		JOIN inbounds i ON i.id=uc.inbound_id
		WHERE i.node_id=$1
		  AND i.role='entry'
		  AND u.is_active=TRUE
		  AND (u.expire_at IS NULL OR u.expire_at > now())
		  AND EXISTS (
			SELECT 1
			FROM user_node_groups ug
			JOIN node_group_members ngm ON ngm.group_id=ug.group_id
			WHERE ug.user_id=uc.user_id AND ngm.node_id=i.node_id
		  )
		ORDER BY uc.inbound_id, uc.user_id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := make([]models.UserCredential, 0)
	for rows.Next() {
		var credential models.UserCredential
		if err := rows.Scan(&credential.UserID, &credential.InboundID, &credential.Username, &credential.Protocol, &credential.Credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

// ListUserCredentialsForInbound returns current credentials for one active,
// authorized entry inbound.
func (r *UserRepo) ListUserCredentialsForInbound(ctx context.Context, inboundID int64) ([]models.UserCredential, error) {
	var nodeID int64
	if err := r.db.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE id=$1 AND role='entry'`, inboundID).Scan(&nodeID); err != nil {
		return nil, err
	}
	if err := r.ReconcileUserCredentialsForNode(ctx, nodeID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT uc.user_id, uc.inbound_id, u.username, i.protocol, uc.credential
		FROM user_credentials uc
		JOIN users u ON u.id=uc.user_id
		JOIN inbounds i ON i.id=uc.inbound_id
		WHERE uc.inbound_id=$1
		  AND i.role='entry'
		  AND u.is_active=TRUE
		  AND (u.expire_at IS NULL OR u.expire_at > now())
		  AND EXISTS (
			SELECT 1
			FROM user_node_groups ug
			JOIN node_group_members ngm ON ngm.group_id=ug.group_id
			WHERE ug.user_id=uc.user_id AND ngm.node_id=i.node_id
		  )
		ORDER BY uc.user_id`, inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := make([]models.UserCredential, 0)
	for rows.Next() {
		var credential models.UserCredential
		if err := rows.Scan(&credential.UserID, &credential.InboundID, &credential.Username, &credential.Protocol, &credential.Credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

func newUserCredential(protocol string, methods ...string) (string, error) {
	if protocol != "vless-reality" && protocol != "shadowsocks" && protocol != "hysteria2" {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedProtocol, protocol)
	}
	credentialBytes := 32
	if protocol == "shadowsocks" {
		method := ""
		if len(methods) > 0 {
			method = methods[0]
		}
		if method == "" {
			method = "2022-blake3-aes-128-gcm"
		}
		switch method {
		case "2022-blake3-aes-128-gcm":
			credentialBytes = 16
		case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
			credentialBytes = 32
		default:
			return "", fmt.Errorf("%w: unsupported SS2022 method %s", ErrUnsupportedProtocol, method)
		}
	}
	bytes := make([]byte, credentialBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate user credential: %w", err)
	}
	if protocol == "vless-reality" {
		bytes[6] = (bytes[6] & 0x0f) | 0x40
		bytes[8] = (bytes[8] & 0x3f) | 0x80
		return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(bytes[0:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]), hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:16])), nil
	}
	if protocol == "shadowsocks" {
		return base64.StdEncoding.EncodeToString(bytes), nil
	}
	return hex.EncodeToString(bytes), nil
}

func credentialMethod(rawConfig []byte) string {
	if len(rawConfig) == 0 {
		return ""
	}
	var params struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(rawConfig, &params) != nil {
		return ""
	}
	return params.Method
}

func (r *UserRepo) RegisterWithInvite(ctx context.Context, username, hash, email, code string) (*models.User, *InviteCode, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var invite InviteCode
	err = tx.QueryRowContext(ctx, `
		SELECT id, code, max_uses, used_count, expires_at, node_group_id, created_at
		FROM invite_codes WHERE code=$1 FOR UPDATE`, code).
		Scan(&invite.ID, &invite.Code, &invite.MaxUses, &invite.UsedCount, &invite.ExpiresAt, &invite.NodeGroupID, &invite.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, nil, err
	}
	if invite.ExpiresAt != nil && !time.Now().Before(*invite.ExpiresAt) {
		return nil, nil, ErrInvalidInvite
	}
	if invite.UsedCount >= invite.MaxUses {
		return nil, nil, ErrInvalidInvite
	}
	if invite.NodeGroupID == nil || *invite.NodeGroupID <= 0 {
		return nil, nil, ErrInvalidInvite
	}

	var user models.User
	var scannedEmail sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, email, role, is_active)
		VALUES ($1,$2,$3,'user',TRUE)
		RETURNING id, username, password_hash, email, role, is_active, traffic_limit_bytes, expire_at, created_at`,
		username, hash, nullStr(email)).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &scannedEmail, &user.Role, &user.Active,
		&user.TrafficLimitBytes, &user.ExpireAt, &user.CreatedAt)
	if err != nil {
		return nil, nil, err
	}
	user.Email = scannedEmail.String
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_node_groups (user_id, group_id) VALUES ($1,$2)`, user.ID, *invite.NodeGroupID); err != nil {
		return nil, nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE invite_codes SET used_count=used_count+1 WHERE id=$1 AND used_count < max_uses`, invite.ID)
	if err != nil {
		return nil, nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrInvalidInvite
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	invite.UsedCount++
	user.PasswordHash = ""
	return &user, &invite, nil
}

func (r *UserRepo) BootstrapOwner(ctx context.Context, username, hash, email string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('coxpanel.owner.bootstrap'))`); err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, email, role, is_active)
		VALUES ($1,$2,$3,'owner',TRUE)`, username, hash, nullStr(email)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
	if nodeGroupID == nil || *nodeGroupID <= 0 {
		return 0, ErrInviteGroupRequired
	}
	if maxUses <= 0 || code == "" || createdBy <= 0 {
		return 0, ErrInvalidInvite
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_groups WHERE id=$1)`, *nodeGroupID).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, sql.ErrNoRows
	}
	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO invite_codes (code, max_uses, expires_at, node_group_id, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		code, maxUses, expiresAt, *nodeGroupID, createdBy).Scan(&id)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
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
	if ic.NodeGroupID == nil || *ic.NodeGroupID <= 0 {
		return nil, ErrInviteGroupRequired
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
	out := make([]InviteCode, 0)
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
	if err := ValidateSubscriptionSelection(s.Format, s.TemplateID, s.TemplateVersion); err != nil {
		return 0, err
	}
	if err := r.validateTemplateForCreate(ctx, s); err != nil {
		return 0, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO subscriptions (user_id, name, token, format, node_group_id, template_id, template_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		s.UserID, s.Name, s.Token, s.Format, s.NodeGroupID, s.TemplateID, s.TemplateVersion).Scan(&id)
	if isP2CompatibilityError(err) {
		err = r.db.QueryRowContext(ctx, `
			INSERT INTO subscriptions (user_id, name, token, format, node_group_id, template_id)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			s.UserID, s.Name, s.Token, s.Format, s.NodeGroupID, s.TemplateID).Scan(&id)
	}
	return id, err
}

func (r *SubscriptionRepo) CreateForUser(ctx context.Context, s *models.Subscription) (int64, error) {
	if s == nil || s.NodeGroupID == nil {
		return 0, ErrNodeNotAuthorized
	}
	if id, err := createSubscriptionForUserP2(ctx, r.db, s); err == nil || !isP2CompatibilityError(err) {
		return id, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO subscriptions (user_id, name, token, format, node_group_id, template_id)
		SELECT $1,$2,$3,$4,$5,$6
		WHERE EXISTS (
			SELECT 1 FROM users
			WHERE id=$1 AND is_active=TRUE
			  AND (expire_at IS NULL OR expire_at > now())
		)
		AND EXISTS (
			SELECT 1 FROM user_node_groups
			WHERE user_id=$1 AND group_id=$5
		)
		RETURNING id`,
		s.UserID, s.Name, s.Token, s.Format, s.NodeGroupID, s.TemplateID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNodeNotAuthorized
	}
	return id, err
}

// GetByToken 按 token 取订阅。
func (r *SubscriptionRepo) GetByToken(ctx context.Context, token string) (*models.Subscription, error) {
	return getSubscriptionCompat(ctx, r.db, `WHERE token=$1`, token)
}

// GetByID 按 ID 取订阅。
func (r *SubscriptionRepo) GetByID(ctx context.Context, id int64) (*models.Subscription, error) {
	return getSubscriptionCompat(ctx, r.db, `WHERE id=$1`, id)
}

func (r *SubscriptionRepo) GetByIDForUser(ctx context.Context, userID, id int64) (*models.Subscription, error) {
	s, err := getSubscriptionCompat(ctx, r.db, `WHERE id=$1 AND user_id=$2`, id, userID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrTemplateNotFound) {
		return nil, ErrSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ListByUser 列用户订阅。
func (r *SubscriptionRepo) ListByUser(ctx context.Context, userID int64) ([]models.Subscription, error) {
	return listSubscriptionsCompat(ctx, r.db, `WHERE user_id=$1 ORDER BY id`, userID)
}

// Delete 删除订阅。
func (r *SubscriptionRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=$1`, id)
	return err
}

func (r *SubscriptionRepo) DeleteForUser(ctx context.Context, userID, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrSubscriptionNotFound
	}
	return nil
}

// OverrideRow 覆写记录。
type OverrideRow struct {
	NodeID       int64
	InboundID    int64
	DisplayName  string
	SortOrder    int
	SortOrderSet bool
	Icon         string
	Params       json.RawMessage
	ProxyGroup   string
	Revision     int64
}

type OverrideKey struct {
	NodeID    int64
	InboundID int64
}

// ListOverrides 取订阅的覆写（map[节点ID]覆写）。
func (r *SubscriptionRepo) ListOverrides(ctx context.Context, subID int64) (map[int64]OverrideRow, error) {
	return listOverridesCompat(ctx, r.db, subID)
}

// SaveOverride 写入/更新单节点覆写。
func (r *SubscriptionRepo) SaveOverride(ctx context.Context, subID, nodeID int64, displayName string, sortOrder int, icon, proxyGroup string, params json.RawMessage) error {
	displayNamePtr := stringPtr(displayName)
	sortOrderPtr := &sortOrder
	iconPtr := stringPtr(icon)
	proxyGroupPtr := stringPtr(proxyGroup)
	patch := OverridePatch{DisplayName: displayNamePtr, SortOrder: sortOrderPtr, Icon: iconPtr, Params: params, ProxyGroup: proxyGroupPtr}
	if err := saveOverrideP2(ctx, r.db, subID, nodeID, patch); err == nil || !isP2CompatibilityError(err) {
		return err
	}
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

func (r *SubscriptionRepo) SaveOverrideForUser(ctx context.Context, userID, subID, nodeID int64, displayName string, sortOrder int, icon, proxyGroup string, params json.RawMessage) error {
	patch := OverridePatch{DisplayName: stringPtr(displayName), SortOrder: &sortOrder, Icon: stringPtr(icon), Params: params, ProxyGroup: stringPtr(proxyGroup)}
	if err := r.saveOverrideForUserP2(ctx, userID, subID, nodeID, 0, patch); err == nil || !isP2CompatibilityError(err) {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT node_group_id FROM subscriptions WHERE id=$1 AND user_id=$2`, subID, userID).Scan(&groupID); errors.Is(err, sql.ErrNoRows) {
		return ErrSubscriptionNotFound
	} else if err != nil {
		return err
	}
	if !groupID.Valid {
		return ErrNodeNotAuthorized
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM user_node_groups ug
			JOIN node_group_members ngm ON ngm.group_id=ug.group_id
			WHERE ug.user_id=$1 AND ug.group_id=$2 AND ngm.node_id=$3
		)`, userID, groupID.Int64, nodeID).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrNodeNotAuthorized
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscription_node_overrides (subscription_id, node_id, display_name, sort_order, icon, params, proxy_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (subscription_id, node_id) DO UPDATE
		SET display_name=EXCLUDED.display_name, sort_order=EXCLUDED.sort_order,
		    icon=EXCLUDED.icon, params=EXCLUDED.params, proxy_group=EXCLUDED.proxy_group,
		    updated_at=now()`,
		subID, nodeID, nullStr(displayName), sortOrder, nullStr(icon), nullJSON(params), nullStr(proxyGroup)); err != nil {
		return err
	}
	return tx.Commit()
}

// ListInboundOverrides returns overrides scoped to one subscription/node/inbound.
func (r *SubscriptionRepo) ListInboundOverrides(ctx context.Context, subID int64) (map[OverrideKey]OverrideRow, error) {
	return listInboundOverridesCompat(ctx, r.db, subID)
}

// SaveInboundOverrideForUser writes an override for one authorized inbound.
func (r *SubscriptionRepo) SaveInboundOverrideForUser(ctx context.Context, userID, subID, nodeID, inboundID int64, displayName string, sortOrder int, icon, proxyGroup string, params json.RawMessage) error {
	patch := OverridePatch{DisplayName: stringPtr(displayName), SortOrder: &sortOrder, Icon: stringPtr(icon), Params: params, ProxyGroup: stringPtr(proxyGroup)}
	if err := r.saveOverrideForUserP2(ctx, userID, subID, nodeID, inboundID, patch); err == nil || !isP2CompatibilityError(err) {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT node_group_id FROM subscriptions WHERE id=$1 AND user_id=$2`, subID, userID).Scan(&groupID); errors.Is(err, sql.ErrNoRows) {
		return ErrSubscriptionNotFound
	} else if err != nil {
		return err
	}
	if !groupID.Valid {
		return ErrNodeNotAuthorized
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM inbounds i
			JOIN node_group_members ngm ON ngm.node_id=i.node_id
			JOIN user_node_groups ug ON ug.group_id=ngm.group_id
			JOIN users u ON u.id=ug.user_id
			WHERE i.id=$1 AND i.node_id=$2 AND i.role='entry'
			  AND ug.user_id=$3 AND ug.group_id=$4
			  AND u.is_active=TRUE AND (u.expire_at IS NULL OR u.expire_at > now())
		)`, inboundID, nodeID, userID, groupID.Int64).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrNodeNotAuthorized
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscription_inbound_overrides (subscription_id, node_id, inbound_id, display_name, sort_order, icon, params, proxy_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (subscription_id, node_id, inbound_id) DO UPDATE
		SET display_name=EXCLUDED.display_name, sort_order=EXCLUDED.sort_order,
		    icon=EXCLUDED.icon, params=EXCLUDED.params, proxy_group=EXCLUDED.proxy_group,
		    updated_at=now()`,
		subID, nodeID, inboundID, nullStr(displayName), sortOrder, nullStr(icon), nullJSON(params), nullStr(proxyGroup)); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- 节点组 ----

// GroupRepo 节点组数据访问。
type GroupRepo struct{ db *sql.DB }

func NewGroupRepo(db *sql.DB) *GroupRepo { return &GroupRepo{db: db} }

// NodeGroup 节点组。
type NodeGroup struct {
	ID                   int64           `json:"id"`
	Name                 string          `json:"name"`
	NodeIDs              []int64         `json:"nodeIds"`
	Revision             int64           `json:"revision"`
	SubscriptionDefaults json.RawMessage `json:"subscriptionDefaults"`
}

// List 列出节点组。
func (r *GroupRepo) List(ctx context.Context) ([]NodeGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name,revision,subscription_defaults FROM node_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]NodeGroup, 0)
	for rows.Next() {
		var g NodeGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Revision, &g.SubscriptionDefaults); err != nil {
			return nil, err
		}
		g.NodeIDs, err = r.NodeIDs(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		if g.NodeIDs == nil {
			g.NodeIDs = make([]int64, 0)
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
	rows, err := r.db.QueryContext(ctx, `SELECT node_id FROM node_group_members WHERE group_id=$1 ORDER BY node_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetNodeIDs replaces a group's node membership after validating every
// referenced node. The replacement is transactional and duplicate IDs are
// rejected rather than silently collapsing user input.
func (r *GroupRepo) SetNodeIDs(ctx context.Context, groupID int64, nodeIDs []int64) error {
	if groupID <= 0 {
		return ErrNodeGroupNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_groups WHERE id=$1)`, groupID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNodeGroupNotFound
	}
	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			return errors.New("invalid node id")
		}
		if _, duplicate := seen[nodeID]; duplicate {
			return errors.New("duplicate node id")
		}
		seen[nodeID] = struct{}{}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id=$1)`, nodeID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_group_members WHERE group_id=$1`, groupID); err != nil {
		return err
	}
	for _, nodeID := range nodeIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_group_members (group_id, node_id) VALUES ($1,$2)`, groupID, nodeID); err != nil {
			return err
		}
	}
	if err := InvalidateTopologyAuthorization(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// AdminUser is the safe user summary exposed to administrators.
type AdminUser struct {
	ID       int64      `json:"id"`
	Username string     `json:"username"`
	Email    string     `json:"email,omitempty"`
	Role     string     `json:"role"`
	Active   bool       `json:"active"`
	Status   string     `json:"status"`
	ExpireAt *time.Time `json:"expiresAt,omitempty"`
	GroupIDs []int64    `json:"groupIds"`
}

// ListAdminUsers returns user summaries without password hashes or tokens.
func (r *UserRepo) ListAdminUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, username, email, role, is_active, expire_at
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminUser, 0)
	for rows.Next() {
		var user AdminUser
		var email sql.NullString
		if err := rows.Scan(&user.ID, &user.Username, &email, &user.Role, &user.Active, &user.ExpireAt); err != nil {
			return nil, err
		}
		user.Email = email.String
		user.Status = adminUserStatus(user.Active, user.ExpireAt)
		user.GroupIDs, err = r.UserGroupIDs(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		if user.GroupIDs == nil {
			user.GroupIDs = make([]int64, 0)
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func adminUserStatus(active bool, expiresAt *time.Time) string {
	if !active {
		return "inactive"
	}
	if expiresAt != nil && !time.Now().Before(*expiresAt) {
		return "expired"
	}
	return "active"
}

func (r *UserRepo) UserGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT group_id FROM user_node_groups WHERE user_id=$1 ORDER BY group_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		out = append(out, groupID)
	}
	return out, rows.Err()
}

// SetUserGroupIDs validates all foreign keys before replacing membership.
func (r *UserRepo) SetUserGroupIDs(ctx context.Context, userID int64, groupIDs []int64) error {
	if userID <= 0 {
		return sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return errors.New("invalid group id")
		}
		if _, duplicate := seen[groupID]; duplicate {
			return errors.New("duplicate group id")
		}
		seen[groupID] = struct{}{}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_groups WHERE id=$1)`, groupID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_node_groups WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_node_groups (user_id, group_id) VALUES ($1,$2)`, userID, groupID); err != nil {
			return err
		}
	}
	if err := InvalidateTopologyAuthorization(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *GroupRepo) UserHasGroup(ctx context.Context, userID, groupID int64) (bool, error) {
	var authorized bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM user_node_groups WHERE user_id=$1 AND group_id=$2)`, userID, groupID).Scan(&authorized)
	return authorized, err
}

func (r *GroupRepo) Exists(ctx context.Context, groupID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_groups WHERE id=$1)`, groupID).Scan(&exists)
	return exists, err
}

func (r *GroupRepo) AuthorizedNodeIDs(ctx context.Context, userID, groupID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ngm.node_id
		FROM user_node_groups ug
		JOIN node_group_members ngm ON ngm.group_id=ug.group_id
		WHERE ug.user_id=$1 AND ug.group_id=$2
		ORDER BY ngm.node_id`, userID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var nodeID int64
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		out = append(out, nodeID)
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
