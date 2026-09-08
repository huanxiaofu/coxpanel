package topology

import (
	"errors"
	"testing"
	"time"
)

func TestValidateGraphAcceptsThreeLevelChainAndOrdersDownstreamFirst(t *testing.T) {
	graph := Graph{
		Nodes: []GraphNode{
			{NodeID: 1, Inbounds: []GraphInbound{{ID: 101, NodeID: 1, Role: "entry", EgressMode: "chain"}}},
			{NodeID: 2, Inbounds: []GraphInbound{{ID: 201, NodeID: 2, Role: "relay", EgressMode: "chain"}}},
			{NodeID: 3, Inbounds: []GraphInbound{{ID: 301, NodeID: 3, Role: "landing", EgressMode: "direct"}}},
		},
		Edges: []GraphEdge{
			{FromInboundID: 101, ToNodeID: 2, ToInboundID: 201, Weight: 1},
			{FromInboundID: 201, ToNodeID: 3, ToInboundID: 301, Weight: 1},
		},
	}

	result, err := ValidateGraph(graph)
	if err != nil {
		t.Fatalf("ValidateGraph() error = %v", err)
	}
	if result.ChainCount != 1 || result.MaxDepth != 3 {
		t.Fatalf("validation result = %+v, want one depth-three chain", result)
	}
	order, err := DeploymentOrder(graph)
	if err != nil {
		t.Fatalf("DeploymentOrder() error = %v", err)
	}
	want := []int64{3, 2, 1}
	if len(order) != len(want) {
		t.Fatalf("deployment order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("deployment order = %v, want %v", order, want)
		}
	}
}

func TestValidateGraphRejectsCycleDepthAndSameNodeReentry(t *testing.T) {
	tests := []struct {
		name  string
		graph Graph
		want  error
	}{
		{
			name: "cycle",
			graph: Graph{
				Nodes: []GraphNode{
					{NodeID: 1, Inbounds: []GraphInbound{{ID: 101, NodeID: 1, Role: "entry", EgressMode: "chain"}}},
					{NodeID: 2, Inbounds: []GraphInbound{{ID: 201, NodeID: 2, Role: "relay", EgressMode: "chain"}}},
				},
				Edges: []GraphEdge{
					{FromInboundID: 101, ToNodeID: 2, ToInboundID: 201, Weight: 1},
					{FromInboundID: 201, ToNodeID: 2, ToInboundID: 201, Weight: 1},
				},
			},
			want: ErrTopologyCycle,
		},
		{
			name: "same physical node reentry",
			graph: Graph{
				Nodes: []GraphNode{
					{NodeID: 1, Inbounds: []GraphInbound{
						{ID: 101, NodeID: 1, Role: "entry", EgressMode: "chain"},
						{ID: 102, NodeID: 1, Role: "landing", EgressMode: "direct"},
					}},
					{NodeID: 2, Inbounds: []GraphInbound{{ID: 201, NodeID: 2, Role: "relay", EgressMode: "chain"}}},
				},
				Edges: []GraphEdge{
					{FromInboundID: 101, ToNodeID: 2, ToInboundID: 201, Weight: 1},
					{FromInboundID: 201, ToNodeID: 1, ToInboundID: 102, Weight: 1},
				},
			},
			want: ErrSameNodeReentry,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateGraph(test.graph); !errors.Is(err, test.want) {
				t.Fatalf("ValidateGraph() error = %v, want %v", err, test.want)
			}
		})
	}

	deep := Graph{}
	for index := 0; index < 9; index++ {
		nodeID := int64(index + 1)
		inboundID := int64((index + 1) * 100)
		role, mode := "relay", "chain"
		if index == 0 {
			role = "entry"
		} else if index == 8 {
			role, mode = "landing", "direct"
		}
		deep.Nodes = append(deep.Nodes, GraphNode{NodeID: nodeID, Inbounds: []GraphInbound{{ID: inboundID, NodeID: nodeID, Role: role, EgressMode: mode}}})
		if index > 0 {
			deep.Edges = append(deep.Edges, GraphEdge{FromInboundID: int64(index * 100), ToNodeID: nodeID, ToInboundID: inboundID, Weight: 1})
		}
	}
	if _, err := ValidateGraph(deep); !errors.Is(err, ErrChainTooDeep) {
		t.Fatalf("deep graph error = %v, want %v", err, ErrChainTooDeep)
	}
}

func TestValidateDraftGraphAllowsIncompleteChainButPreviewDoesNot(t *testing.T) {
	graph := Graph{
		Nodes: []GraphNode{
			{NodeID: 1, Inbounds: []GraphInbound{{ID: 101, NodeID: 1, Role: "entry", EgressMode: "chain"}}},
			{NodeID: 2, Inbounds: []GraphInbound{{ID: 201, NodeID: 2, Role: "relay", EgressMode: "chain"}}},
		},
		Edges: []GraphEdge{{FromInboundID: 101, ToNodeID: 2, ToInboundID: 201, Weight: 1}},
	}
	if _, err := ValidateDraftGraph(graph); err != nil {
		t.Fatalf("ValidateDraftGraph() error = %v", err)
	}
	if _, err := ValidateGraph(graph); !errors.Is(err, ErrMissingNextHop) {
		t.Fatalf("ValidateGraph() error = %v, want %v", err, ErrMissingNextHop)
	}
}

func TestPreviewFreshRejectsTenMinuteExpiry(t *testing.T) {
	created := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	preview := &Preview{PreviewID: "preview-1", ExpiresAt: created.Add(10 * time.Minute)}
	if err := ValidatePreviewFresh(preview, created.Add(9*time.Minute+59*time.Second)); err != nil {
		t.Fatalf("fresh preview error = %v", err)
	}
	if !errors.Is(ValidatePreviewFresh(preview, created.Add(10*time.Minute)), ErrTopologyPreviewStale) {
		t.Fatal("expired preview was accepted")
	}
}
