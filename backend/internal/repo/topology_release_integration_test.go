package repo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/coxpanel/backend/internal/mail"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/backend/internal/topology"
	"github.com/coxpanel/shared/contract"
	"strings"
	"testing"
)

func p2TopologyDB(t *testing.T) *sql.DB {
	t.Helper()
	return repo.OpenP2TopologyTestDB(t)
}

func TestP2ThreeHopPrepareApplyAndRollbackIntegration(t *testing.T) {
	pool := p2TopologyDB(t)
	ctx := context.Background()
	_, err := pool.Exec(`INSERT INTO users(id,username,password_hash,role,is_active) VALUES(1,'owner','synthetic','owner',true);INSERT INTO nodes(id,name,type,public_ip,agent_capabilities) VALUES(1,'A','managed','127.0.0.1','["topology-chain-v2"]'),(2,'B','managed','127.0.0.1','["topology-chain-v2"]'),(3,'C','managed','127.0.0.1','["topology-chain-v2"]');INSERT INTO inbounds(id,node_id,name,protocol,role,egress_mode,listen_addr,listen_port,config) VALUES(101,1,'entry','shadowsocks','entry','chain','127.0.0.1',11101,'{"method":"2022-blake3-aes-128-gcm","password":"QUFBQUFBQUFBQUFBQUFBQQ=="}'),(201,2,'relay','shadowsocks','relay','chain','127.0.0.1',11201,'{"method":"2022-blake3-aes-128-gcm","password":"QkJCQkJCQkJCQkJCQkJCQg=="}'),(301,3,'landing','shadowsocks','landing','direct','127.0.0.1',11301,'{"method":"2022-blake3-aes-128-gcm","password":"Q0NDQ0NDQ0NDQ0NDQ0NDQw=="}');INSERT INTO node_groups(id,name) VALUES(1,'group');INSERT INTO node_group_members(group_id,node_id) VALUES(1,1);INSERT INTO user_node_groups(user_id,group_id) VALUES(1,1);INSERT INTO user_credentials(user_id,inbound_id,credential) VALUES(1,101,'RERERERERERERERERERERA==')`)
	if err != nil {
		t.Fatal(err)
	}
	store := repo.NewTopologyRepo(pool)
	sealer, err := mail.New(pool, mail.Config{}, strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	store.SetSnapshotSealer(sealer)
	service := topology.NewService(repo.NewNodeRepo(pool), store)
	graph, err := service.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	changes := []repo.GraphChange{{NodeID: 1, Edges: []models.TopologyEdge{{FromInboundID: 101, ToNodeID: 2, ToInboundID: 201}}}, {NodeID: 2, Edges: []models.TopologyEdge{{FromInboundID: 201, ToNodeID: 3, ToInboundID: 301}}}, {NodeID: 3, Edges: []models.TopologyEdge{}}}
	graph, err = service.SaveGraph(ctx, graph.GraphRevision, changes)
	if err != nil || !graph.DraftValid {
		t.Fatalf("save chain failed: %v", err)
	}
	preview, err := service.PreviewChain(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Dependencies) != 3 || preview.Dependencies[0] != 3 || preview.Dependencies[2] != 1 {
		t.Fatal("wrong downstream-first order")
	}
	release, err := store.CreateTopologyRelease(ctx, preview.PreviewID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	var deployments int
	if err = pool.QueryRow(`SELECT count(*) FROM topology_deployments`).Scan(&deployments); err != nil || deployments != 0 {
		t.Fatal("prepare activated runtime")
	}
	for _, nodeID := range []int64{1, 2, 3} {
		document, err := store.GetAgentDeployment(ctx, nodeID, release.ID, "prepare")
		if err != nil {
			t.Fatal(err)
		}
		if err = document.Validate(true); err != nil {
			t.Fatal(err)
		}
		ack := &contract.DeploymentAck{ReleaseID: release.ID, NodeID: nodeID, Generation: document.Generation, Phase: "prepare", RuntimeVersion: document.Version, Status: "prepared"}
		if err = store.AcknowledgeAgentDeployment(ctx, ack); err != nil {
			t.Fatal(err)
		}
		if err = store.AcknowledgeAgentDeployment(ctx, ack); err != nil {
			t.Fatal("duplicate prepare not idempotent")
		}
	}
	for _, nodeID := range []int64{3, 2, 1} {
		pending, err := store.PendingDeployment(ctx, nodeID)
		if err != nil || pending == nil || pending.Phase != "apply" {
			t.Fatal("expected ordered apply")
		}
		document, err := store.GetAgentDeployment(ctx, nodeID, release.ID, "apply")
		if err != nil {
			t.Fatal(err)
		}
		if err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: release.ID, NodeID: nodeID, Generation: document.Generation, Phase: "apply", RuntimeVersion: document.Version, Status: "applied"}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.GetRelease(ctx, release.ID)
	if err != nil || result["status"] != "succeeded" {
		t.Fatal("release not succeeded")
	}
	if err = store.RollbackRelease(ctx, release.ID); err == nil {
		t.Fatal("unrelated successful rollback accepted")
	}
	preview, err = service.PreviewChain(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateTopologyRelease(ctx, preview.PreviewID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []int64{1, 2, 3} {
		document, err := store.GetAgentDeployment(ctx, nodeID, second.ID, "prepare")
		if err != nil {
			t.Fatal(err)
		}
		if err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: second.ID, NodeID: nodeID, Generation: document.Generation, Phase: "prepare", RuntimeVersion: document.Version, Status: "prepared"}); err != nil {
			t.Fatal(err)
		}
	}
	document, err := store.GetAgentDeployment(ctx, 3, second.ID, "apply")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: second.ID, NodeID: 3, Generation: document.Generation, Phase: "apply", Status: "failed", ErrorCode: "synthetic_failure"}); err != nil {
		t.Fatal(err)
	}
	rollback, err := store.GetAgentDeployment(ctx, 3, second.ID, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Generation <= document.Generation {
		t.Fatal("rollback generation did not advance")
	}
	if err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: second.ID, NodeID: 3, Generation: rollback.Generation, Phase: "rollback", RuntimeVersion: rollback.Version, Status: "rolled_back"}); err != nil {
		t.Fatal(err)
	}
	result, err = store.GetRelease(ctx, second.ID)
	if err != nil || result["status"] != "rolled_back" {
		encoded, _ := json.Marshal(result)
		t.Fatalf("rollback not finished: %s", encoded)
	}
	preview, err = service.PreviewChain(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateTopologyRelease(ctx, preview.PreviewID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.GetAgentDeployment(ctx, 1, third.ID, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(`UPDATE users SET expire_at=now()-interval '1 second' WHERE id=1`); err != nil {
		t.Fatal("fixture expiry failed")
	}
	err = store.AcknowledgeAgentDeployment(ctx, &contract.DeploymentAck{ReleaseID: third.ID, NodeID: 1, Generation: prepared.Generation, Phase: "prepare", RuntimeVersion: prepared.Version, Status: "prepared"})
	if !errors.Is(err, repo.ErrTopologyGenerationStale) {
		t.Fatal("expired runtime acknowledgement accepted")
	}
	result, err = store.GetRelease(ctx, third.ID)
	if err != nil || result["status"] != "manual_required" {
		t.Fatal("revoked runtime did not stop release")
	}
}
