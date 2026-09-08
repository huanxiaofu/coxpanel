package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	sharedconfig "github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
	"sort"
	"time"
)

func graphValue(snapshot *repo.GraphSnapshot) Graph {
	graph := Graph{}
	for _, node := range snapshot.Nodes {
		item := GraphNode{NodeID: node.NodeID}
		for _, inbound := range node.Inbounds {
			item.Inbounds = append(item.Inbounds, GraphInbound{ID: inbound.ID, NodeID: node.NodeID, Role: inbound.Role, EgressMode: inbound.EgressMode})
		}
		graph.Nodes = append(graph.Nodes, item)
		for _, edge := range node.Edges {
			graph.Edges = append(graph.Edges, GraphEdge{FromInboundID: edge.FromInboundID, ToNodeID: edge.ToNodeID, ToInboundID: edge.ToInboundID, Weight: 1})
		}
	}
	return graph
}
func (s *Service) Graph(ctx context.Context) (*repo.GraphSnapshot, error) {
	store, ok := s.Drafts.(*repo.TopologyRepo)
	if !ok {
		return nil, ErrInvalidTopology
	}
	graph, err := store.Graph(ctx)
	if err != nil {
		return nil, err
	}
	_, err = ValidateGraph(graphValue(graph))
	graph.DraftValid = err == nil
	if err != nil {
		graph.Issues = append(graph.Issues, err.Error())
	}
	return graph, nil
}
func (s *Service) SaveGraph(ctx context.Context, expected int64, changes []repo.GraphChange) (*repo.GraphSnapshot, error) {
	store, ok := s.Drafts.(*repo.TopologyRepo)
	if !ok {
		return nil, ErrInvalidTopology
	}
	return store.SaveGraph(ctx, expected, changes, func(snapshot *repo.GraphSnapshot) (bool, error) {
		graph := graphValue(snapshot)
		if _, err := ValidateDraftGraph(graph); err != nil {
			return false, fmt.Errorf("%w: %v", ErrInvalidTopology, err)
		}
		_, err := ValidateGraph(graph)
		return err == nil, nil
	})
}

type ChainPreview struct {
	PreviewID     string           `json:"previewId"`
	GraphRevision int64            `json:"graphRevision"`
	ExpiresAt     time.Time        `json:"expiresAt"`
	Dependencies  []int64          `json:"dependencies"`
	Versions      map[int64]string `json:"versions"`
	Configs       []map[string]any `json:"configs"`
	Warnings      []string         `json:"warnings"`
}

func (s *Service) PreviewChain(ctx context.Context, root int64) (*ChainPreview, error) {
	store, ok := s.Drafts.(*repo.TopologyRepo)
	if !ok {
		return nil, ErrInvalidTopology
	}
	snapshot, err := store.Graph(ctx)
	if err != nil {
		return nil, err
	}
	whole := graphValue(snapshot)
	selected := map[int64]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, node := range snapshot.Nodes {
			for _, edge := range node.Edges {
				if selected[node.NodeID] || selected[edge.ToNodeID] {
					if !selected[node.NodeID] || !selected[edge.ToNodeID] {
						changed = true
					}
					selected[node.NodeID] = true
					selected[edge.ToNodeID] = true
				}
			}
		}
	}
	partial := Graph{}
	for _, node := range whole.Nodes {
		if selected[node.NodeID] {
			partial.Nodes = append(partial.Nodes, node)
		}
	}
	for _, edge := range whole.Edges {
		if selected[edge.ToNodeID] {
			partial.Edges = append(partial.Edges, edge)
		}
	}
	if _, err = ValidateGraph(partial); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTopology, err)
	}
	order, err := DeploymentOrder(partial)
	if err != nil {
		return nil, err
	}
	included := map[int64]bool{}
	for _, nodeID := range order {
		included[nodeID] = true
	}
	for _, node := range partial.Nodes {
		if !included[node.NodeID] {
			order = append(order, node.NodeID)
		}
	}
	preview := &repo.TopologyPreviewV2{RootNodeID: root, GraphRevision: snapshot.GraphRevision, MaterialRevision: snapshot.MaterialRevision, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), Candidates: []repo.TopologyCandidate{}}
	result := &ChainPreview{GraphRevision: snapshot.GraphRevision, ExpiresAt: preview.ExpiresAt, Dependencies: order, Versions: map[int64]string{}, Configs: []map[string]any{}, Warnings: []string{"预检不代表公网端到端握手成功"}}
	vector := map[int64]any{}
	for index, nodeID := range order {
		var metadata *repo.GraphSnapshotNode
		for position := range snapshot.Nodes {
			if snapshot.Nodes[position].NodeID == nodeID {
				metadata = &snapshot.Nodes[position]
				break
			}
		}
		if metadata == nil {
			return nil, ErrInvalidTopology
		}
		capable := false
		for _, capability := range metadata.Capabilities {
			if capability == contract.AgentCapabilityTopologyChainV2 {
				capable = true
			}
		}
		if !capable {
			return nil, fmt.Errorf("agent_upgrade_required: node %d", nodeID)
		}
		resolved, resolveErr := s.resolveChainNode(ctx, nodeID, metadata.Edges)
		if resolveErr != nil {
			return nil, resolveErr
		}
		routing, renderErr := sharedconfig.RenderRouting(resolved)
		if renderErr != nil {
			return nil, renderErr
		}
		topologyRaw, _ := json.Marshal(resolved)
		candidate := repo.TopologyCandidate{NodeID: nodeID, Topology: topologyRaw, Rendered: routing.Content, RoutingVersion: routing.Version, ExpectedGeneration: metadata.Generation + 1, PreviousGeneration: metadata.Generation, ApplyOrder: index, DraftRevision: metadata.Revision}
		previous, previousErr := store.GetDeploymentForAgent(ctx, nodeID)
		if previousErr == nil {
			candidate.PreviousTopology = previous.Topology
			candidate.PreviousRendered = previous.Rendered
		} else if !errors.Is(previousErr, sql.ErrNoRows) {
			return nil, previousErr
		}
		preview.Candidates = append(preview.Candidates, candidate)
		vector[nodeID] = map[string]any{"draftRevision": metadata.Revision, "generation": metadata.Generation, "routingVersion": routing.Version}
		redacted, renderErr := sharedconfig.RenderRedacted(resolved)
		if renderErr != nil {
			return nil, renderErr
		}
		result.Configs = append(result.Configs, map[string]any{"nodeId": nodeID, "applyOrder": index, "config": json.RawMessage(redacted.Content)})
		result.Versions[nodeID] = routing.Version
	}
	preview.RevisionVector, _ = json.Marshal(vector)
	if err = store.SavePreviewV2(ctx, preview); err != nil {
		return nil, err
	}
	result.PreviewID = preview.PreviewID
	return result, nil
}
func (s *Service) resolveChainNode(ctx context.Context, nodeID int64, edges []models.TopologyEdge) (sharedconfig.NodeConfig, error) {
	node, err := s.Nodes.Get(ctx, nodeID)
	if err != nil {
		return sharedconfig.NodeConfig{}, err
	}
	inbounds, err := s.Nodes.ListInbounds(ctx, nodeID)
	if err != nil {
		return sharedconfig.NodeConfig{}, err
	}
	result := sharedconfig.NodeConfig{SchemaVersion: sharedconfig.SchemaVersion, NodeID: nodeID, NodeName: node.Name, Inbounds: []sharedconfig.Inbound{}, Edges: []sharedconfig.Edge{}, Outbound: "direct"}
	for _, inbound := range inbounds {
		params, err := stringParams(inbound.Config)
		if err != nil {
			return result, err
		}
		result.Inbounds = append(result.Inbounds, sharedconfig.Inbound{ID: inbound.ID, Name: inbound.Name, Protocol: inbound.Protocol, Role: inbound.Role, Listen: inbound.ListenAddr, Port: inbound.ListenPort, Params: params})
	}
	sort.Slice(edges, func(left, right int) bool { return edges[left].FromInboundID < edges[right].FromInboundID })
	for index, edge := range edges {
		target, err := s.Nodes.Get(ctx, edge.ToNodeID)
		if err != nil || target.Type != "managed" {
			return result, ErrInvalidTopology
		}
		targets, err := s.Nodes.ListInbounds(ctx, edge.ToNodeID)
		if err != nil {
			return result, err
		}
		inbound, found := findInbound(targets, edge.ToInboundID)
		if !found {
			return result, ErrInvalidTopology
		}
		params, err := stringParams(inbound.Config)
		if err != nil {
			return result, err
		}
		server := target.PublicIP
		if server == "" {
			server = target.EasyIP
		}
		if server == "" {
			return result, ErrInvalidTopology
		}
		result.Edges = append(result.Edges, sharedconfig.Edge{ID: int64(index + 1), FromInboundID: edge.FromInboundID, ToNodeID: edge.ToNodeID, ToInboundID: edge.ToInboundID, ToServer: server, ToPort: inbound.ListenPort, ToProtocol: inbound.Protocol, ToParams: params})
	}
	return result, sharedconfig.ValidateTopologyMaterial(result)
}
