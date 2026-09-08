package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

type deploymentAuthStore struct{}

func (deploymentAuthStore) AuthenticateAgent(context.Context, int64, string) error { return nil }

type deploymentStore struct {
	document *contract.TopologyDeploymentDocument
	ack      *contract.DeploymentAck
}

func (s *deploymentStore) GetAgentDeployment(_ context.Context, nodeID, releaseID int64, phase string) (*contract.TopologyDeploymentDocument, error) {
	if s.document == nil || s.document.NodeID != nodeID || s.document.ReleaseID != releaseID || s.document.Phase != phase {
		return nil, context.Canceled
	}
	return s.document, nil
}

func (s *deploymentStore) AcknowledgeAgentDeployment(_ context.Context, ack *contract.DeploymentAck) error {
	s.ack = ack
	return nil
}

func TestDeploymentHandlerAuthenticatesNodeAndReturnsCandidate(t *testing.T) {
	store := &deploymentStore{document: &contract.TopologyDeploymentDocument{
		ConfigDocument: validDeploymentConfig(t, 7),
		ReleaseID:      12,
		Generation:     3,
		Phase:          contract.DeploymentPhasePrepare,
		RoutingVersion: "routing-version",
	}}
	handler := NewDeploymentHandler(deploymentAuthStore{}, store)
	request := httptest.NewRequest(http.MethodGet, "/api/agent/config?phase=prepare&releaseId=12", nil)
	request.Header.Set(contract.AgentNodeIDHeader, "7")
	request.Header.Set(contract.AgentCredentialHeader, "synthetic-agent-credential")
	response := httptest.NewRecorder()
	handler.GetConfig(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GetConfig() status = %d, body = %s", response.Code, response.Body.String())
	}
	var got contract.TopologyDeploymentDocument
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode candidate: %v", err)
	}
	if got.ReleaseID != 12 || got.Generation != 3 || got.Phase != contract.DeploymentPhasePrepare {
		t.Fatalf("candidate = %+v", got)
	}
}

func TestDeploymentHandlerRejectsMismatchedAckNode(t *testing.T) {
	store := &deploymentStore{}
	handler := NewDeploymentHandler(deploymentAuthStore{}, store)
	body, err := json.Marshal(contract.DeploymentAck{ReleaseID: 12, NodeID: 8, Generation: 3, Phase: contract.DeploymentPhaseApply, Status: contract.AckStatusApplied})
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-acks", bytes.NewReader(body))
	request.Header.Set(contract.AgentNodeIDHeader, "7")
	request.Header.Set(contract.AgentCredentialHeader, "synthetic-agent-credential")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.Acknowledge(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("Acknowledge() status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHandlerDelegatesPhaseConfigToTopologyDeploymentHandler(t *testing.T) {
	store := &deploymentStore{document: &contract.TopologyDeploymentDocument{
		ConfigDocument: validDeploymentConfig(t, 7),
		ReleaseID:      12,
		Generation:     3,
		Phase:          contract.DeploymentPhaseApply,
		RoutingVersion: "routing-version",
	}}
	handler := &Handler{DeploymentV2: NewDeploymentHandler(deploymentAuthStore{}, store)}
	request := httptest.NewRequest(http.MethodGet, "/api/agent/config?phase=apply&releaseId=12", nil)
	request.Header.Set(contract.AgentNodeIDHeader, "7")
	request.Header.Set(contract.AgentCredentialHeader, "synthetic-agent-credential")
	response := httptest.NewRecorder()
	handler.GetConfig(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GetConfig() status = %d, body = %s", response.Code, response.Body.String())
	}
	var got contract.TopologyDeploymentDocument
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode deployment document: %v", err)
	}
	if got.ReleaseID != 12 || got.Generation != 3 || got.Phase != contract.DeploymentPhaseApply {
		t.Fatalf("deployment document = %+v", got)
	}
}

func validDeploymentConfig(t *testing.T, nodeID int64) contract.ConfigDocument {
	t.Helper()
	topology := config.NodeConfig{
		SchemaVersion: config.SchemaVersion,
		NodeID:        nodeID,
		Inbounds:      []config.Inbound{},
		Edges:         []config.Edge{},
		Outbound:      "direct",
	}
	rendered, err := config.RenderRouting(topology)
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewConfigDocument(topology, *rendered, true)
}
