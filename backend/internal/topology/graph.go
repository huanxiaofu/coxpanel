package topology

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	MaxGraphInbounds = 2000
	MaxGraphEdges    = 2000
	MinChainInbounds = 2
	MaxChainInbounds = 8
)

var (
	ErrTopologyCycle       = errors.New("topology cycle")
	ErrChainTooDeep        = errors.New("topology chain is too deep")
	ErrSameNodeReentry     = errors.New("topology re-enters a physical node")
	ErrMissingNextHop      = errors.New("topology chain is missing a next hop")
	ErrMissingPreviousHop  = errors.New("topology chain is missing a previous hop")
	ErrDuplicateEdge       = errors.New("topology edge is duplicated")
	ErrDuplicateSource     = errors.New("topology source has multiple next hops")
	ErrInvalidTopologyRole = errors.New("topology inbound role is invalid")
	ErrInvalidEgressMode   = errors.New("topology egress mode is invalid")
	ErrInvalidTopologyEdge = errors.New("topology edge is invalid")
	ErrTopologyTooLarge    = errors.New("topology graph is too large")
)

// Graph is the complete graph visible to the topology validator. Vertices are
// inbounds, while NodeID on a vertex identifies its physical machine.
type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

type GraphNode struct {
	NodeID   int64
	Inbounds []GraphInbound
}

type GraphInbound struct {
	ID         int64
	NodeID     int64
	Role       string
	EgressMode string
}

type GraphEdge struct {
	FromInboundID int64
	ToNodeID      int64
	ToInboundID   int64
	Weight        int
}

type ValidationResult struct {
	ChainCount int
	MaxDepth   int
	InboundIDs []int64
	NodeIDs    []int64
}

type graphVertex struct {
	inbound GraphInbound
	outs    []GraphEdge
	ins     []GraphEdge
}

// ValidateDraftGraph checks structural safety while permitting an editor to
// save a chain which has not yet been connected to its final destination.
func ValidateDraftGraph(graph Graph) (*ValidationResult, error) {
	vertices, outgoing, incoming, err := validateStructure(graph)
	if err != nil {
		return nil, err
	}
	if err := detectCycle(vertices, outgoing); err != nil {
		return nil, err
	}
	if err := detectPhysicalReentry(vertices, outgoing); err != nil {
		return nil, err
	}
	return summarizeDraft(vertices, outgoing, incoming), nil
}

// ValidateGraph is the publish-time validator. Every chain entry and relay
// must resolve to a landing inbound, with no missing hop.
func ValidateGraph(graph Graph) (*ValidationResult, error) {
	result, err := ValidateDraftGraph(graph)
	if err != nil {
		return nil, err
	}
	vertices, outgoing, incoming, err := validateStructure(graph)
	if err != nil {
		return nil, err
	}
	result = summarizeDraft(vertices, outgoing, incoming)
	participating := make(map[int64]struct{})
	for inboundID, vertex := range vertices {
		if vertex.inbound.Role != "entry" && vertex.inbound.Role != "relay" {
			continue
		}
		if vertex.inbound.EgressMode != "chain" {
			continue
		}
		if len(incoming[inboundID]) == 0 && vertex.inbound.Role == "relay" {
			return nil, fmt.Errorf("%w: inbound %d", ErrMissingPreviousHop, inboundID)
		}
		if len(outgoing[inboundID]) == 0 {
			return nil, fmt.Errorf("%w: inbound %d", ErrMissingNextHop, inboundID)
		}
		if err := requireLandingPath(inboundID, vertices, outgoing, participating); err != nil {
			return nil, err
		}
	}
	if len(participating) == 0 {
		return result, nil
	}
	result.InboundIDs = sortedIDs(participating)
	nodes := make(map[int64]struct{})
	for inboundID := range participating {
		nodes[vertices[inboundID].inbound.NodeID] = struct{}{}
	}
	result.NodeIDs = sortedIDs(nodes)
	return result, nil
}

// DeploymentOrder returns unique physical nodes in downstream-first order.
// Nodes which are not part of a complete chain are omitted.
func DeploymentOrder(graph Graph) ([]int64, error) {
	result, err := ValidateGraph(graph)
	if err != nil {
		return nil, err
	}
	vertices, outgoing, _, err := validateStructure(graph)
	if err != nil {
		return nil, err
	}
	distanceByNode := make(map[int64]int)
	for inboundID, vertex := range vertices {
		if vertex.inbound.Role != "entry" || vertex.inbound.EgressMode != "chain" {
			continue
		}
		depth, err := pathDepth(inboundID, vertices, outgoing)
		if err != nil {
			return nil, err
		}
		nodeID := vertex.inbound.NodeID
		distance := depth - 1
		if distance > distanceByNode[nodeID] {
			distanceByNode[nodeID] = distance
		}
		current := inboundID
		for {
			next := outgoing[current]
			if len(next) == 0 {
				break
			}
			target := vertices[next[0].ToInboundID].inbound
			distance--
			if distance > distanceByNode[target.NodeID] {
				distanceByNode[target.NodeID] = distance
			}
			current = target.ID
			if target.Role == "landing" {
				break
			}
		}
	}
	order := make([]int64, 0, len(result.NodeIDs))
	for _, nodeID := range result.NodeIDs {
		order = append(order, nodeID)
	}
	sort.Slice(order, func(left, right int) bool {
		if distanceByNode[order[left]] != distanceByNode[order[right]] {
			return distanceByNode[order[left]] < distanceByNode[order[right]]
		}
		return order[left] < order[right]
	})
	return order, nil
}

func validateStructure(graph Graph) (map[int64]graphVertex, map[int64][]GraphEdge, map[int64][]GraphEdge, error) {
	if len(graph.Edges) > MaxGraphEdges {
		return nil, nil, nil, ErrTopologyTooLarge
	}
	vertices := make(map[int64]graphVertex)
	nodeIDs := make(map[int64]struct{})
	inboundCount := 0
	for _, node := range graph.Nodes {
		if node.NodeID <= 0 {
			return nil, nil, nil, fmt.Errorf("%w: node %d", ErrInvalidTopologyEdge, node.NodeID)
		}
		if _, exists := nodeIDs[node.NodeID]; exists {
			return nil, nil, nil, fmt.Errorf("%w: node %d", ErrInvalidTopologyEdge, node.NodeID)
		}
		nodeIDs[node.NodeID] = struct{}{}
		inboundCount += len(node.Inbounds)
		if inboundCount > MaxGraphInbounds {
			return nil, nil, nil, ErrTopologyTooLarge
		}
		for _, inbound := range node.Inbounds {
			if inbound.ID <= 0 || inbound.NodeID != node.NodeID {
				return nil, nil, nil, fmt.Errorf("%w: inbound %d", ErrInvalidTopologyEdge, inbound.ID)
			}
			if _, exists := vertices[inbound.ID]; exists {
				return nil, nil, nil, fmt.Errorf("%w: inbound %d", ErrInvalidTopologyEdge, inbound.ID)
			}
			if err := validateRoleMode(inbound); err != nil {
				return nil, nil, nil, err
			}
			vertices[inbound.ID] = graphVertex{inbound: inbound}
		}
	}
	outgoing := make(map[int64][]GraphEdge, len(vertices))
	incoming := make(map[int64][]GraphEdge, len(vertices))
	seenEdges := make(map[string]struct{}, len(graph.Edges))
	for _, edge := range graph.Edges {
		if edge.Weight != 1 {
			return nil, nil, nil, fmt.Errorf("%w: edge weight must be one", ErrInvalidTopologyEdge)
		}
		from, ok := vertices[edge.FromInboundID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: source inbound %d", ErrInvalidTopologyEdge, edge.FromInboundID)
		}
		to, ok := vertices[edge.ToInboundID]
		if !ok || to.inbound.NodeID != edge.ToNodeID {
			return nil, nil, nil, fmt.Errorf("%w: destination inbound %d", ErrInvalidTopologyEdge, edge.ToInboundID)
		}
		if edge.FromInboundID == edge.ToInboundID {
			return nil, nil, nil, fmt.Errorf("%w: inbound %d", ErrTopologyCycle, edge.FromInboundID)
		}
		if from.inbound.Role != "entry" && from.inbound.Role != "relay" {
			return nil, nil, nil, fmt.Errorf("%w: source inbound %d", ErrInvalidTopologyRole, edge.FromInboundID)
		}
		if from.inbound.EgressMode != "chain" {
			return nil, nil, nil, fmt.Errorf("%w: source inbound %d", ErrInvalidEgressMode, edge.FromInboundID)
		}
		if to.inbound.Role != "relay" && to.inbound.Role != "landing" {
			return nil, nil, nil, fmt.Errorf("%w: destination inbound %d", ErrInvalidTopologyRole, edge.ToInboundID)
		}
		key := fmt.Sprintf("%d:%d:%d", edge.FromInboundID, edge.ToNodeID, edge.ToInboundID)
		if _, exists := seenEdges[key]; exists {
			return nil, nil, nil, fmt.Errorf("%w: %s", ErrDuplicateEdge, key)
		}
		seenEdges[key] = struct{}{}
		outgoing[edge.FromInboundID] = append(outgoing[edge.FromInboundID], edge)
		incoming[edge.ToInboundID] = append(incoming[edge.ToInboundID], edge)
	}
	for inboundID, edges := range outgoing {
		if len(edges) > 1 {
			return nil, nil, nil, fmt.Errorf("%w: inbound %d", ErrDuplicateSource, inboundID)
		}
	}
	for inboundID, edges := range incoming {
		if vertices[inboundID].inbound.Role == "relay" && len(edges) > 1 {
			return nil, nil, nil, fmt.Errorf("%w: relay inbound %d", ErrDuplicateSource, inboundID)
		}
	}
	return vertices, outgoing, incoming, nil
}

func validateRoleMode(inbound GraphInbound) error {
	switch inbound.Role {
	case "entry":
		if inbound.EgressMode != "direct" && inbound.EgressMode != "chain" {
			return fmt.Errorf("%w: inbound %d", ErrInvalidEgressMode, inbound.ID)
		}
	case "relay":
		if inbound.EgressMode != "chain" {
			return fmt.Errorf("%w: inbound %d", ErrInvalidEgressMode, inbound.ID)
		}
	case "landing":
		if inbound.EgressMode != "direct" {
			return fmt.Errorf("%w: inbound %d", ErrInvalidEgressMode, inbound.ID)
		}
	default:
		return fmt.Errorf("%w: inbound %d", ErrInvalidTopologyRole, inbound.ID)
	}
	return nil
}

func detectCycle(vertices map[int64]graphVertex, outgoing map[int64][]GraphEdge) error {
	colors := make(map[int64]uint8, len(vertices))
	var visit func(int64) error
	visit = func(inboundID int64) error {
		switch colors[inboundID] {
		case 1:
			return fmt.Errorf("%w: inbound %d", ErrTopologyCycle, inboundID)
		case 2:
			return nil
		}
		colors[inboundID] = 1
		for _, edge := range outgoing[inboundID] {
			if _, ok := vertices[edge.ToInboundID]; !ok {
				continue
			}
			if err := visit(edge.ToInboundID); err != nil {
				return err
			}
		}
		colors[inboundID] = 2
		return nil
	}
	ids := make([]int64, 0, len(vertices))
	for inboundID := range vertices {
		ids = append(ids, inboundID)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, inboundID := range ids {
		if err := visit(inboundID); err != nil {
			return err
		}
	}
	return nil
}

func detectPhysicalReentry(vertices map[int64]graphVertex, outgoing map[int64][]GraphEdge) error {
	ids := make([]int64, 0, len(vertices))
	for inboundID := range vertices {
		ids = append(ids, inboundID)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, start := range ids {
		seenNodes := map[int64]struct{}{vertices[start].inbound.NodeID: {}}
		current := start
		for {
			edges := outgoing[current]
			if len(edges) == 0 {
				break
			}
			target, ok := vertices[edges[0].ToInboundID]
			if !ok {
				break
			}
			if _, exists := seenNodes[target.inbound.NodeID]; exists {
				return fmt.Errorf("%w: node %d", ErrSameNodeReentry, target.inbound.NodeID)
			}
			seenNodes[target.inbound.NodeID] = struct{}{}
			current = target.inbound.ID
		}
	}
	return nil
}

func requireLandingPath(start int64, vertices map[int64]graphVertex, outgoing map[int64][]GraphEdge, participating map[int64]struct{}) error {
	depth := 0
	current := start
	for {
		vertex, ok := vertices[current]
		if !ok {
			return fmt.Errorf("%w: inbound %d", ErrInvalidTopologyEdge, current)
		}
		depth++
		if depth > MaxChainInbounds {
			return fmt.Errorf("%w: inbound %d", ErrChainTooDeep, start)
		}
		participating[current] = struct{}{}
		if vertex.inbound.Role == "landing" {
			if depth < MinChainInbounds {
				return fmt.Errorf("%w: chain starting at %d", ErrMissingNextHop, start)
			}
			return nil
		}
		edges := outgoing[current]
		if len(edges) == 0 {
			return fmt.Errorf("%w: inbound %d", ErrMissingNextHop, current)
		}
		current = edges[0].ToInboundID
	}
}

func pathDepth(start int64, vertices map[int64]graphVertex, outgoing map[int64][]GraphEdge) (int, error) {
	depth := 0
	current := start
	seen := make(map[int64]struct{})
	for {
		if _, exists := seen[current]; exists {
			return 0, ErrTopologyCycle
		}
		seen[current] = struct{}{}
		vertex, ok := vertices[current]
		if !ok {
			return 0, ErrInvalidTopologyEdge
		}
		depth++
		if depth > MaxChainInbounds {
			return 0, ErrChainTooDeep
		}
		if vertex.inbound.Role == "landing" {
			return depth, nil
		}
		edges := outgoing[current]
		if len(edges) != 1 {
			return 0, ErrMissingNextHop
		}
		current = edges[0].ToInboundID
	}
}

func summarizeDraft(vertices map[int64]graphVertex, outgoing, incoming map[int64][]GraphEdge) *ValidationResult {
	result := &ValidationResult{}
	participating := make(map[int64]struct{})
	for inboundID, vertex := range vertices {
		if vertex.inbound.Role == "entry" && vertex.inbound.EgressMode == "chain" {
			result.ChainCount++
		}
		if vertex.inbound.Role == "entry" || vertex.inbound.Role == "relay" {
			if len(outgoing[inboundID]) > 0 {
				participating[inboundID] = struct{}{}
			}
		}
		if len(incoming[inboundID]) > 0 {
			participating[inboundID] = struct{}{}
		}
	}
	for inboundID := range participating {
		depth := 1
		current := inboundID
		seen := map[int64]struct{}{}
		for len(outgoing[current]) > 0 {
			if _, exists := seen[current]; exists {
				break
			}
			seen[current] = struct{}{}
			depth++
			current = outgoing[current][0].ToInboundID
		}
		if depth > result.MaxDepth {
			result.MaxDepth = depth
		}
	}
	return result
}

func sortedIDs(values map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}

// ValidatePreviewFresh enforces the ten-minute server-side preview window.
func ValidatePreviewFresh(preview *Preview, now time.Time) error {
	if preview == nil || preview.PreviewID == "" || preview.ExpiresAt.IsZero() {
		return ErrTopologyPreviewStale
	}
	if !now.Before(preview.ExpiresAt) {
		return ErrTopologyPreviewStale
	}
	return nil
}
