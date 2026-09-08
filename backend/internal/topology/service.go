package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	sharedconfig "github.com/coxpanel/shared/config"
)

var (
	ErrInvalidTopology      = errors.New("invalid topology")
	ErrUnsupportedTopology  = errors.New("unsupported topology")
	ErrTopologyPreviewStale = repo.ErrTopologyPreviewStale
)

type NodeStore interface {
	Get(context.Context, int64) (*models.Node, error)
	ListInbounds(context.Context, int64) ([]models.Inbound, error)
}

type DraftStore interface {
	GetDraft(context.Context, int64) (*models.TopologyDraft, error)
	SaveDraft(context.Context, int64, []models.TopologyEdge) (*models.TopologyDraft, error)
	GetPreview(context.Context, int64) (*repo.TopologyPreview, error)
	SavePreview(context.Context, *repo.TopologyPreview) error
	DeployPreview(context.Context, *models.TopologyDeployment, string, string) error
}

type materialRevisionStore interface {
	GetMaterialRevision(context.Context) (int64, error)
}

type materialRevisionDraftStore interface {
	SaveDraftAtMaterialRevision(context.Context, int64, []models.TopologyEdge, int64) (*models.TopologyDraft, error)
}

type Service struct {
	Nodes  NodeStore
	Drafts DraftStore
}

type Preview struct {
	PreviewID string          `json:"previewId,omitempty"`
	NodeID    int64           `json:"nodeId"`
	Version   string          `json:"version"`
	Config    json.RawMessage `json:"config"`
	ExpiresAt time.Time       `json:"expiresAt,omitempty"`
}

func NewService(nodes NodeStore, drafts DraftStore) *Service {
	return &Service{Nodes: nodes, Drafts: drafts}
}

func (s *Service) SaveDraft(ctx context.Context, nodeID int64, edges []models.TopologyEdge) (*models.TopologyDraft, error) {
	if s == nil || s.Nodes == nil || s.Drafts == nil || nodeID <= 0 {
		return nil, ErrInvalidTopology
	}
	if edges == nil {
		edges = []models.TopologyEdge{}
	}
	materialRevision, hasMaterialRevision, err := s.readMaterialRevision(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.resolve(ctx, nodeID, edges); err != nil {
		return nil, err
	}
	if hasMaterialRevision {
		if store, ok := s.Drafts.(materialRevisionDraftStore); ok {
			return store.SaveDraftAtMaterialRevision(ctx, nodeID, edges, materialRevision)
		}
	}
	return s.Drafts.SaveDraft(ctx, nodeID, edges)
}

func (s *Service) Preview(ctx context.Context, nodeID int64) (*Preview, error) {
	if s == nil || s.Drafts == nil || nodeID <= 0 {
		return nil, ErrInvalidTopology
	}
	materialRevision, hasMaterialRevision, err := s.readMaterialRevision(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.currentDraft(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolve(ctx, nodeID, draft.Edges)
	if err != nil {
		return nil, err
	}
	return s.previewResolved(ctx, resolved, draft.Revision, materialRevision, hasMaterialRevision)
}

func (s *Service) Draft(ctx context.Context, nodeID int64) (*models.TopologyDraft, error) {
	if s == nil || s.Drafts == nil || nodeID <= 0 {
		return nil, ErrInvalidTopology
	}
	return s.Drafts.GetDraft(ctx, nodeID)
}

func (s *Service) Deploy(ctx context.Context, nodeID int64, version string) (*models.TopologyDeployment, error) {
	if s == nil || s.Drafts == nil || nodeID <= 0 || strings.TrimSpace(version) == "" {
		return nil, ErrInvalidTopology
	}
	preview, err := s.Drafts.GetPreview(ctx, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTopologyPreviewStale
		}
		return nil, err
	}
	if sharedconfig.Hash(preview.Rendered) != preview.Version || sharedconfig.ValidateRendered(preview.Rendered) != nil {
		return nil, ErrTopologyPreviewStale
	}
	draft, err := s.currentDraft(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if draft.Revision != preview.DraftRevision {
		return nil, ErrTopologyPreviewStale
	}
	resolved, err := s.resolve(ctx, nodeID, draft.Edges)
	if err != nil {
		return nil, err
	}
	materialHash, topologyBytes, err := material(resolved)
	if err != nil {
		return nil, err
	}
	if materialHash != preview.MaterialHash || preview.Version != version {
		return nil, ErrTopologyPreviewStale
	}
	rendered, err := sharedconfig.RenderRouting(resolved.config)
	if err != nil {
		return nil, fmt.Errorf("render topology deployment: %w", err)
	}
	if rendered.Version != preview.Version {
		return nil, ErrTopologyPreviewStale
	}
	deployment := &models.TopologyDeployment{
		NodeID:        nodeID,
		Version:       preview.Version,
		SchemaVersion: rendered.SchemaVersion,
		DraftRevision: draft.Revision,
		Topology:      topologyBytes,
		Rendered:      append([]byte(nil), rendered.Content...),
	}
	if err := s.Drafts.DeployPreview(ctx, deployment, preview.Version, materialHash); err != nil {
		return nil, err
	}
	return deployment, nil
}

type resolvedTopology struct {
	config sharedconfig.NodeConfig
}

func (s *Service) previewResolved(ctx context.Context, resolved *resolvedTopology, revision int64, materialRevision int64, hasMaterialRevision bool) (*Preview, error) {
	if resolved == nil {
		return nil, ErrInvalidTopology
	}
	materialHash, topologyBytes, err := material(resolved)
	if err != nil {
		return nil, err
	}
	fullRendered, err := sharedconfig.RenderRouting(resolved.config)
	if err != nil {
		return nil, fmt.Errorf("render topology preview: %w", err)
	}
	rendered, err := sharedconfig.RenderRedacted(resolved.config)
	if err != nil {
		return nil, fmt.Errorf("render topology preview: %w", err)
	}
	preview := &repo.TopologyPreview{
		NodeID:        resolved.config.NodeID,
		Version:       fullRendered.Version,
		DraftRevision: revision,
		MaterialHash:  materialHash,
		Topology:      topologyBytes,
		Rendered:      append([]byte(nil), fullRendered.Content...),
	}
	if hasMaterialRevision {
		preview.MaterialRevision = materialRevisionPointer(materialRevision)
	}
	if err := s.Drafts.SavePreview(ctx, preview); err != nil {
		return nil, err
	}
	return &Preview{NodeID: preview.NodeID, Version: preview.Version, Config: append(json.RawMessage(nil), rendered.Content...)}, nil
}

func (s *Service) readMaterialRevision(ctx context.Context) (int64, bool, error) {
	store, ok := s.Drafts.(materialRevisionStore)
	if !ok {
		return 0, false, nil
	}
	revision, err := store.GetMaterialRevision(ctx)
	return revision, true, err
}

func materialRevisionPointer(value int64) *int64 {
	return &value
}

func (s *Service) currentDraft(ctx context.Context, nodeID int64) (*models.TopologyDraft, error) {
	draft, err := s.Drafts.GetDraft(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return &models.TopologyDraft{NodeID: nodeID, Revision: 0, Edges: []models.TopologyEdge{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if draft == nil || draft.NodeID != nodeID || draft.Revision < 1 {
		return nil, ErrInvalidTopology
	}
	if draft.Edges == nil {
		draft.Edges = []models.TopologyEdge{}
	}
	return draft, nil
}

func (s *Service) resolve(ctx context.Context, nodeID int64, edges []models.TopologyEdge) (*resolvedTopology, error) {
	if s == nil || s.Nodes == nil || nodeID <= 0 {
		return nil, ErrInvalidTopology
	}
	node, err := s.Nodes.Get(ctx, nodeID)
	if err != nil || node == nil || node.ID != nodeID {
		return nil, ErrInvalidTopology
	}
	inbounds, err := s.Nodes.ListInbounds(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	seenInboundIDs := make(map[int64]struct{}, len(inbounds))
	sharedInbounds := make([]sharedconfig.Inbound, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound.ID <= 0 || inbound.NodeID != nodeID {
			return nil, ErrInvalidTopology
		}
		if _, exists := seenInboundIDs[inbound.ID]; exists {
			return nil, ErrInvalidTopology
		}
		seenInboundIDs[inbound.ID] = struct{}{}
		params, err := stringParams(inbound.Config)
		if err != nil {
			return nil, fmt.Errorf("inbound %d: %w", inbound.ID, err)
		}
		sharedInbounds = append(sharedInbounds, sharedconfig.Inbound{
			ID: inbound.ID, Name: inbound.Name, Protocol: inbound.Protocol, Role: inbound.Role,
			Listen: inbound.ListenAddr, Port: inbound.ListenPort, Params: params,
		})
	}
	sort.Slice(sharedInbounds, func(i, j int) bool { return sharedInbounds[i].ID < sharedInbounds[j].ID })
	sharedEdges := make([]sharedconfig.Edge, 0, len(edges))
	seenSources := make(map[int64]struct{}, len(edges))
	for index, edge := range edges {
		if edge.FromInboundID <= 0 || edge.ToNodeID <= 0 || edge.ToInboundID <= 0 {
			return nil, ErrInvalidTopology
		}
		from, exists := findInbound(inbounds, edge.FromInboundID)
		if !exists || from.NodeID != nodeID || from.Role != "entry" {
			return nil, ErrInvalidTopology
		}
		if _, duplicate := seenSources[edge.FromInboundID]; duplicate {
			return nil, ErrInvalidTopology
		}
		seenSources[edge.FromInboundID] = struct{}{}
		if edge.ToNodeID == nodeID {
			return nil, ErrUnsupportedTopology
		}
		targetNode, err := s.Nodes.Get(ctx, edge.ToNodeID)
		if err != nil || targetNode == nil || targetNode.ID != edge.ToNodeID {
			return nil, ErrInvalidTopology
		}
		targets, err := s.Nodes.ListInbounds(ctx, edge.ToNodeID)
		if err != nil {
			return nil, err
		}
		to, exists := findInbound(targets, edge.ToInboundID)
		if !exists || to.NodeID != edge.ToNodeID || (to.Role != "landing" && to.Role != "relay") {
			return nil, ErrInvalidTopology
		}
		targetDraft, err := s.Drafts.GetDraft(ctx, edge.ToNodeID)
		if err == nil && targetDraft != nil && len(targetDraft.Edges) > 0 {
			return nil, ErrUnsupportedTopology
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		toParams, err := stringParams(to.Config)
		if err != nil {
			return nil, fmt.Errorf("destination inbound %d: %w", to.ID, err)
		}
		server := strings.TrimSpace(targetNode.PublicIP)
		if server == "" {
			server = strings.TrimSpace(targetNode.EasyIP)
		}
		if server == "" {
			extParams, extErr := stringParams(targetNode.ExtParams)
			if extErr != nil {
				return nil, ErrInvalidTopology
			}
			server = strings.TrimSpace(extParams["server"])
		}
		if server == "" {
			return nil, ErrInvalidTopology
		}
		sharedEdges = append(sharedEdges, sharedconfig.Edge{
			ID: int64(index + 1), FromInboundID: edge.FromInboundID,
			ToNodeID: edge.ToNodeID, ToInboundID: edge.ToInboundID,
			ToServer: server, ToPort: to.ListenPort, ToProtocol: to.Protocol, ToParams: toParams,
		})
	}
	config := sharedconfig.NodeConfig{
		SchemaVersion: sharedconfig.SchemaVersion,
		NodeID:        node.ID,
		NodeName:      node.Name,
		Inbounds:      sharedInbounds,
		Edges:         sharedEdges,
		Outbound:      "direct",
	}
	if err := sharedconfig.ValidateTopologyMaterial(config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTopology, err)
	}
	return &resolvedTopology{config: config}, nil
}

func material(resolved *resolvedTopology) (string, []byte, error) {
	if resolved == nil {
		return "", nil, ErrInvalidTopology
	}
	canonical := resolved.config
	canonical.Version = ""
	canonical.UpdatedAt = canonical.UpdatedAt.UTC()
	canonical.Credentials = nil
	bytes, err := json.Marshal(canonical)
	if err != nil {
		return "", nil, err
	}
	return sharedconfig.Hash(bytes), bytes, nil
}

func findInbound(inbounds []models.Inbound, id int64) (models.Inbound, bool) {
	for _, inbound := range inbounds {
		if inbound.ID == id {
			return inbound, true
		}
	}
	return models.Inbound{}, false
}

func stringParams(raw json.RawMessage) (map[string]string, error) {
	params := make(map[string]string)
	if len(raw) == 0 || string(raw) == "null" {
		return params, nil
	}
	var values map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, ErrInvalidTopology
	}
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			params[key] = typed
		case json.Number:
			params[key] = typed.String()
		case bool:
			params[key] = fmt.Sprintf("%t", typed)
		default:
			return nil, fmt.Errorf("parameter %q must be scalar", key)
		}
	}
	return params, nil
}
