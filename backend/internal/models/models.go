// Package models 定义领域模型（与数据库表对应）。
package models

import (
	"encoding/json"
	"time"
)

// Node 服务器节点（受管 + 外部）。
type Node struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"` // managed / external
	PublicIP    string          `json:"publicIp"`
	EasyIP      string          `json:"easyIp"`
	SSHHost     string          `json:"sshHost,omitempty"`
	SSHUser     string          `json:"sshUser,omitempty"`
	SSHPort     int             `json:"sshPort,omitempty"`
	CoreVersion string          `json:"coreVersion,omitempty"`
	Status      string          `json:"status"`
	LastSeenAt  *time.Time      `json:"lastSeenAt,omitempty"`
	ExtProtocol string          `json:"extProtocol,omitempty"`
	ExtParams   json.RawMessage `json:"extParams,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// Inbound 入站。
type Inbound struct {
	ID           int64           `json:"id"`
	NodeID       int64           `json:"nodeId"`
	Name         string          `json:"name"`
	Protocol     string          `json:"protocol"` // vless-reality / shadowsocks / hysteria2
	Role         string          `json:"role"`     // entry / landing / relay
	ListenAddr   string          `json:"listenAddr"`
	ListenPort   int             `json:"listenPort"`
	Config       json.RawMessage `json:"config"`
	MinClientVer string          `json:"minClientVer"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// User 用户。
type User struct {
	ID                  int64      `json:"id"`
	Username            string     `json:"username"`
	PasswordHash        string     `json:"-"`
	Email               string     `json:"email,omitempty"`
	Role                string     `json:"role"`
	TrafficLimitBytes   int64      `json:"trafficLimitBytes"`
	ExpireAt            *time.Time `json:"expireAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// Subscription 订阅。
type Subscription struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	Name        string    `json:"name"`
	Token       string    `json:"token"`
	Format      string    `json:"format"`
	NodeGroupID *int64    `json:"nodeGroupId,omitempty"`
	TemplateID  *int64    `json:"templateId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
