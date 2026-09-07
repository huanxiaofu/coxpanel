// Package config defines the shared panel, backend, and agent configuration contract.
package config

import "time"

const (
	// SingBoxVersion is the fixed upstream release used by the renderer.
	SingBoxVersion = "1.13.21"
	// SchemaVersion identifies the upstream schema emitted by the renderer.
	SchemaVersion = "sing-box/v1.13.21"
)

// NodeConfig is the saved and delivered topology model.
type NodeConfig struct {
	// Deprecated: deployment identity is the hash of rendered sing-box bytes.
	Version string `json:"version,omitempty"`
	// Deprecated: timestamps must not participate in deployment identity.
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
	SchemaVersion string    `json:"schemaVersion,omitempty"`

	NodeID      int64            `json:"nodeId"`
	NodeName    string           `json:"nodeName"`
	Inbounds    []Inbound        `json:"inbounds"`
	Edges       []Edge           `json:"edges"`
	Credentials []UserCredential `json:"credentials,omitempty"`
	Outbound    string           `json:"outbound"`
}

// Inbound is a node inbound.
type Inbound struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Protocol string            `json:"protocol"`
	Role     string            `json:"role"`
	Listen   string            `json:"listen"`
	Port     int               `json:"port"`
	Params   map[string]string `json:"params,omitempty"`
}

// UserCredential is an independent user credential for one inbound.
type UserCredential struct {
	UserID    int64  `json:"userId"`
	InboundID int64  `json:"inboundId"`
	Name      string `json:"name,omitempty"`
	Protocol  string `json:"protocol"`
	UUID      string `json:"uuid,omitempty"`
	Password  string `json:"password,omitempty"`
}

// Edge connects a local inbound to a remote inbound.
type Edge struct {
	ID            int64             `json:"id"`
	FromInboundID int64             `json:"fromInboundId"`
	ToNodeID      int64             `json:"toNodeId"`
	ToInboundID   int64             `json:"toInboundId"`
	ToServer      string            `json:"toServer"`
	ToPort        int               `json:"toPort"`
	ToProtocol    string            `json:"toProtocol"`
	ToParams      map[string]string `json:"toParams,omitempty"`
}
