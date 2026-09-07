package agent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/models"
	sharedconfig "github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

type fakeNodeStore struct {
	nodes                   map[int64]*models.Node
	credentials             map[int64]string
	inbounds                map[int64][]models.Inbound
	deployments             map[int64]*models.TopologyDeployment
	listCalls               int
	deploymentForAgentCalls int
}

type fakeUserCredentialStore struct {
	credentials []models.UserCredential
}

func (f *fakeUserCredentialStore) ListUserCredentialsForNode(context.Context, int64) ([]models.UserCredential, error) {
	return f.credentials, nil
}

func (f *fakeNodeStore) AuthenticateAgent(_ context.Context, nodeID int64, credential string) error {
	if f.credentials[nodeID] != credential {
		return errors.New("invalid agent credential")
	}
	return nil
}

func (f *fakeNodeStore) Get(_ context.Context, nodeID int64) (*models.Node, error) {
	node, ok := f.nodes[nodeID]
	if !ok {
		return nil, errors.New("node not found")
	}
	return node, nil
}

func (f *fakeNodeStore) ListInbounds(_ context.Context, nodeID int64) ([]models.Inbound, error) {
	f.listCalls++
	return f.inbounds[nodeID], nil
}

func (f *fakeNodeStore) UpdateStatus(_ context.Context, _ int64, _, _ string) error {
	return nil
}

func TestAgentRejectsInvalidCredentialsAndUserJWT(t *testing.T) {
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{1: {ID: 1, Name: "node-1", Type: "managed"}},
		credentials: map[int64]string{1: "agent-secret-1"},
	}
	handler := &Handler{Nodes: store}
	authService := auth.NewService("synthetic-test-secret", 0)
	userJWT, err := authService.IssueToken(42, "user-42", "owner")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		credential string
		authority  string
		nodeID     string
	}{
		{name: "missing credential", nodeID: "1"},
		{name: "wrong credential", credential: "wrong", nodeID: "1"},
		{name: "user JWT impersonation", authority: "Bearer " + userJWT, nodeID: "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
			req.Header.Set("X-Node-Id", test.nodeID)
			if test.credential != "" {
				req.Header.Set("X-Agent-Credential", test.credential)
			}
			if test.authority != "" {
				req.Header.Set("Authorization", test.authority)
			}
			resp := httptest.NewRecorder()
			handler.GetConfig(resp, req)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAgentRejectsCredentialBoundToAnotherNode(t *testing.T) {
	store := &fakeNodeStore{
		nodes: map[int64]*models.Node{
			1: {ID: 1, Name: "node-1", Type: "managed"},
			2: {ID: 2, Name: "node-2", Type: "managed"},
		},
		credentials: map[int64]string{1: "agent-secret-1", 2: "agent-secret-2"},
	}
	handler := &Handler{Nodes: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set("X-Node-Id", "2")
	req.Header.Set("X-Agent-Credential", "agent-secret-1")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestAgentAcceptsCredentialForBoundNode(t *testing.T) {
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{1: {ID: 1, Name: "node-1", Type: "managed"}},
		credentials: map[int64]string{1: "agent-secret-1"},
		deployments: map[int64]*models.TopologyDeployment{1: explicitEmptyDeployment(t, 1)},
	}
	handler := &Handler{Nodes: store, Deployments: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set("X-Node-Id", "1")
	req.Header.Set("X-Agent-Credential", "agent-secret-1")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestAgentReturnsApplyableNoListenerDocumentAfterLastCredentialRevocation(t *testing.T) {
	inbounds := []models.Inbound{{
		ID: 11, NodeID: 4, Name: "ss2022", Protocol: "shadowsocks", Role: "entry",
		ListenAddr: "127.0.0.1", ListenPort: 8388,
		Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}`),
	}}
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{4: {ID: 4, Name: "node-4", Type: "managed"}},
		credentials: map[int64]string{4: "agent-secret-4"},
		deployments: map[int64]*models.TopologyDeployment{4: explicitDeployment(t, &models.Node{ID: 4, Name: "node-4", Type: "managed"}, inbounds, nil)},
	}
	handler := &Handler{Nodes: store, Users: &fakeUserCredentialStore{}, Deployments: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set(contract.AgentNodeIDHeader, "4")
	req.Header.Set(contract.AgentCredentialHeader, "agent-secret-4")
	response := httptest.NewRecorder()
	handler.GetConfig(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var document contract.ConfigDocument
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode no-listener document: %v", err)
	}
	if err := document.Validate(true); err != nil {
		t.Fatalf("no-listener document validation: %v", err)
	}
	var rendered sharedconfig.SingBoxConfig
	if err := json.Unmarshal(document.Singbox, &rendered); err != nil {
		t.Fatalf("decode no-listener sing-box config: %v", err)
	}
	if len(rendered.Inbounds) != 0 || len(document.Topology.Inbounds) != 0 {
		t.Fatalf("revoked entry remained observable: rendered=%+v topology=%+v", rendered.Inbounds, document.Topology.Inbounds)
	}
}

func (f *fakeNodeStore) GetDeploymentForAgent(_ context.Context, nodeID int64) (*models.TopologyDeployment, error) {
	f.deploymentForAgentCalls++
	deployment, ok := f.deployments[nodeID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return deployment, nil
}

func TestGetConfigReturnsSharedDocumentBoundToRenderedContent(t *testing.T) {
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{4: {ID: 4, Name: "node-4", Type: "managed"}},
		credentials: map[int64]string{4: "agent-secret-4"},
		inbounds: map[int64][]models.Inbound{4: {{
			ID:         11,
			NodeID:     4,
			Name:       "ss2022",
			Protocol:   "shadowsocks",
			Role:       "entry",
			ListenAddr: "127.0.0.1",
			ListenPort: 8388,
			Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}`),
		}}},
	}
	credentials := []models.UserCredential{{
		UserID: 1, InboundID: 11, Username: "synthetic-user", Protocol: "shadowsocks", Credential: "BBBBBBBBBBBBBBBBBBBBBB==",
	}}
	store.deployments = map[int64]*models.TopologyDeployment{4: explicitDeployment(t, store.nodes[4], store.inbounds[4], credentials)}
	handler := &Handler{Nodes: store, Users: &fakeUserCredentialStore{credentials: credentials}, Deployments: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set(contract.AgentNodeIDHeader, "4")
	req.Header.Set(contract.AgentCredentialHeader, "agent-secret-4")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var document contract.ConfigDocument
	if err := json.Unmarshal(resp.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode shared document: %v", err)
	}
	topology, err := sharedNode(store.nodes[4], store.inbounds[4], []models.UserCredential{{
		UserID: 1, InboundID: 11, Username: "synthetic-user", Protocol: "shadowsocks", Credential: "BBBBBBBBBBBBBBBBBBBBBB==",
	}})
	if err != nil {
		t.Fatalf("build expected topology: %v", err)
	}
	rendered, err := sharedconfig.Render(topology)
	if err != nil {
		t.Fatalf("render expected config: %v", err)
	}
	if !bytes.Equal(document.Singbox, rendered.Content) {
		t.Fatalf("singbox bytes differ from rendered content: got %d bytes, want %d", len(document.Singbox), len(rendered.Content))
	}
	if document.Version != sharedconfig.Hash(document.Singbox) {
		t.Fatalf("document version = %q, want hash of returned singbox %q", document.Version, sharedconfig.Hash(document.Singbox))
	}
	if document.Version != rendered.Version {
		t.Fatalf("document version = %q, want rendered version %q", document.Version, rendered.Version)
	}
	if err := document.Validate(true); err != nil {
		t.Fatalf("shared document validation: %v", err)
	}
	if document.NodeID != 4 || document.Topology.NodeID != 4 {
		t.Fatalf("document node binding = %d/%d, want 4/4", document.NodeID, document.Topology.NodeID)
	}
	if store.listCalls != 0 {
		t.Fatalf("agent read current inbound state %d times after deployment", store.listCalls)
	}
	if store.deploymentForAgentCalls != 1 {
		t.Fatalf("agent deployment snapshot calls = %d, want 1", store.deploymentForAgentCalls)
	}
}

func TestAgentUsesDeployedTopologyAfterCurrentDatabaseMutation(t *testing.T) {
	credentials := []models.UserCredential{{UserID: 1, InboundID: 11, Username: "synthetic-user", Protocol: "shadowsocks", Credential: "BBBBBBBBBBBBBBBBBBBBBB=="}}
	inbounds := []models.Inbound{{ID: 11, NodeID: 4, Name: "ss2022", Protocol: "shadowsocks", Role: "entry", ListenAddr: "127.0.0.1", ListenPort: 8388, Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}`)}}
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{4: {ID: 4, Name: "node-4", Type: "managed"}},
		credentials: map[int64]string{4: "agent-secret-4"},
		inbounds:    map[int64][]models.Inbound{4: inbounds},
		deployments: map[int64]*models.TopologyDeployment{4: explicitDeployment(t, &models.Node{ID: 4, Name: "node-4", Type: "managed"}, inbounds, credentials)},
	}
	handler := &Handler{Nodes: store, Users: &fakeUserCredentialStore{credentials: credentials}, Deployments: store}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
		req.Header.Set(contract.AgentNodeIDHeader, "4")
		req.Header.Set(contract.AgentCredentialHeader, "agent-secret-4")
		response := httptest.NewRecorder()
		handler.GetConfig(response, req)
		return response
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("first config status = %d, body = %s", first.Code, first.Body.String())
	}
	store.inbounds[4][0].ListenPort = 9999
	store.inbounds[4][0].Config = json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"changed-server-psk"}`)
	second := request()
	if second.Code != http.StatusOK {
		t.Fatalf("second config status = %d, body = %s", second.Code, second.Body.String())
	}
	var firstDocument, secondDocument contract.ConfigDocument
	if err := json.Unmarshal(first.Body.Bytes(), &firstDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondDocument); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstDocument.Singbox, secondDocument.Singbox) {
		t.Fatal("current draft/inbound mutation changed the agent-served deployed topology")
	}
	if store.listCalls != 0 {
		t.Fatalf("agent read current inbound state %d times after deployment", store.listCalls)
	}
}

func TestAgentFailsClosedWithoutExplicitDeployment(t *testing.T) {
	store := &fakeNodeStore{nodes: map[int64]*models.Node{1: {ID: 1, Name: "node-1", Type: "managed"}}, credentials: map[int64]string{1: "agent-secret-1"}}
	handler := &Handler{Nodes: store, Deployments: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set(contract.AgentNodeIDHeader, "1")
	req.Header.Set(contract.AgentCredentialHeader, "agent-secret-1")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

func explicitEmptyDeployment(t *testing.T, nodeID int64) *models.TopologyDeployment {
	return explicitDeployment(t, &models.Node{ID: nodeID, Name: "node", Type: "managed"}, nil, nil)
}

func explicitDeployment(t *testing.T, node *models.Node, inbounds []models.Inbound, credentials []models.UserCredential) *models.TopologyDeployment {
	t.Helper()
	topology, err := sharedNode(node, inbounds)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := sharedconfig.RenderRouting(topology)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(topology)
	if err != nil {
		t.Fatal(err)
	}
	return &models.TopologyDeployment{NodeID: node.ID, Version: rendered.Version, SchemaVersion: sharedconfig.SchemaVersion, DraftRevision: 1, Topology: raw, Rendered: rendered.Content}
}

func TestAgentRejectsStoreNodeBindingMismatch(t *testing.T) {
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{4: {ID: 99, Name: "wrong-node", Type: "managed"}},
		credentials: map[int64]string{4: "agent-secret-4"},
	}
	handler := &Handler{Nodes: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set(contract.AgentNodeIDHeader, "4")
	req.Header.Set(contract.AgentCredentialHeader, "agent-secret-4")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusInternalServerError)
	}
}

func TestAgentFailsClosedWhenCurrentInboundExistsWithoutDeployment(t *testing.T) {
	store := &fakeNodeStore{
		nodes:       map[int64]*models.Node{4: {ID: 4, Name: "node-4", Type: "managed"}},
		credentials: map[int64]string{4: "agent-secret-4"},
		inbounds: map[int64][]models.Inbound{4: {{
			ID:         11,
			NodeID:     99,
			Protocol:   "shadowsocks",
			Role:       "entry",
			ListenAddr: "127.0.0.1",
			ListenPort: 8388,
			Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}`),
		}}},
	}
	handler := &Handler{Nodes: store}
	req := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	req.Header.Set(contract.AgentNodeIDHeader, "4")
	req.Header.Set(contract.AgentCredentialHeader, "agent-secret-4")
	resp := httptest.NewRecorder()
	handler.GetConfig(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}
