// Package models 定义领域模型（与数据库表对应）。
package models

import (
	"encoding/json"
	"time"
)

// Node 服务器节点（受管 + 外部）。
type Node struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Type              string          `json:"type"` // managed / external
	PublicIP          string          `json:"publicIp"`
	EasyIP            string          `json:"easyIp"`
	SSHHost           string          `json:"sshHost,omitempty"`
	SSHUser           string          `json:"sshUser,omitempty"`
	SSHPort           int             `json:"sshPort,omitempty"`
	CoreVersion       string          `json:"coreVersion,omitempty"`
	Status            string          `json:"status"`
	LastSeenAt        *time.Time      `json:"lastSeenAt,omitempty"`
	ExtProtocol       string          `json:"extProtocol,omitempty"`
	ExtParams         json.RawMessage `json:"extParams,omitempty"`
	AgentCapabilities []string        `json:"agentCapabilities,omitempty"`
	ConfigGeneration  int64           `json:"configGeneration"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

// Inbound 入站。
type Inbound struct {
	ID           int64           `json:"id"`
	NodeID       int64           `json:"nodeId"`
	Name         string          `json:"name"`
	Protocol     string          `json:"protocol"` // vless-reality / shadowsocks / hysteria2
	Role         string          `json:"role"`     // entry / landing / relay
	EgressMode   string          `json:"egressMode"`
	Revision     int64           `json:"revision"`
	ListenAddr   string          `json:"listenAddr"`
	ListenPort   int             `json:"listenPort"`
	Config       json.RawMessage `json:"config"`
	MinClientVer string          `json:"minClientVer"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// User 用户。
type User struct {
	ID                int64      `json:"id"`
	Username          string     `json:"username"`
	PasswordHash      string     `json:"-"`
	Email             string     `json:"email,omitempty"`
	Role              string     `json:"role"`
	Active            bool       `json:"active"`
	TrafficLimitBytes int64      `json:"trafficLimitBytes"`
	ExpireAt          *time.Time `json:"expireAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// UserCredential is a per-user credential assigned to one managed inbound.
// Credential is never serialized through the public model API.
type UserCredential struct {
	UserID     int64  `json:"userId"`
	InboundID  int64  `json:"inboundId"`
	Username   string `json:"username,omitempty"`
	Protocol   string `json:"protocol"`
	Credential string `json:"-"`
}

// Subscription 订阅。
type Subscription struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"userId"`
	Name            string    `json:"name"`
	Token           string    `json:"token"`
	Format          string    `json:"format"`
	NodeGroupID     *int64    `json:"nodeGroupId,omitempty"`
	TemplateID      *int64    `json:"templateId,omitempty"`
	TemplateVersion *int      `json:"templateVersion,omitempty"`
	Revision        int64     `json:"revision"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// TopologyEdge is a user-editable reference between two persisted inbounds.
// Server-side code resolves the referenced node and protocol material before
// previewing or deploying it.
type TopologyEdge struct {
	FromInboundID int64 `json:"fromInboundId"`
	ToNodeID      int64 `json:"toNodeId"`
	ToInboundID   int64 `json:"toInboundId"`
}

// TopologyDraft is the durable, not-yet-deployed topology for one node.
type TopologyDraft struct {
	NodeID           int64           `json:"nodeId"`
	Edges            []TopologyEdge  `json:"edges"`
	Revision         int64           `json:"revision"`
	Layout           json.RawMessage `json:"layout,omitempty"`
	ValidationStatus string          `json:"validationStatus,omitempty"`
	UpdatedAt        time.Time       `json:"updatedAt,omitempty"`
}

// TopologyDeployment is the last explicit deployment snapshot. Credentials
// are intentionally not part of the snapshot; the active roster is merged
// when an agent retrieves its deployed topology.
type TopologyDeployment struct {
	NodeID        int64
	Version       string
	SchemaVersion string
	DraftRevision int64
	Topology      json.RawMessage
	Rendered      []byte
	ReleaseID     *int64
	Generation    int64
	DeployedAt    time.Time
}

// TopologyGraphChange is the atomic unit of a full-graph edit.
type TopologyGraphChange struct {
	NodeID           int64            `json:"nodeId"`
	ExpectedRevision int64            `json:"expectedRevision"`
	Edges            []TopologyEdge   `json:"edges"`
	Layout           json.RawMessage  `json:"layout,omitempty"`
	InboundModes     map[int64]string `json:"inboundModes,omitempty"`
}
