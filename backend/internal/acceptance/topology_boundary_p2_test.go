package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/coxpanel/backend/internal/mail"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/shared/contract"
)

func TestP2LegacyTopologyCannotBypassGraphOrDeployChain(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	source := harness.createNode(owner, "legacy-source", "127.0.0.1")
	target := harness.createNode(owner, "legacy-target", "127.0.0.1")
	material := json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"QUFBQUFBQUFBQUFBQUFBQQ=="}`)
	entry := harness.createInbound(owner, source.ID, "entry", "shadowsocks", "entry", "127.0.0.1", reserveAcceptanceTCPPort(t), material)
	landing := harness.createInbound(owner, target.ID, "landing", "shadowsocks", "landing", "127.0.0.1", reserveAcceptanceTCPPort(t), material)
	var before, after repo.GraphSnapshot
	harness.doJSON(http.MethodGet, "/api/topology/graph", owner.Token, nil, 200, &before)
	path := fmt.Sprintf("/api/topology/%d", source.ID)
	edges := []models.TopologyEdge{{FromInboundID: entry, ToNodeID: target.ID, ToInboundID: landing}}
	harness.doRaw(http.MethodPut, path, owner.Token, map[string]any{"edges": edges}, 200)
	harness.doJSON(http.MethodGet, "/api/topology/graph", owner.Token, nil, 200, &after)
	if after.GraphRevision != before.GraphRevision+1 {
		t.Fatal("legacy write did not participate in global revision")
	}
	harness.expectError(http.MethodPut, path, owner.Token, map[string]any{"expectedRevision": 0, "edges": edges}, 409, "revision_conflict")
	harness.expectError(http.MethodPost, path+"/deploy", owner.Token, map[string]any{"version": "legacy-version"}, 409, "upgrade_required")
	harness.expectError(http.MethodPost, path+"/preview", owner.Token, nil, 409, "agent_upgrade_required")
}

func (harness *panelHarness) deployChainWithFixtureAcks(owner principal, root int64, nodes []int64) {
	harness.t.Helper()
	ctx := context.Background()
	for _, node := range nodes {
		if _, err := harness.db.Exec(`UPDATE nodes SET agent_capabilities='["topology-chain-v2"]' WHERE id=$1`, node); err != nil {
			harness.t.Fatal("fixture capability failed")
		}
	}
	var preview struct {
		PreviewID     string `json:"previewId"`
		GraphRevision int64  `json:"graphRevision"`
	}
	harness.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/preview", root), owner.Token, nil, 200, &preview)
	var release struct {
		ID int64 `json:"releaseId"`
	}
	harness.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/deploy", root), owner.Token, map[string]any{"previewId": preview.PreviewID, "expectedGraphRevision": preview.GraphRevision}, 202, &release)
	store := repo.NewTopologyRepo(harness.db)
	sealer, err := mail.New(harness.db, mail.Config{}, strings.Repeat("a7", 32))
	if err != nil {
		harness.t.Fatal("synthetic sealer failed")
	}
	store.SetSnapshotSealer(sealer)
	for _, phase := range []string{"prepare", "apply"} {
		for _, node := range nodes {
			document, err := store.GetAgentDeployment(ctx, node, release.ID, phase)
			if err != nil {
				harness.t.Fatal("fixture deployment unavailable")
			}
			status := "prepared"
			if phase == "apply" {
				status = "applied"
			}
			if err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: release.ID, NodeID: node, Generation: document.Generation, Phase: phase, RuntimeVersion: document.Version, Status: status}); err != nil {
				harness.t.Fatal("fixture acknowledgement rejected")
			}
		}
	}
}
