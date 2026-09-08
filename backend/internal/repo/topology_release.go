package repo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	sharedconfig "github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
	"sort"
	"time"
)

func (r *TopologyRepo) sealCandidate(ctx context.Context, releaseID, nodeID int64, purpose string, candidate TopologyCandidate) ([]byte, error) {
	raw, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	sealed, err := r.sealer.Seal(ctx, fmt.Sprintf("topology_release_nodes:%d:%d:%s", releaseID, nodeID, purpose), raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"ciphertext": base64.RawStdEncoding.EncodeToString(sealed), "version": "v1"})
}
func (r *TopologyRepo) openCandidate(ctx context.Context, releaseID, nodeID int64, raw []byte) (TopologyCandidate, error) {
	var candidate TopologyCandidate
	var envelope map[string]string
	if json.Unmarshal(raw, &envelope) != nil || envelope["version"] != "v1" {
		return candidate, ErrTopologySecureStorageUnavailable
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope["ciphertext"])
	if err != nil {
		return candidate, ErrTopologySecureStorageUnavailable
	}
	plaintext, err := r.sealer.Open(ctx, fmt.Sprintf("topology_release_nodes:%d:%d:candidate", releaseID, nodeID), ciphertext)
	if err != nil {
		return candidate, ErrTopologySecureStorageUnavailable
	}
	if json.Unmarshal(plaintext, &candidate) != nil || candidate.NodeID != nodeID {
		return candidate, ErrTopologySecureStorageUnavailable
	}
	return candidate, nil
}

func (r *TopologyRepo) CreateTopologyRelease(ctx context.Context, previewID string, root, actorID int64) (*TopologyRelease, error) {
	if _, err := r.snapshotSealer(); err != nil {
		return nil, err
	}
	preview, err := r.GetPreviewV2(ctx, root)
	if err != nil {
		return nil, err
	}
	if preview.PreviewID != previewID {
		return nil, ErrTopologyPreviewStale
	}
	for _, candidate := range preview.Candidates {
		if err = NewUserRepo(r.db).ReconcileUserCredentialsForNode(ctx, candidate.NodeID); err != nil {
			return nil, err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var graphRevision, materialRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT graph_revision,revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`).Scan(&graphRevision, &materialRevision); err != nil {
		return nil, err
	}
	if graphRevision != preview.GraphRevision || materialRevision != preview.MaterialRevision {
		return nil, ErrTopologyPreviewStale
	}
	var currentID string
	var expiry time.Time
	if err = tx.QueryRowContext(ctx, `SELECT preview_id::text,expires_at FROM topology_previews WHERE node_id=$1 FOR UPDATE`, root).Scan(&currentID, &expiry); err != nil {
		return nil, err
	}
	if currentID != previewID || !time.Now().Before(expiry) {
		return nil, ErrTopologyPreviewStale
	}
	ordered := append([]TopologyCandidate(nil), preview.Candidates...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].NodeID < ordered[right].NodeID })
	for _, candidate := range ordered {
		var generation int64
		if err = tx.QueryRowContext(ctx, `SELECT config_generation FROM nodes WHERE id=$1 AND agent_capabilities ? 'topology-chain-v2' FOR UPDATE`, candidate.NodeID).Scan(&generation); err != nil {
			return nil, ErrTopologyReleaseConflict
		}
		if generation != candidate.PreviousGeneration {
			return nil, ErrTopologyPreviewStale
		}
		var busy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id WHERE n.node_id=$1 AND r.status IN('preparing','ready','applying','rolling_back','manual_required'))`, candidate.NodeID).Scan(&busy); err != nil {
			return nil, err
		}
		if busy {
			return nil, ErrTopologyReleaseConflict
		}
	}
	release := &TopologyRelease{RootNodeID: root, GraphRevision: graphRevision, MaterialRevision: materialRevision, RevisionVector: preview.RevisionVector, Status: "preparing", CreatedBy: actorID}
	if err = tx.QueryRowContext(ctx, `INSERT INTO topology_releases(root_node_id,graph_revision,material_revision,revision_vector,status,created_by) VALUES($1,$2,$3,$4,'preparing',$5) RETURNING id,created_at`, root, graphRevision, materialRevision, preview.RevisionVector, actorID).Scan(&release.ID, &release.CreatedAt); err != nil {
		return nil, err
	}
	for _, candidate := range preview.Candidates {
		document, renderErr := r.runtimeDocument(ctx, tx, candidate, false)
		if renderErr != nil {
			return nil, renderErr
		}
		candidate.ExpectedRuntime = document.Version
		sealed, sealErr := r.sealCandidate(ctx, release.ID, candidate.NodeID, "candidate", candidate)
		if sealErr != nil {
			return nil, sealErr
		}
		var previous any
		if len(candidate.PreviousTopology) > 0 {
			previous, sealErr = r.sealCandidate(ctx, release.ID, candidate.NodeID, "previous", TopologyCandidate{NodeID: candidate.NodeID, Topology: candidate.PreviousTopology, Rendered: candidate.PreviousRendered})
			if sealErr != nil {
				return nil, sealErr
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO topology_release_nodes(release_id,node_id,phase,candidate_topology,previous_topology,routing_version,expected_runtime_version,expected_generation,previous_generation,apply_order,status,last_ack_at) VALUES($1,$2,'prepare',$3,$4,$5,$6,$7,$8,$9,'pending',now())`, release.ID, candidate.NodeID, sealed, previous, candidate.RoutingVersion, candidate.ExpectedRuntime, candidate.ExpectedGeneration, candidate.PreviousGeneration, candidate.ApplyOrder); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET config_generation=$2 WHERE id=$1`, candidate.NodeID, candidate.ExpectedGeneration); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE topology_previews SET expires_at=now() WHERE node_id=$1`, root); err != nil {
		return nil, err
	}
	return release, tx.Commit()
}

func (r *TopologyRepo) runtimeDocument(ctx context.Context, query graphQuery, candidate TopologyCandidate, rollback bool) (contract.ConfigDocument, error) {
	var node sharedconfig.NodeConfig
	raw := candidate.Topology
	if rollback {
		raw = candidate.PreviousTopology
	}
	if len(raw) == 0 {
		node = sharedconfig.NodeConfig{SchemaVersion: sharedconfig.SchemaVersion, NodeID: candidate.NodeID, Inbounds: []sharedconfig.Inbound{}, Edges: []sharedconfig.Edge{}, Outbound: "direct"}
	} else if json.Unmarshal(raw, &node) != nil {
		return contract.ConfigDocument{}, ErrTopologyReleaseConflict
	}
	node.Credentials = nil
	rows, err := query.QueryContext(ctx, `SELECT uc.user_id,uc.inbound_id,u.username,i.protocol,uc.credential FROM user_credentials uc JOIN users u ON u.id=uc.user_id JOIN inbounds i ON i.id=uc.inbound_id WHERE i.node_id=$1 AND i.role='entry' AND u.is_active AND (u.expire_at IS NULL OR u.expire_at>now()) AND EXISTS(SELECT 1 FROM user_node_groups ug JOIN node_group_members ng ON ng.group_id=ug.group_id WHERE ug.user_id=u.id AND ng.node_id=i.node_id) ORDER BY uc.inbound_id,uc.user_id`, candidate.NodeID)
	if err != nil {
		return contract.ConfigDocument{}, err
	}
	for rows.Next() {
		var credential sharedconfig.UserCredential
		var value string
		if err = rows.Scan(&credential.UserID, &credential.InboundID, &credential.Name, &credential.Protocol, &value); err != nil {
			rows.Close()
			return contract.ConfigDocument{}, err
		}
		if credential.Protocol == "vless-reality" {
			credential.UUID = value
		} else {
			credential.Password = value
		}
		for _, inbound := range node.Inbounds {
			if inbound.ID == credential.InboundID && inbound.Role == "entry" && inbound.Protocol == credential.Protocol {
				node.Credentials = append(node.Credentials, credential)
				break
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return contract.ConfigDocument{}, err
	}
	runtime, err := sharedconfig.RuntimeNode(node)
	if err != nil {
		return contract.ConfigDocument{}, err
	}
	runtime.TrafficStatsListen = r.StatsListen
	rendered, err := sharedconfig.RenderRuntime(runtime)
	if err != nil {
		return contract.ConfigDocument{}, err
	}
	return contract.NewConfigDocument(runtime, *rendered, true), nil
}

func (r *TopologyRepo) GetAgentDeployment(ctx context.Context, nodeID, releaseID int64, phase string) (*contract.TopologyDeploymentDocument, error) {
	if _, err := r.snapshotSealer(); err != nil {
		return nil, err
	}
	var raw []byte
	var status, expectedRuntime, routing string
	var generation, currentGeneration int64
	err := r.db.QueryRowContext(ctx, `SELECT n.candidate_topology,r.status,COALESCE(n.expected_runtime_version,''),n.routing_version,n.expected_generation,x.config_generation FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id JOIN nodes x ON x.id=n.node_id WHERE n.release_id=$1 AND n.node_id=$2 AND n.phase=$3 AND n.status IN('pending','prepared','active')`, releaseID, nodeID, phase).Scan(&raw, &status, &expectedRuntime, &routing, &generation, &currentGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTopologyReleaseNotFound
	}
	if err != nil {
		return nil, err
	}
	if generation != currentGeneration {
		return nil, ErrTopologyGenerationStale
	}
	if status != "preparing" && status != "applying" && status != "rolling_back" && status != "ready" {
		return nil, ErrTopologyReleaseConflict
	}
	candidate, err := r.openCandidate(ctx, releaseID, nodeID, raw)
	if err != nil {
		return nil, err
	}
	document, err := r.runtimeDocument(ctx, r.db, candidate, phase == "rollback")
	if err != nil {
		return nil, err
	}
	if expectedRuntime != "" && expectedRuntime != document.Version {
		_, _ = r.db.ExecContext(ctx, `UPDATE topology_releases SET status='manual_required',error_code='runtime_material_changed',finished_at=now() WHERE id=$1`, releaseID)
		_, _ = r.db.ExecContext(ctx, `UPDATE nodes SET config_generation=config_generation+1 WHERE id=$1 AND config_generation=$2`, nodeID, generation)
		return nil, ErrTopologyGenerationStale
	}
	return &contract.TopologyDeploymentDocument{ConfigDocument: document, ReleaseID: releaseID, Generation: generation, Phase: phase, RoutingVersion: routing}, nil
}

type PendingDeployment struct {
	ReleaseID  int64  `json:"releaseId"`
	Phase      string `json:"phase"`
	Generation int64  `json:"generation"`
}

func (r *TopologyRepo) GetPendingAgentDeployment(ctx context.Context, nodeID int64) (*contract.PendingDeployment, error) {
	pending, err := r.PendingDeployment(ctx, nodeID)
	if err != nil || pending == nil {
		return nil, err
	}
	return &contract.PendingDeployment{ReleaseID: pending.ReleaseID, Phase: pending.Phase, Generation: pending.Generation}, nil
}

func (r *TopologyRepo) PendingDeployment(ctx context.Context, nodeID int64) (*PendingDeployment, error) {
	var pending PendingDeployment
	err := r.db.QueryRowContext(ctx, `SELECT n.release_id,n.phase,n.expected_generation FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id WHERE n.node_id=$1 AND ((r.status='preparing' AND n.phase='prepare' AND n.status='pending') OR (r.status='applying' AND n.phase='apply' AND n.status='active') OR (r.status='rolling_back' AND n.phase='rollback' AND n.status='active')) ORDER BY r.id DESC LIMIT 1`, nodeID).Scan(&pending.ReleaseID, &pending.Phase, &pending.Generation)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &pending, err
}

func (r *TopologyRepo) AcknowledgeAgentDeployment(ctx context.Context, ack *contract.DeploymentAck) error {
	if ack == nil || ack.Validate() != nil {
		return ErrTopologyReleaseConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var releaseStatus string
	if _, err = lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT status FROM topology_releases WHERE id=$1 FOR UPDATE`, ack.ReleaseID).Scan(&releaseStatus); err != nil {
		return err
	}
	var generation int64
	var phase, status, expected string
	if err = tx.QueryRowContext(ctx, `SELECT expected_generation,phase,status,COALESCE(expected_runtime_version,'') FROM topology_release_nodes WHERE release_id=$1 AND node_id=$2 FOR UPDATE`, ack.ReleaseID, ack.NodeID).Scan(&generation, &phase, &status, &expected); err != nil {
		return err
	}
	if generation != ack.Generation {
		return ErrTopologyGenerationStale
	}
	if expected != "" && ack.RuntimeVersion != expected && ack.Status != "failed" {
		return ErrTopologyReleaseConflict
	}
	if status == ack.Status {
		return tx.Commit()
	}
	if (phase == "apply" && ack.Phase == "prepare" && ack.Status == "prepared") || (phase == "rollback" && ack.Phase == "apply" && ack.Status == "applied") {
		return tx.Commit()
	}
	if phase != ack.Phase {
		return ErrTopologyReleaseConflict
	}
	if releaseStatus != "preparing" && releaseStatus != "applying" && releaseStatus != "rolling_back" {
		return ErrTopologyReleaseConflict
	}
	if phase != "prepare" && status != "active" {
		return ErrTopologyReleaseConflict
	}
	var currentGeneration int64
	if err = tx.QueryRowContext(ctx, `SELECT config_generation FROM nodes WHERE id=$1 FOR UPDATE`, ack.NodeID).Scan(&currentGeneration); err != nil {
		return err
	}
	if currentGeneration != generation {
		return ErrTopologyGenerationStale
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT candidate_topology FROM topology_release_nodes WHERE release_id=$1 AND node_id=$2`, ack.ReleaseID, ack.NodeID).Scan(&raw); err != nil {
		return err
	}
	candidate, err := r.openCandidate(ctx, ack.ReleaseID, ack.NodeID, raw)
	if err != nil {
		return err
	}
	current, err := r.runtimeDocument(ctx, tx, candidate, phase == "rollback")
	if err != nil {
		return err
	}
	if current.Version != expected {
		if _, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status='manual_required',error_code='runtime_material_changed',finished_at=now() WHERE id=$1`, ack.ReleaseID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET config_generation=config_generation+1 WHERE id IN(SELECT node_id FROM topology_release_nodes WHERE release_id=$1)`, ack.ReleaseID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return ErrTopologyGenerationStale
	}
	expectedStatus := map[string]string{"prepare": "prepared", "apply": "applied", "rollback": "rolled_back"}[phase]
	if ack.Status != expectedStatus && ack.Status != "failed" {
		return ErrTopologyReleaseConflict
	}
	errorCode := any(nil)
	if ack.Status == "failed" {
		errorCode = "agent_" + phase + "_failed"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE topology_release_nodes SET status=$3,last_ack_at=now(),attempts=attempts+1,error_code=$4 WHERE release_id=$1 AND node_id=$2`, ack.ReleaseID, ack.NodeID, ack.Status, errorCode); err != nil {
		return err
	}
	if ack.Status == "failed" {
		if phase == "prepare" {
			_, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status='failed',finished_at=now(),error_code='prepare_failed' WHERE id=$1`, ack.ReleaseID)
		} else if phase == "rollback" {
			_, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status='manual_required',finished_at=now(),error_code='rollback_failed' WHERE id=$1`, ack.ReleaseID)
		} else {
			err = r.beginRollback(ctx, tx, ack.ReleaseID)
		}
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if phase == "prepare" {
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM topology_release_nodes WHERE release_id=$1 AND status<>'prepared'`, ack.ReleaseID).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status='ready' WHERE id=$1`, ack.ReleaseID); err != nil {
				return err
			}
			err = r.activateNext(ctx, tx, ack.ReleaseID, false)
		}
	} else {
		err = r.activateNext(ctx, tx, ack.ReleaseID, phase == "rollback")
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TopologyRepo) activateNext(ctx context.Context, tx *sql.Tx, releaseID int64, rollback bool) error {
	status, phase, order := "prepared", "apply", "ASC"
	if rollback {
		status, phase, order = "rollback_pending", "rollback", "DESC"
	}
	var nodeID int64
	var raw []byte
	var generation int64
	err := tx.QueryRowContext(ctx, `SELECT node_id,candidate_topology,expected_generation FROM topology_release_nodes WHERE release_id=$1 AND status=$2 ORDER BY apply_order `+order+` LIMIT 1 FOR UPDATE`, releaseID, status).Scan(&nodeID, &raw, &generation)
	if err == sql.ErrNoRows {
		terminal := "succeeded"
		if rollback {
			terminal = "rolled_back"
		}
		_, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status=$2,finished_at=now() WHERE id=$1`, releaseID, terminal)
		return err
	}
	if err != nil {
		return err
	}
	candidate, err := r.openCandidate(ctx, releaseID, nodeID, raw)
	if err != nil {
		return err
	}
	if rollback {
		generation++
		candidate.ExpectedGeneration = generation
	}
	document, err := r.runtimeDocument(ctx, tx, candidate, rollback)
	if err != nil {
		return err
	}
	topologyRaw, rendered := candidate.Topology, candidate.Rendered
	if rollback {
		topologyRaw, rendered = candidate.PreviousTopology, candidate.PreviousRendered
		if len(topologyRaw) == 0 {
			node := sharedconfig.NodeConfig{SchemaVersion: sharedconfig.SchemaVersion, NodeID: nodeID, Inbounds: []sharedconfig.Inbound{}, Edges: []sharedconfig.Edge{}, Outbound: "direct"}
			topologyRaw, _ = json.Marshal(node)
			routing, renderErr := sharedconfig.RenderRouting(node)
			if renderErr != nil {
				return renderErr
			}
			rendered = routing.Content
		}
	}
	if !rollback && document.Version != candidate.ExpectedRuntime {
		return ErrTopologyGenerationStale
	}
	routingVersion := sharedconfig.Hash(rendered)
	if _, err = tx.ExecContext(ctx, `INSERT INTO topology_deployments(node_id,version,schema_version,draft_revision,topology,rendered,release_id,generation) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(node_id) DO UPDATE SET version=EXCLUDED.version,schema_version=EXCLUDED.schema_version,draft_revision=EXCLUDED.draft_revision,topology=EXCLUDED.topology,rendered=EXCLUDED.rendered,release_id=EXCLUDED.release_id,generation=EXCLUDED.generation,deployed_at=now()`, nodeID, routingVersion, sharedconfig.SchemaVersion, candidate.DraftRevision, topologyRaw, rendered, releaseID, generation); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET config_generation=$2 WHERE id=$1`, nodeID, generation); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE topology_release_nodes SET phase=$3,status='active',expected_generation=$4,expected_runtime_version=$5,routing_version=$6,last_ack_at=now() WHERE release_id=$1 AND node_id=$2`, releaseID, nodeID, phase, generation, document.Version, routingVersion); err != nil {
		return err
	}
	releaseState := "applying"
	if rollback {
		releaseState = "rolling_back"
	}
	_, err = tx.ExecContext(ctx, `UPDATE topology_releases SET status=$2,finished_at=NULL WHERE id=$1`, releaseID, releaseState)
	return err
}
func (r *TopologyRepo) beginRollback(ctx context.Context, tx *sql.Tx, releaseID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE topology_release_nodes SET status='rollback_pending' WHERE release_id=$1 AND phase='apply' AND status IN('active','applied','failed')`, releaseID); err != nil {
		return err
	}
	return r.activateNext(ctx, tx, releaseID, true)
}
func (r *TopologyRepo) RollbackRelease(ctx context.Context, releaseID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM topology_releases WHERE id=$1 FOR UPDATE`, releaseID).Scan(&status); err != nil {
		return err
	}
	if status == "rolling_back" || status == "rolled_back" {
		return nil
	}
	if status != "failed" && status != "manual_required" {
		return ErrTopologyReleaseConflict
	}
	if err = r.beginRollback(ctx, tx, releaseID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *TopologyRepo) GetRelease(ctx context.Context, id int64) (map[string]any, error) {
	var release TopologyRelease
	err := r.db.QueryRowContext(ctx, `SELECT id,root_node_id,graph_revision,material_revision,status,created_by,COALESCE(error_code,''),created_at,finished_at FROM topology_releases WHERE id=$1`, id).Scan(&release.ID, &release.RootNodeID, &release.GraphRevision, &release.MaterialRevision, &release.Status, &release.CreatedBy, &release.ErrorCode, &release.CreatedAt, &release.FinishedAt)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT node_id,phase,routing_version,COALESCE(expected_runtime_version,''),expected_generation,apply_order,status,attempts,last_ack_at,COALESCE(error_code,'') FROM topology_release_nodes WHERE release_id=$1 ORDER BY apply_order`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []TopologyReleaseNode{}
	for rows.Next() {
		node := TopologyReleaseNode{ReleaseID: id}
		if err = rows.Scan(&node.NodeID, &node.Phase, &node.RoutingVersion, &node.ExpectedRuntime, &node.ExpectedGeneration, &node.ApplyOrder, &node.Status, &node.Attempts, &node.LastAckAt, &node.ErrorCode); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return map[string]any{"id": id, "releaseId": id, "status": release.Status, "rootNodeId": release.RootNodeID, "createdAt": release.CreatedAt, "finishedAt": release.FinishedAt, "errorCode": release.ErrorCode, "nodes": nodes}, rows.Err()
}
func (r *TopologyRepo) RecoverReleases(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE topology_releases r SET status=CASE WHEN status='preparing' THEN 'failed' ELSE 'manual_required' END,error_code='release_timeout',finished_at=now() WHERE status IN('preparing','ready','applying','rolling_back') AND (created_at<now()-interval '15 minutes' OR EXISTS(SELECT 1 FROM topology_release_nodes n WHERE n.release_id=r.id AND n.status IN('pending','active') AND COALESCE(n.last_ack_at,r.created_at)<now()-interval '120 seconds'))`)
	return err
}
func (r *TopologyRepo) RunReleases(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	nextCleanup := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		_ = r.RecoverReleases(ctx)
		if time.Now().After(nextCleanup) {
			_ = r.PruneReleaseHistory(ctx)
			nextCleanup = time.Now().Add(time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
