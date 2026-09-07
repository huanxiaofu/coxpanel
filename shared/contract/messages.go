// Package contract defines agent to panel message formats.
package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/coxpanel/shared/config"
)

const (
	AgentNodeIDHeader     = "X-Node-Id"
	AgentCredentialHeader = "X-Agent-Credential"
)

// Heartbeat is the periodic agent status message.
type Heartbeat struct {
	NodeID      int64     `json:"nodeId"`
	Version     string    `json:"version"`
	CoreVersion string    `json:"coreVersion"`
	CPU         float64   `json:"cpu"`
	Mem         float64   `json:"mem"`
	OnlineUsers int       `json:"onlineUsers"`
	Uptime      int64     `json:"uptime"`
	At          time.Time `json:"at"`
}

// TrafficReport is a user-level traffic report.
type TrafficReport struct {
	NodeID int64         `json:"nodeId"`
	Period time.Time     `json:"period"`
	Users  []UserTraffic `json:"users"`
	At     time.Time     `json:"at"`
}

// UserTraffic is traffic for one user on one inbound.
type UserTraffic struct {
	UserUUID  string `json:"userUuid"`
	InboundID int64  `json:"inboundId"`
	UpBytes   int64  `json:"upBytes"`
	DownBytes int64  `json:"downBytes"`
}

// ConfigPush is a lightweight notification that points to a full document.
type ConfigPush struct {
	// Version is the SHA-256 of the rendered config bytes, not a timestamp.
	Version       string `json:"version"`
	SchemaVersion string `json:"schemaVersion,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	NodeID        int64  `json:"nodeId,omitempty"`
	ConfigURL     string `json:"configUrl,omitempty"`
	// URL is retained during the HTTP migration and is deprecated.
	URL      string `json:"url,omitempty"`
	Explicit bool   `json:"explicit,omitempty"`
}

// AgentCredentialIssue is returned once when a managed node is provisioned.
// The credential must be bcrypt-hashed by the backend immediately and must
// only be sent later in AgentCredentialHeader, never as a user JWT.
type AgentCredentialIssue struct {
	NodeID     int64  `json:"nodeId"`
	Credential string `json:"credential"`
}

// AgentAuthentication is an in-memory header pair used by agent requests.
// Credential is deliberately excluded from JSON serialization.
type AgentAuthentication struct {
	NodeID     int64  `json:"nodeId"`
	Credential string `json:"-"`
}

// ConfigDocument is the complete document an agent may apply.
type ConfigDocument struct {
	SchemaVersion string            `json:"schemaVersion"`
	Version       string            `json:"version"`
	SHA256        string            `json:"sha256"`
	NodeID        int64             `json:"nodeId"`
	Explicit      bool              `json:"explicit"`
	Topology      config.NodeConfig `json:"topology,omitempty"`
	Singbox       json.RawMessage   `json:"singbox"`
}

// ApplyRequest is intentionally explicit; save and preview must not apply.
type ApplyRequest struct {
	Document ConfigDocument `json:"document"`
	Explicit bool           `json:"explicit"`
}

// ApplyResult is returned only after real process readiness is confirmed.
type ApplyResult struct {
	SchemaVersion string `json:"schemaVersion"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
}

const (
	ApplyStateApplied  = "applied"
	ApplyStateRestored = "restored"
)

// NewConfigDocument binds a rendered result to its topology.
func NewConfigDocument(topology config.NodeConfig, rendered config.RenderedConfig, explicit bool) ConfigDocument {
	return ConfigDocument{
		SchemaVersion: rendered.SchemaVersion,
		Version:       rendered.Version,
		SHA256:        rendered.SHA256,
		NodeID:        topology.NodeID,
		Explicit:      explicit,
		Topology:      topology,
		Singbox:       append(json.RawMessage(nil), rendered.Content...),
	}
}

// Validate rejects stale metadata, changed content, and implicit applies.
func (d ConfigDocument) Validate(requireExplicit bool) error {
	if d.SchemaVersion != config.SchemaVersion {
		return fmt.Errorf("unsupported schema version: %s", d.SchemaVersion)
	}
	if d.NodeID <= 0 {
		return fmt.Errorf("invalid nodeId")
	}
	if d.Topology.NodeID != d.NodeID {
		return fmt.Errorf("topology node binding mismatch")
	}
	if d.Topology.SchemaVersion != config.SchemaVersion {
		return fmt.Errorf("topology schema version mismatch")
	}
	if !json.Valid(d.Singbox) {
		return fmt.Errorf("sing-box config is not valid JSON")
	}
	if err := config.ValidateRendered(d.Singbox); err != nil {
		return err
	}
	want := config.Hash(d.Singbox)
	if !strings.EqualFold(d.Version, want) || !strings.EqualFold(d.SHA256, want) {
		return fmt.Errorf("config version hash mismatch")
	}
	if requireExplicit && !d.Explicit {
		return fmt.Errorf("explicit deployment confirmation is required")
	}
	return nil
}
