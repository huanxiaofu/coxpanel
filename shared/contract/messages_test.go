package contract

import (
	"testing"

	"github.com/coxpanel/shared/config"
)

func TestConfigDocumentBindsContentHashAndExplicitDeployment(t *testing.T) {
	topology := config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: 9}
	rendered := config.RenderedConfig{SchemaVersion: config.SchemaVersion, Content: []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`)}
	rendered.Version = config.Hash(rendered.Content)
	rendered.SHA256 = rendered.Version
	document := NewConfigDocument(topology, rendered, false)
	if err := document.Validate(false); err != nil {
		t.Fatalf("Validate(false) error = %v", err)
	}
	if err := document.Validate(true); err == nil {
		t.Fatalf("Validate(true) error = nil, want explicit confirmation failure")
	}
	document.Explicit = true
	if err := document.Validate(true); err != nil {
		t.Fatalf("Validate(true) error = %v", err)
	}
	document.Singbox = append(document.Singbox, ' ')
	if err := document.Validate(false); err == nil {
		t.Fatalf("Validate() error = nil after content mutation")
	}
}

func TestConfigDocumentRejectsTopologyNodeMismatch(t *testing.T) {
	topology := config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: 9}
	rendered := config.RenderedConfig{SchemaVersion: config.SchemaVersion, Content: []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`)}
	rendered.Version = config.Hash(rendered.Content)
	rendered.SHA256 = rendered.Version
	document := NewConfigDocument(topology, rendered, true)
	document.Topology.NodeID = 10

	if err := document.Validate(true); err == nil {
		t.Fatalf("Validate() error = nil for topology node mismatch")
	}
}

func TestConfigDocumentRejectsTopologySchemaMismatch(t *testing.T) {
	topology := config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: 9}
	rendered := config.RenderedConfig{SchemaVersion: config.SchemaVersion, Content: []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`)}
	rendered.Version = config.Hash(rendered.Content)
	rendered.SHA256 = rendered.Version
	document := NewConfigDocument(topology, rendered, true)
	document.Topology.SchemaVersion = "sing-box/unsupported"

	if err := document.Validate(true); err == nil {
		t.Fatalf("Validate() error = nil for topology schema mismatch")
	}
}
