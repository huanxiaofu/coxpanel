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
	NodeID               int64    `json:"nodeId"`
	Version              string   `json:"version"`
	CoreVersion          string   `json:"coreVersion"`
	CPU                  float64  `json:"cpu"`
	Mem                  float64  `json:"mem"`
	OnlineUsers          int      `json:"onlineUsers"`
	Uptime               int64    `json:"uptime"`
	Capabilities         []string `json:"capabilities,omitempty"`
	ConfigGeneration     int64    `json:"configGeneration,omitempty"`
	AppliedRuntimeVersion string   `json:"appliedRuntimeVersion,omitempty"`
	At                   time.Time `json:"at"`
}

type PendingDeployment struct {
	ReleaseID  int64  `json:"releaseId"`
	Phase      string `json:"phase"`
	Generation int64  `json:"generation"`
}

func (p PendingDeployment) Validate() error {
	if p.ReleaseID <= 0 || p.Generation <= 0 {
		return fmt.Errorf("pending deployment identity is invalid")
	}
	switch p.Phase {
	case DeploymentPhasePrepare, DeploymentPhaseApply, DeploymentPhaseRollback:
		return nil
	default:
		return fmt.Errorf("unsupported pending deployment phase: %s", p.Phase)
	}
}

type HeartbeatResponse struct {
	OK                bool               `json:"ok"`
	PendingDeployment *PendingDeployment `json:"pendingDeployment,omitempty"`
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

const (
	AgentCapabilityTopologyChainV2 = "topology-chain-v2"

	DeploymentPhasePrepare  = "prepare"
	DeploymentPhaseApply    = "apply"
	DeploymentPhaseRollback = "rollback"

	AckStatusPrepared  = "prepared"
	AckStatusApplied   = "applied"
	AckStatusRolledBack = "rolled_back"
	AckStatusFailed    = "failed"
)

// TopologyDeploymentDocument is the explicit, phase-bound document used by
// the P2 deployment protocol. It embeds the P1 document so old clients can
// continue to decode the shared config fields.
type TopologyDeploymentDocument struct {
	ConfigDocument
	ReleaseID      int64  `json:"releaseId"`
	Generation     int64  `json:"generation"`
	Phase          string `json:"phase"`
	RoutingVersion string `json:"routingVersion"`
	Token          string `json:"token,omitempty"`
}

func (d TopologyDeploymentDocument) Validate(requireExplicit bool) error {
	if d.ReleaseID <= 0 {
		return fmt.Errorf("invalid releaseId")
	}
	if d.Generation <= 0 {
		return fmt.Errorf("invalid generation")
	}
	switch d.Phase {
	case DeploymentPhasePrepare, DeploymentPhaseApply, DeploymentPhaseRollback:
	default:
		return fmt.Errorf("unsupported deployment phase: %s", d.Phase)
	}
	if strings.TrimSpace(d.RoutingVersion) == "" {
		return fmt.Errorf("missing routingVersion")
	}
	return d.ConfigDocument.Validate(requireExplicit)
}

// DeploymentAck is the authenticated agent receipt for one release phase.
type DeploymentAck struct {
	ReleaseID      int64  `json:"releaseId"`
	NodeID         int64  `json:"nodeId"`
	Generation     int64  `json:"generation"`
	Phase          string `json:"phase"`
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	Status         string `json:"status"`
	Token          string `json:"token,omitempty"`
	ErrorCode      string `json:"errorCode,omitempty"`
}

func (a DeploymentAck) Validate() error {
	if a.ReleaseID <= 0 || a.NodeID <= 0 || a.Generation <= 0 {
		return fmt.Errorf("deployment ack identity is invalid")
	}
	switch a.Phase {
	case DeploymentPhasePrepare, DeploymentPhaseApply, DeploymentPhaseRollback:
	default:
		return fmt.Errorf("unsupported deployment phase: %s", a.Phase)
	}
	switch a.Status {
	case AckStatusPrepared, AckStatusApplied, AckStatusRolledBack, AckStatusFailed:
	default:
		return fmt.Errorf("unsupported deployment ack status: %s", a.Status)
	}
	if a.Status == AckStatusFailed && strings.TrimSpace(a.ErrorCode) == "" {
		return fmt.Errorf("failed deployment ack requires errorCode")
	}
	if a.Status != AckStatusFailed {
		want := map[string]string{
			DeploymentPhasePrepare:  AckStatusPrepared,
			DeploymentPhaseApply:    AckStatusApplied,
			DeploymentPhaseRollback: AckStatusRolledBack,
		}[a.Phase]
		if a.Status != want {
			return fmt.Errorf("deployment ack status does not match phase")
		}
	}
	return nil
}

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
