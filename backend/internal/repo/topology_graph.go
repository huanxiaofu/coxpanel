package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/coxpanel/backend/internal/models"
	"sort"
)

type GraphInboundMode struct {
	InboundID  int64  `json:"inboundId"`
	EgressMode string `json:"egressMode"`
}
type GraphChange struct {
	NodeID           int64                 `json:"nodeId"`
	ExpectedRevision int64                 `json:"expectedRevision"`
	Edges            []models.TopologyEdge `json:"edges"`
	Layout           json.RawMessage       `json:"layout"`
	InboundModes     []GraphInboundMode    `json:"inboundModes"`
}
type GraphSnapshot struct {
	GraphRevision    int64               `json:"graphRevision"`
	MaterialRevision int64               `json:"materialRevision"`
	Nodes            []GraphSnapshotNode `json:"nodes"`
	DraftValid       bool                `json:"draftValid"`
	Issues           []string            `json:"issues"`
}
type GraphSnapshotNode struct {
	NodeID       int64                 `json:"nodeId"`
	Name         string                `json:"name"`
	Revision     int64                 `json:"revision"`
	Generation   int64                 `json:"generation"`
	Capabilities []string              `json:"capabilities"`
	Edges        []models.TopologyEdge `json:"edges"`
	Layout       json.RawMessage       `json:"layout"`
	Inbounds     []models.Inbound      `json:"inbounds"`
}
type graphQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *TopologyRepo) Graph(ctx context.Context) (*GraphSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	graph, err := readGraph(ctx, tx)
	if err != nil {
		return nil, err
	}
	return graph, tx.Commit()
}
func readGraph(ctx context.Context, query graphQuery) (*GraphSnapshot, error) {
	graph := &GraphSnapshot{Nodes: []GraphSnapshotNode{}, Issues: []string{}}
	if err := query.QueryRowContext(ctx, `SELECT graph_revision,revision FROM topology_material_state WHERE id=TRUE`).Scan(&graph.GraphRevision, &graph.MaterialRevision); err != nil {
		return nil, err
	}
	rows, err := query.QueryContext(ctx, `SELECT n.id,n.name,COALESCE(d.revision,0),n.config_generation,n.agent_capabilities,COALESCE(d.edges,'[]'),COALESCE(d.layout,'{}') FROM nodes n LEFT JOIN topology_drafts d ON d.node_id=n.id WHERE n.type='managed' ORDER BY n.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var node GraphSnapshotNode
		var capabilities, edges []byte
		if err = rows.Scan(&node.NodeID, &node.Name, &node.Revision, &node.Generation, &capabilities, &edges, &node.Layout); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(capabilities, &node.Capabilities); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(edges, &node.Edges); err != nil {
			rows.Close()
			return nil, err
		}
		node.Inbounds = []models.Inbound{}
		graph.Nodes = append(graph.Nodes, node)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for index := range graph.Nodes {
		rows, err = query.QueryContext(ctx, `SELECT id,node_id,name,protocol,role,egress_mode,revision,listen_addr,listen_port FROM inbounds WHERE node_id=$1 ORDER BY id`, graph.Nodes[index].NodeID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var inbound models.Inbound
			if err = rows.Scan(&inbound.ID, &inbound.NodeID, &inbound.Name, &inbound.Protocol, &inbound.Role, &inbound.EgressMode, &inbound.Revision, &inbound.ListenAddr, &inbound.ListenPort); err != nil {
				rows.Close()
				return nil, err
			}
			graph.Nodes[index].Inbounds = append(graph.Nodes[index].Inbounds, inbound)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return graph, nil
}
func (r *TopologyRepo) SaveGraph(ctx context.Context, expected int64, changes []GraphChange, validate func(*GraphSnapshot) (bool, error)) (*GraphSnapshot, error) {
	if expected < 1 || len(changes) == 0 || len(changes) > 2000 {
		return nil, ErrTopologyReleaseConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT graph_revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`).Scan(&revision); err != nil {
		return nil, err
	}
	if revision != expected {
		return nil, ErrTopologyPreviewStale
	}
	changes = append([]GraphChange(nil), changes...)
	sort.Slice(changes, func(left, right int) bool { return changes[left].NodeID < changes[right].NodeID })
	seen := map[int64]bool{}
	for _, change := range changes {
		if seen[change.NodeID] {
			return nil, errors.New("duplicate_graph_change")
		}
		seen[change.NodeID] = true
		if err = lockTopologyNode(ctx, tx, change.NodeID); err != nil {
			return nil, err
		}
		var busy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id WHERE n.node_id=$1 AND r.status IN('preparing','ready','applying','rolling_back','manual_required'))`, change.NodeID).Scan(&busy); err != nil {
			return nil, err
		}
		if busy {
			return nil, ErrTopologyReleaseConflict
		}
		var current int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM topology_drafts WHERE node_id=$1),0)`, change.NodeID).Scan(&current); err != nil {
			return nil, err
		}
		if current != change.ExpectedRevision {
			return nil, ErrTopologyPreviewStale
		}
		if change.Edges == nil {
			change.Edges = []models.TopologyEdge{}
		}
		edges, _ := json.Marshal(change.Edges)
		layout := change.Layout
		if len(layout) == 0 {
			layout = json.RawMessage(`{}`)
		}
		var layoutObject map[string]json.RawMessage
		if len(layout) > 256<<10 || json.Unmarshal(layout, &layoutObject) != nil || layoutObject == nil {
			return nil, errors.New("invalid_graph_layout")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO topology_drafts(node_id,revision,edges,layout) VALUES($1,1,$2,$3) ON CONFLICT(node_id) DO UPDATE SET revision=topology_drafts.revision+1,edges=EXCLUDED.edges,layout=EXCLUDED.layout,updated_at=now()`, change.NodeID, edges, layout); err != nil {
			return nil, err
		}
		for _, mode := range change.InboundModes {
			if mode.EgressMode != "direct" && mode.EgressMode != "chain" {
				return nil, errors.New("invalid_egress_mode")
			}
			result, updateErr := tx.ExecContext(ctx, `UPDATE inbounds SET egress_mode=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND node_id=$2 AND ((role='entry') OR (role='relay' AND $3='chain') OR (role='landing' AND $3='direct'))`, mode.InboundID, change.NodeID, mode.EgressMode)
			if updateErr != nil {
				return nil, updateErr
			}
			count, _ := result.RowsAffected()
			if count != 1 {
				return nil, errors.New("invalid_inbound_mode")
			}
		}
	}
	graph, err := readGraph(ctx, tx)
	if err != nil {
		return nil, err
	}
	valid, err := validate(graph)
	if err != nil {
		return nil, err
	}
	graph.DraftValid = valid
	if _, err = tx.ExecContext(ctx, `UPDATE topology_material_state SET graph_revision=graph_revision+1 WHERE id=TRUE`); err != nil {
		return nil, err
	}
	graph.GraphRevision++
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return graph, nil
}
