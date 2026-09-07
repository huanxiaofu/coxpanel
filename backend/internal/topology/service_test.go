package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/shared/config"
)

type serviceNodeStore struct {
	nodes    map[int64]*models.Node
	inbounds map[int64][]models.Inbound
}

func (s *serviceNodeStore) Get(_ context.Context, nodeID int64) (*models.Node, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return node, nil
}

func (s *serviceNodeStore) ListInbounds(_ context.Context, nodeID int64) ([]models.Inbound, error) {
	return append([]models.Inbound(nil), s.inbounds[nodeID]...), nil
}

type serviceDraftStore struct {
	drafts   map[int64]*models.TopologyDraft
	previews map[int64]*repo.TopologyPreview
	deployed map[int64]*models.TopologyDeployment
}

func (s *serviceDraftStore) GetDraft(_ context.Context, nodeID int64) (*models.TopologyDraft, error) {
	draft, ok := s.drafts[nodeID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	copy := *draft
	copy.Edges = append([]models.TopologyEdge(nil), draft.Edges...)
	return &copy, nil
}

func (s *serviceDraftStore) SaveDraft(_ context.Context, nodeID int64, edges []models.TopologyEdge) (*models.TopologyDraft, error) {
	draft := &models.TopologyDraft{NodeID: nodeID, Revision: 1, Edges: append([]models.TopologyEdge(nil), edges...)}
	if old := s.drafts[nodeID]; old != nil {
		draft.Revision = old.Revision + 1
	}
	s.drafts[nodeID] = draft
	return draft, nil
}

func (s *serviceDraftStore) GetPreview(_ context.Context, nodeID int64) (*repo.TopologyPreview, error) {
	preview, ok := s.previews[nodeID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return preview, nil
}

func (s *serviceDraftStore) SavePreview(_ context.Context, preview *repo.TopologyPreview) error {
	s.previews[preview.NodeID] = preview
	return nil
}

func (s *serviceDraftStore) DeployPreview(_ context.Context, deployment *models.TopologyDeployment, version, materialHash string) error {
	preview, ok := s.previews[deployment.NodeID]
	if !ok || preview.Version != version || preview.MaterialHash != materialHash {
		return ErrTopologyPreviewStale
	}
	s.deployed[deployment.NodeID] = deployment
	delete(s.previews, deployment.NodeID)
	return nil
}

func newServiceFixture() (*Service, *serviceDraftStore) {
	nodes := &serviceNodeStore{
		nodes: map[int64]*models.Node{
			1: {ID: 1, Name: "source", Type: "managed", PublicIP: "source.example.invalid"},
			2: {ID: 2, Name: "landing", Type: "external", PublicIP: "landing.example.invalid"},
		},
		inbounds: map[int64][]models.Inbound{
			1: {{ID: 11, NodeID: 1, Name: "entry", Protocol: "shadowsocks", Role: "entry", ListenAddr: "::", ListenPort: 8388, Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"server-psk"}`)}},
			2: {{ID: 22, NodeID: 2, Name: "landing", Protocol: "shadowsocks", Role: "landing", ListenAddr: "::", ListenPort: 443, Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"landing-psk"}`)}},
		},
	}
	drafts := &serviceDraftStore{drafts: make(map[int64]*models.TopologyDraft), previews: make(map[int64]*repo.TopologyPreview), deployed: make(map[int64]*models.TopologyDeployment)}
	return NewService(nodes, drafts), drafts
}

func TestSaveDraftRejectsWrongNodeAndDuplicateSourceWithoutPersistence(t *testing.T) {
	service, drafts := newServiceFixture()
	ctx := context.Background()
	if _, err := service.SaveDraft(ctx, 1, []models.TopologyEdge{{FromInboundID: 22, ToNodeID: 2, ToInboundID: 22}}); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("wrong source node error = %v, want ErrInvalidTopology", err)
	}
	if _, err := service.SaveDraft(ctx, 1, []models.TopologyEdge{
		{FromInboundID: 11, ToNodeID: 2, ToInboundID: 22},
		{FromInboundID: 11, ToNodeID: 2, ToInboundID: 22},
	}); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("duplicate source error = %v, want ErrInvalidTopology", err)
	}
	if len(drafts.drafts) != 0 {
		t.Fatal("invalid draft was persisted")
	}
}

func TestPreviewUsesServerResolvedDestinationAndDeployConsumesPreview(t *testing.T) {
	service, drafts := newServiceFixture()
	ctx := context.Background()
	if _, err := service.SaveDraft(ctx, 1, []models.TopologyEdge{{FromInboundID: 11, ToNodeID: 2, ToInboundID: 22}}); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	preview, err := service.Preview(ctx, 1)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Version == "" || drafts.previews[1] == nil || drafts.previews[1].MaterialHash == "" {
		t.Fatal("preview did not contain server version and material hash")
	}
	if len(preview.Config) == 0 {
		t.Fatal("preview config is empty")
	}
	if got := string(drafts.previews[1].Rendered); !strings.Contains(got, "server-psk") || !strings.Contains(got, "landing-psk") {
		t.Fatalf("stored preview did not retain exact internal render: %s", got)
	}
	storedHash := config.Hash(drafts.previews[1].Rendered)
	if storedHash != preview.Version {
		t.Fatalf("stored preview hash = %q, want version %q", storedHash, preview.Version)
	}
	var redacted config.SingBoxConfig
	if err := json.Unmarshal(preview.Config, &redacted); err != nil {
		t.Fatalf("decode redacted preview: %v", err)
	}
	previewText := string(preview.Config)
	for _, secret := range []string{"server-psk", "landing-psk", "user-psk"} {
		if strings.Contains(previewText, secret) {
			t.Fatalf("preview leaked secret %q: %s", secret, previewText)
		}
	}
	if len(redacted.Inbounds[0].Users) != 0 {
		t.Fatal("preview included the user roster")
	}
	if _, err := service.Deploy(ctx, 1, preview.Version); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if drafts.deployed[1] == nil {
		t.Fatal("deployment snapshot was not persisted")
	}
	if _, err := service.Deploy(ctx, 1, preview.Version); !errors.Is(err, ErrTopologyPreviewStale) {
		t.Fatalf("replayed deploy error = %v, want ErrTopologyPreviewStale", err)
	}
}

func TestPreviewRejectsNestedDestinationDraft(t *testing.T) {
	service, drafts := newServiceFixture()
	drafts.drafts[2] = &models.TopologyDraft{NodeID: 2, Revision: 1, Edges: []models.TopologyEdge{{FromInboundID: 22, ToNodeID: 1, ToInboundID: 11}}}
	if _, err := service.SaveDraft(context.Background(), 1, []models.TopologyEdge{{FromInboundID: 11, ToNodeID: 2, ToInboundID: 22}}); !errors.Is(err, ErrUnsupportedTopology) {
		t.Fatalf("nested draft error = %v, want ErrUnsupportedTopology", err)
	}
}
