package repo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"github.com/coxpanel/backend/internal/models"
	sharedconfig "github.com/coxpanel/shared/config"
)

// TopologyRepo persists drafts and explicit deployment snapshots.
type TopologyRepo struct{ db *sql.DB }

func NewTopologyRepo(db *sql.DB) *TopologyRepo { return &TopologyRepo{db: db} }

// TopologyPreview is the server-owned material produced by the preview step.
// Rendered contains the exact internal bytes used for deployment; the
// service returns a separate redacted representation to administrators.
type TopologyPreview struct {
	NodeID           int64
	Version          string
	DraftRevision    int64
	MaterialRevision *int64
	MaterialHash     string
	Topology         json.RawMessage
	Rendered         []byte
}

func (r *TopologyRepo) GetDraft(ctx context.Context, nodeID int64) (*models.TopologyDraft, error) {
	var draft models.TopologyDraft
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, revision, edges, updated_at
		FROM topology_drafts WHERE node_id=$1`, nodeID).
		Scan(&draft.NodeID, &draft.Revision, &raw, &draft.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &draft.Edges); err != nil {
		return nil, err
	}
	if draft.Edges == nil {
		draft.Edges = make([]models.TopologyEdge, 0)
	}
	return &draft, nil
}

func (r *TopologyRepo) SaveDraft(ctx context.Context, nodeID int64, edges []models.TopologyEdge) (*models.TopologyDraft, error) {
	return r.saveDraft(ctx, nodeID, edges, nil)
}

func (r *TopologyRepo) SaveDraftAtMaterialRevision(ctx context.Context, nodeID int64, edges []models.TopologyEdge, expectedMaterialRevision int64) (*models.TopologyDraft, error) {
	return r.saveDraft(ctx, nodeID, edges, &expectedMaterialRevision)
}

func (r *TopologyRepo) saveDraft(ctx context.Context, nodeID int64, edges []models.TopologyEdge, expectedMaterialRevision *int64) (*models.TopologyDraft, error) {
	if nodeID <= 0 {
		return nil, errors.New("invalid node id")
	}
	if edges == nil {
		edges = make([]models.TopologyEdge, 0)
	}
	raw, err := json.Marshal(edges)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockTopologyNode(ctx, tx, nodeID); err != nil {
		return nil, err
	}
	if expectedMaterialRevision != nil {
		materialRevision, err := lockTopologyMaterial(ctx, tx)
		if err != nil {
			return nil, err
		}
		if materialRevision != *expectedMaterialRevision {
			return nil, ErrTopologyPreviewStale
		}
	}
	var draft models.TopologyDraft
	var storedEdges []byte
	err = tx.QueryRowContext(ctx, `
		INSERT INTO topology_drafts (node_id, revision, edges)
		VALUES ($1, 1, $2)
		ON CONFLICT (node_id) DO UPDATE
		SET revision=topology_drafts.revision+1, edges=EXCLUDED.edges, updated_at=now()
		RETURNING node_id, revision, edges, updated_at`, nodeID, raw).
		Scan(&draft.NodeID, &draft.Revision, &storedEdges, &draft.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(storedEdges, &draft.Edges); err != nil {
		return nil, err
	}
	if draft.Edges == nil {
		draft.Edges = make([]models.TopologyEdge, 0)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &draft, nil
}

func (r *TopologyRepo) GetMaterialRevision(ctx context.Context) (int64, error) {
	var revision int64
	err := r.db.QueryRowContext(ctx, `SELECT revision FROM topology_material_state WHERE id=TRUE`).Scan(&revision)
	return revision, err
}

// GetDeploymentForAgent returns only the last explicitly deployed snapshot.
// Drafts and current inbound rows are intentionally not consulted here.
func (r *TopologyRepo) GetDeploymentForAgent(ctx context.Context, nodeID int64) (*models.TopologyDeployment, error) {
	var deployment models.TopologyDeployment
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, version, schema_version, draft_revision, topology, rendered, deployed_at
		FROM topology_deployments WHERE node_id=$1`, nodeID).
		Scan(&deployment.NodeID, &deployment.Version, &deployment.SchemaVersion, &deployment.DraftRevision, &deployment.Topology, &deployment.Rendered, &deployment.DeployedAt)
	if err != nil {
		return nil, err
	}
	if err := validateDeployment(&deployment); err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (r *TopologyRepo) SaveDeployment(ctx context.Context, deployment *models.TopologyDeployment) error {
	if err := validateDeployment(deployment); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO topology_deployments (node_id, version, schema_version, draft_revision, topology, rendered)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (node_id) DO UPDATE
		SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version,
			draft_revision=EXCLUDED.draft_revision, topology=EXCLUDED.topology,
			rendered=EXCLUDED.rendered, deployed_at=now()`,
		deployment.NodeID, deployment.Version, deployment.SchemaVersion, deployment.DraftRevision, deployment.Topology, deployment.Rendered)
	return err
}

func (r *TopologyRepo) GetPreview(ctx context.Context, nodeID int64) (*TopologyPreview, error) {
	var preview TopologyPreview
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, version, draft_revision, material_revision, material_hash, topology, rendered
		FROM topology_previews WHERE node_id=$1`, nodeID).
		Scan(&preview.NodeID, &preview.Version, &preview.DraftRevision, &preview.MaterialRevision, &preview.MaterialHash, &preview.Topology, &preview.Rendered)
	if err != nil {
		return nil, err
	}
	if err := validatePreview(&preview); err != nil {
		return nil, err
	}
	return &preview, nil
}

func (r *TopologyRepo) SavePreview(ctx context.Context, preview *TopologyPreview) error {
	if err := validatePreview(preview); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockTopologyNode(ctx, tx, preview.NodeID); err != nil {
		return err
	}
	var materialRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`).Scan(&materialRevision); err != nil {
		return err
	}
	if preview.MaterialRevision != nil && *preview.MaterialRevision != materialRevision {
		return ErrTopologyPreviewStale
	}
	var draftRevision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM topology_drafts WHERE node_id=$1 FOR UPDATE`, preview.NodeID).Scan(&draftRevision)
	if errors.Is(err, sql.ErrNoRows) {
		draftRevision = 0
	} else if err != nil {
		return err
	}
	if draftRevision != preview.DraftRevision {
		return ErrTopologyPreviewStale
	}
	preview.MaterialRevision = int64Ptr(materialRevision)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO topology_previews (node_id, version, draft_revision, material_revision, material_hash, topology, rendered)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (node_id) DO UPDATE
		SET version=EXCLUDED.version, draft_revision=EXCLUDED.draft_revision,
			material_revision=EXCLUDED.material_revision,
			material_hash=EXCLUDED.material_hash, topology=EXCLUDED.topology,
			rendered=EXCLUDED.rendered, created_at=now()`,
		preview.NodeID, preview.Version, preview.DraftRevision, materialRevision, preview.MaterialHash, preview.Topology, preview.Rendered)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeployPreview atomically verifies the server-owned preview and draft
// revision before replacing the last explicit deployment snapshot.
func (r *TopologyRepo) DeployPreview(ctx context.Context, deployment *models.TopologyDeployment, version, materialHash string) error {
	if deployment == nil || deployment.NodeID <= 0 || version == "" || materialHash == "" {
		return errors.New("invalid topology deployment")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockTopologyNode(ctx, tx, deployment.NodeID); err != nil {
		return err
	}
	var currentMaterialRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`).Scan(&currentMaterialRevision); err != nil {
		return err
	}
	var previewVersion, previewMaterial string
	var previewRevision int64
	var previewMaterialRevision int64
	var previewRendered []byte
	var previewTopology []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT version, draft_revision, material_revision, material_hash, topology, rendered
		FROM topology_previews WHERE node_id=$1 FOR UPDATE`, deployment.NodeID).
		Scan(&previewVersion, &previewRevision, &previewMaterialRevision, &previewMaterial, &previewTopology, &previewRendered); err != nil {
		return err
	}
	if err := validatePreview(&TopologyPreview{NodeID: deployment.NodeID, Version: previewVersion, DraftRevision: previewRevision, MaterialRevision: int64Ptr(previewMaterialRevision), MaterialHash: previewMaterial, Rendered: previewRendered, Topology: previewTopology}); err != nil {
		return err
	}
	if previewMaterialRevision != currentMaterialRevision {
		return ErrTopologyPreviewStale
	}
	if !semanticJSONEqual(previewTopology, deployment.Topology) {
		return ErrTopologyPreviewStale
	}
	if previewVersion != version || previewRevision != deployment.DraftRevision || previewMaterial != materialHash {
		return ErrTopologyPreviewStale
	}
	var draftRevision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM topology_drafts WHERE node_id=$1 FOR UPDATE`, deployment.NodeID).Scan(&draftRevision)
	if errors.Is(err, sql.ErrNoRows) {
		draftRevision = 0
	} else if err != nil {
		return err
	}
	if draftRevision != deployment.DraftRevision {
		return ErrTopologyPreviewStale
	}
	if err := validateDeployment(deployment); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO topology_deployments (node_id, version, schema_version, draft_revision, topology, rendered)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (node_id) DO UPDATE
		SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version,
			draft_revision=EXCLUDED.draft_revision, topology=EXCLUDED.topology,
			rendered=EXCLUDED.rendered, deployed_at=now()`,
		deployment.NodeID, deployment.Version, deployment.SchemaVersion, deployment.DraftRevision,
		deployment.Topology, deployment.Rendered); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM topology_previews WHERE node_id=$1`, deployment.NodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func validatePreview(preview *TopologyPreview) error {
	if preview == nil || preview.NodeID <= 0 || preview.Version == "" || preview.MaterialHash == "" || len(preview.Topology) == 0 || len(preview.Rendered) == 0 {
		return errors.New("invalid topology preview")
	}
	if !json.Valid(preview.Topology) || sharedconfig.Hash(preview.Rendered) != preview.Version || sharedconfig.ValidateRendered(preview.Rendered) != nil {
		return errors.New("invalid topology preview content")
	}
	if err := validateRenderedTopology(preview.NodeID, preview.Topology, preview.Rendered, preview.Version); err != nil {
		return err
	}
	return nil
}

func validateDeployment(deployment *models.TopologyDeployment) error {
	if deployment == nil || deployment.NodeID <= 0 || deployment.Version == "" || deployment.SchemaVersion != sharedconfig.SchemaVersion || len(deployment.Topology) == 0 || len(deployment.Rendered) == 0 {
		return errors.New("invalid topology deployment")
	}
	if !json.Valid(deployment.Topology) || sharedconfig.Hash(deployment.Rendered) != deployment.Version || sharedconfig.ValidateRendered(deployment.Rendered) != nil {
		return errors.New("invalid topology deployment content")
	}
	if err := validateRenderedTopology(deployment.NodeID, deployment.Topology, deployment.Rendered, deployment.Version); err != nil {
		return err
	}
	return nil
}

func validateRenderedTopology(expectedNodeID int64, raw json.RawMessage, rendered []byte, version string) error {
	var topology sharedconfig.NodeConfig
	if err := json.Unmarshal(raw, &topology); err != nil {
		return errors.New("invalid topology snapshot")
	}
	if topology.NodeID <= 0 || topology.NodeID != expectedNodeID || topology.SchemaVersion != sharedconfig.SchemaVersion {
		return errors.New("invalid topology snapshot binding")
	}
	if expected, err := sharedconfig.RenderRouting(topology); err != nil || expected.Version != version || !bytes.Equal(expected.Content, rendered) {
		return errors.New("topology snapshot content mismatch")
	}
	return nil
}

func semanticJSONEqual(left, right []byte) bool {
	leftCanonical, err := canonicalJSON(left)
	if err != nil {
		return false
	}
	rightCanonical, err := canonicalJSON(right)
	if err != nil {
		return false
	}
	return bytes.Equal(leftCanonical, rightCanonical)
}

func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

var ErrTopologyPreviewStale = errors.New("topology preview is stale")

func lockTopologyNode(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	if nodeID <= 0 {
		return errors.New("invalid node id")
	}
	_, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended('coxpanel.topology.node:' || $1::bigint::text, 0))`, nodeID)
	return err
}

func lockTopologyMaterial(ctx context.Context, tx *sql.Tx) (int64, error) {
	var revision int64
	if err := tx.QueryRowContext(ctx, `
		SELECT revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`).Scan(&revision); err != nil {
		return 0, err
	}
	return revision, nil
}

func bumpTopologyMaterial(ctx context.Context, tx *sql.Tx) error {
	if _, err := lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE topology_material_state SET revision=revision+1 WHERE id=TRUE`)
	return err
}

func int64Ptr(value int64) *int64 {
	return &value
}
