package repo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SnapshotSealer is the application boundary for topology material. A
// repository never falls back to storing candidate configuration in cleartext.
type SnapshotSealer interface {
	Seal(context.Context, string, []byte) ([]byte, error)
	Open(context.Context, string, []byte) ([]byte, error)
}

var ErrTopologySecureStorageUnavailable = errors.New("topology secure storage unavailable")
var ErrTopologyReleaseNotFound = errors.New("topology release not found")
var ErrTopologyReleaseConflict = errors.New("topology release conflict")
var ErrTopologyGenerationStale = errors.New("topology generation is stale")

type TopologyCandidate struct {
	NodeID             int64           `json:"nodeId"`
	Topology           json.RawMessage `json:"topology,omitempty"`
	Rendered           []byte          `json:"rendered,omitempty"`
	PreviousTopology   json.RawMessage `json:"previousTopology,omitempty"`
	PreviousRendered   []byte          `json:"previousRendered,omitempty"`
	RoutingVersion     string          `json:"routingVersion,omitempty"`
	ExpectedRuntime    string          `json:"expectedRuntimeVersion,omitempty"`
	ExpectedGeneration int64           `json:"expectedGeneration"`
	PreviousGeneration int64           `json:"previousGeneration"`
	ApplyOrder         int             `json:"applyOrder"`
	DraftRevision      int64           `json:"draftRevision"`
}

type TopologyPreviewV2 struct {
	PreviewID        string              `json:"previewId"`
	RootNodeID       int64               `json:"rootNodeId"`
	GraphRevision    int64               `json:"graphRevision"`
	MaterialRevision int64               `json:"materialRevision"`
	RevisionVector   json.RawMessage     `json:"revisionVector,omitempty"`
	Candidates       []TopologyCandidate `json:"candidates"`
	ExpiresAt        time.Time           `json:"expiresAt"`
}

type TopologyRelease struct {
	ID               int64           `json:"id"`
	RootNodeID       int64           `json:"rootNodeId"`
	GraphRevision    int64           `json:"graphRevision"`
	MaterialRevision int64           `json:"materialRevision"`
	RevisionVector   json.RawMessage `json:"revisionVector,omitempty"`
	Status           string          `json:"status"`
	CreatedBy        int64           `json:"createdBy"`
	ErrorCode        string          `json:"errorCode,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	FinishedAt       *time.Time      `json:"finishedAt,omitempty"`
}

type TopologyReleaseNode struct {
	ReleaseID          int64      `json:"releaseId"`
	NodeID             int64      `json:"nodeId"`
	Phase              string     `json:"phase"`
	RoutingVersion     string     `json:"routingVersion"`
	ExpectedRuntime    string     `json:"expectedRuntimeVersion,omitempty"`
	ExpectedGeneration int64      `json:"expectedGeneration"`
	PreviousGeneration int64      `json:"previousGeneration"`
	ApplyOrder         int        `json:"applyOrder"`
	Status             string     `json:"status"`
	Attempts           int        `json:"attempts"`
	LastAckAt          *time.Time `json:"lastAckAt,omitempty"`
	ErrorCode          string     `json:"errorCode,omitempty"`
}

// SetSnapshotSealer injects the key-backed envelope implementation during
// application assembly. It is intentionally separate from NewTopologyRepo so
// existing P1 construction remains source compatible.
func (r *TopologyRepo) SetSnapshotSealer(sealer SnapshotSealer) {
	if r != nil {
		r.sealer = sealer
	}
}

func (r *TopologyRepo) snapshotSealer() (SnapshotSealer, error) {
	if r == nil || r.db == nil || r.sealer == nil {
		return nil, ErrTopologySecureStorageUnavailable
	}
	return r.sealer, nil
}

func newTopologyUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func validPreviewUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func (r *TopologyRepo) SavePreviewV2(ctx context.Context, preview *TopologyPreviewV2) error {
	sealer, err := r.snapshotSealer()
	if err != nil {
		return err
	}
	if preview == nil || preview.RootNodeID <= 0 || len(preview.Candidates) == 0 || preview.ExpiresAt.IsZero() || !preview.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("invalid topology preview")
	}
	if preview.PreviewID == "" || !validPreviewUUID(preview.PreviewID) {
		preview.PreviewID, err = newTopologyUUID()
		if err != nil {
			return err
		}
	}
	if preview.RevisionVector == nil {
		preview.RevisionVector = json.RawMessage(`{}`)
	}
	if !json.Valid(preview.RevisionVector) {
		return errors.New("invalid topology revision vector")
	}
	for _, candidate := range preview.Candidates {
		if candidate.NodeID <= 0 || len(candidate.Rendered) == 0 || !json.Valid(candidate.Topology) {
			return errors.New("invalid topology candidate")
		}
	}
	plaintext, err := json.Marshal(preview.Candidates)
	if err != nil {
		return err
	}
	ciphertext, err := sealer.Seal(ctx, fmt.Sprintf("topology_previews:%d:%s:candidates", preview.RootNodeID, preview.PreviewID), plaintext)
	if err != nil {
		return fmt.Errorf("seal topology preview: %w", err)
	}
	bundle, err := json.Marshal(map[string]string{
		"version":    "v1",
		"ciphertext": base64.RawStdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockTopologyNode(ctx, tx, preview.RootNodeID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO topology_previews
		(node_id,preview_id,graph_revision,revision_vector,candidate_bundle,expires_at,version,draft_revision,material_revision,material_hash,topology,rendered)
		VALUES ($1,$2,$3,$4,$5,$6,'sealed-v2',0,$7,'sealed-v2','{}',$8)
		ON CONFLICT (node_id) DO UPDATE SET
		 preview_id=EXCLUDED.preview_id, graph_revision=EXCLUDED.graph_revision,
		 revision_vector=EXCLUDED.revision_vector, candidate_bundle=EXCLUDED.candidate_bundle,
		 expires_at=EXCLUDED.expires_at, version=EXCLUDED.version,
		 material_revision=EXCLUDED.material_revision, material_hash=EXCLUDED.material_hash,
		 topology=EXCLUDED.topology, rendered=EXCLUDED.rendered, created_at=now()`,
		preview.RootNodeID, preview.PreviewID, preview.GraphRevision, preview.RevisionVector, bundle,
		preview.ExpiresAt.UTC(), preview.MaterialRevision, ciphertext)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TopologyRepo) GetPreviewV2(ctx context.Context, rootNodeID int64) (*TopologyPreviewV2, error) {
	sealer, err := r.snapshotSealer()
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil || rootNodeID <= 0 {
		return nil, errors.New("invalid topology preview")
	}
	var preview TopologyPreviewV2
	var vector, bundle []byte
	var ciphertext []byte
	if err := r.db.QueryRowContext(ctx, `
		SELECT preview_id,graph_revision,material_revision,revision_vector,candidate_bundle,expires_at,rendered
		FROM topology_previews WHERE node_id=$1`, rootNodeID).
		Scan(&preview.PreviewID, &preview.GraphRevision, &preview.MaterialRevision, &vector, &bundle, &preview.ExpiresAt, &ciphertext); err != nil {
		return nil, err
	}
	if !preview.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrTopologyPreviewStale
	}
	preview.RootNodeID = rootNodeID
	preview.RevisionVector = json.RawMessage(vector)
	var envelope struct {
		Version    string `json:"version"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(bundle, &envelope); err != nil || envelope.Version != "v1" {
		return nil, ErrTopologySecureStorageUnavailable
	}
	if envelope.Ciphertext != "" {
		ciphertext, err = base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
		if err != nil {
			return nil, ErrTopologySecureStorageUnavailable
		}
	}
	plaintext, err := sealer.Open(ctx, fmt.Sprintf("topology_previews:%d:%s:candidates", rootNodeID, preview.PreviewID), ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open topology preview: %w", err)
	}
	if err := json.Unmarshal(plaintext, &preview.Candidates); err != nil {
		return nil, ErrTopologySecureStorageUnavailable
	}
	return &preview, nil
}
