package contract

import (
	"encoding/json"
	"testing"

	"github.com/coxpanel/shared/config"
)

func TestTopologyDeploymentDocumentRequiresPhaseAndGeneration(t *testing.T) {
	topology := config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: 7, Inbounds: []config.Inbound{}, Edges: []config.Edge{}, Outbound: "direct"}
	rendered, err := config.RenderRouting(topology)
	if err != nil {
		t.Fatal(err)
	}
	document := TopologyDeploymentDocument{
		ConfigDocument: NewConfigDocument(topology, *rendered, true),
		ReleaseID:      11,
		Generation:     4,
		Phase:          DeploymentPhasePrepare,
		RoutingVersion: "routing-version",
	}
	if err := document.Validate(false); err != nil {
		t.Fatalf("prepare document validation error = %v", err)
	}
	body, err := document.JSONBytes()
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip TopologyDeploymentDocument
	if json.Unmarshal(body, &roundTrip) != nil || roundTrip.Validate(true) != nil {
		t.Fatal("deployment wire must preserve runtime bytes and hash")
	}
	document.Phase = "unknown"
	if err := document.Validate(false); err == nil {
		t.Fatal("unknown deployment phase was accepted")
	}
	document.Phase = DeploymentPhaseApply
	document.Generation = 0
	if err := document.Validate(false); err == nil {
		t.Fatal("zero deployment generation was accepted")
	}
}

func TestHeartbeatCarriesTopologyCapabilityWithoutChangingP1Fields(t *testing.T) {
	heartbeat := Heartbeat{NodeID: 7, Version: "runtime-version", Capabilities: []string{AgentCapabilityTopologyChainV2}, ConfigGeneration: 4, AppliedRuntimeVersion: "runtime-version"}
	encoded, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	var decoded Heartbeat
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal heartbeat: %v", err)
	}
	if decoded.NodeID != heartbeat.NodeID || decoded.Version != heartbeat.Version || len(decoded.Capabilities) != 1 || decoded.Capabilities[0] != AgentCapabilityTopologyChainV2 || decoded.ConfigGeneration != 4 || decoded.AppliedRuntimeVersion != heartbeat.AppliedRuntimeVersion {
		t.Fatalf("heartbeat round trip = %+v", decoded)
	}
}

func TestHeartbeatResponseCarriesPendingDeployment(t *testing.T) {
	response := HeartbeatResponse{
		OK:                true,
		PendingDeployment: &PendingDeployment{ReleaseID: 12, Phase: DeploymentPhaseApply, Generation: 4},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal heartbeat response: %v", err)
	}
	var decoded HeartbeatResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal heartbeat response: %v", err)
	}
	if decoded.PendingDeployment == nil || decoded.PendingDeployment.ReleaseID != 12 || decoded.PendingDeployment.Phase != DeploymentPhaseApply || decoded.PendingDeployment.Generation != 4 {
		t.Fatalf("pending deployment = %+v", decoded.PendingDeployment)
	}
	if err := decoded.PendingDeployment.Validate(); err != nil {
		t.Fatalf("pending deployment validation: %v", err)
	}
}
