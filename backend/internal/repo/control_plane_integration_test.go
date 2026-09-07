package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/models"
	sharedconfig "github.com/coxpanel/shared/config"
)

func TestBootstrapOwnerConcurrentIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)

	hash, err := auth.HashPassword("Synthetic-bootstrap-password-1!")
	if err != nil {
		t.Fatalf("hash bootstrap password: %v", err)
	}
	repository := NewUserRepo(resource.db)
	const attempts = 8
	results := make(chan struct {
		created bool
		err     error
	}, attempts)
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			created, runErr := repository.BootstrapOwner(context.Background(), fmt.Sprintf("synthetic-owner-%d", index), hash, "owner@example.invalid")
			results <- struct {
				created bool
				err     error
			}{created: created, err: runErr}
		}()
	}
	wait.Wait()
	close(results)

	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent bootstrap error: %v", result.err)
		}
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("successful owner bootstraps = %d, want 1", created)
	}
	var owners, users int
	if err := resource.db.QueryRowContext(context.Background(), `SELECT count(*) FROM users WHERE role='owner'`).Scan(&owners); err != nil {
		t.Fatalf("owner count query: %v", err)
	}
	if err := resource.db.QueryRowContext(context.Background(), `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("user count query: %v", err)
	}
	if owners != 1 || users != 1 {
		t.Fatalf("owner/user rows = %d/%d, want 1/1", owners, users)
	}
}

func TestTopologyDeploymentUsesExplicitSnapshotAndRejectsStalePreviewIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()

	var nodeID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name, type, public_ip) VALUES ('snapshot-node', 'managed', 'snapshot.example.invalid') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatalf("create snapshot node: %v", err)
	}
	topologyRepo := NewTopologyRepo(resource.db)
	draft, err := topologyRepo.SaveDraft(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("save direct draft: %v", err)
	}
	deployment, preview := directSnapshot(t, nodeID, draft.Revision, "first-snapshot")
	if err := topologyRepo.SavePreview(ctx, preview); err != nil {
		t.Fatalf("save exact preview: %v", err)
	}
	storedPreview, err := topologyRepo.GetPreview(ctx, nodeID)
	if err != nil {
		t.Fatalf("get exact preview: %v", err)
	}
	if string(storedPreview.Rendered) != string(preview.Rendered) {
		t.Fatal("preview did not retain exact internal rendered bytes")
	}
	if err := topologyRepo.DeployPreview(ctx, deployment, preview.Version, preview.MaterialHash); err != nil {
		t.Fatalf("deploy direct snapshot: %v", err)
	}
	loaded, err := topologyRepo.GetDeploymentForAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("get deployed snapshot: %v", err)
	}
	if loaded.Version != deployment.Version || string(loaded.Rendered) != string(deployment.Rendered) {
		t.Fatalf("loaded snapshot = %q/%d, want %q/%d", loaded.Version, len(loaded.Rendered), deployment.Version, len(deployment.Rendered))
	}

	updatedDraft, err := topologyRepo.SaveDraft(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("save mutated draft: %v", err)
	}
	if updatedDraft.Revision == deployment.DraftRevision {
		t.Fatal("draft revision did not advance")
	}
	unchanged, err := topologyRepo.GetDeploymentForAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("get snapshot after draft mutation: %v", err)
	}
	if unchanged.Version != deployment.Version || string(unchanged.Rendered) != string(deployment.Rendered) {
		t.Fatal("draft mutation changed explicit agent snapshot")
	}

	if err := topologyRepo.SavePreview(ctx, preview); !errors.Is(err, ErrTopologyPreviewStale) {
		t.Fatalf("stale preview save error = %v, want ErrTopologyPreviewStale", err)
	}
	if _, err := topologyRepo.GetPreview(ctx, nodeID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("preview after stale save = %v, want sql.ErrNoRows", err)
	}
	if err := topologyRepo.DeployPreview(ctx, deployment, preview.Version, preview.MaterialHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale deploy without persisted preview error = %v, want sql.ErrNoRows", err)
	}
	unchanged, err = topologyRepo.GetDeploymentForAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("get snapshot after stale deploy: %v", err)
	}
	if unchanged.Version != deployment.Version {
		t.Fatal("stale deploy replaced the previous explicit snapshot")
	}
}

func TestTopologyFailedDeploymentRetainsPreviousSnapshotIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()

	var nodeID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name, type) VALUES ('rollback-node', 'managed') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatalf("create rollback node: %v", err)
	}
	topologyRepo := NewTopologyRepo(resource.db)
	draft, err := topologyRepo.SaveDraft(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("save rollback draft: %v", err)
	}
	previous, previousPreview := directSnapshot(t, nodeID, draft.Revision, "previous-snapshot")
	if err := topologyRepo.SavePreview(ctx, previousPreview); err != nil {
		t.Fatalf("save previous preview: %v", err)
	}
	if err := topologyRepo.DeployPreview(ctx, previous, previousPreview.Version, previousPreview.MaterialHash); err != nil {
		t.Fatalf("deploy previous snapshot: %v", err)
	}

	nextDraft, err := topologyRepo.SaveDraft(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("save next draft: %v", err)
	}
	next, nextPreview := directSnapshot(t, nodeID, nextDraft.Revision, "next-snapshot")
	if err := topologyRepo.SavePreview(ctx, nextPreview); err != nil {
		t.Fatalf("save next preview: %v", err)
	}
	if _, err := resource.db.ExecContext(ctx, `
		CREATE FUNCTION reject_snapshot_replace() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic deployment rejection'; END; $$`); err != nil {
		t.Fatalf("create deployment rejection trigger function: %v", err)
	}
	if _, err := resource.db.ExecContext(ctx, `
		CREATE TRIGGER reject_snapshot_replace
		BEFORE INSERT OR UPDATE ON topology_deployments
		FOR EACH ROW EXECUTE FUNCTION reject_snapshot_replace()`); err != nil {
		t.Fatalf("create deployment rejection trigger: %v", err)
	}
	if err := topologyRepo.DeployPreview(ctx, next, nextPreview.Version, nextPreview.MaterialHash); err == nil {
		t.Fatal("deployment rejection unexpectedly succeeded")
	}
	loaded, err := topologyRepo.GetDeploymentForAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("get previous snapshot after failed deployment: %v", err)
	}
	if loaded.Version != previous.Version || string(loaded.Rendered) != string(previous.Rendered) {
		t.Fatal("failed deployment replaced the previous explicit snapshot")
	}
	if _, err := topologyRepo.GetPreview(ctx, nodeID); err != nil {
		t.Fatalf("failed deployment consumed the pending preview: %v", err)
	}
}

func TestTopologyDeploymentSerializesConcurrentDraftMutationIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()

	var nodeID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name, type) VALUES ('concurrent-snapshot-node', 'managed') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatalf("create concurrent snapshot node: %v", err)
	}
	topologyRepo := NewTopologyRepo(resource.db)
	draft, err := topologyRepo.SaveDraft(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("save concurrent draft: %v", err)
	}
	deployment, preview := directSnapshot(t, nodeID, draft.Revision, "concurrent-snapshot")
	if err := topologyRepo.SavePreview(ctx, preview); err != nil {
		t.Fatalf("save concurrent preview: %v", err)
	}

	const advisoryClassID = 27491
	const advisoryObjectID = 61837
	lockConn, err := resource.db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire trigger lock connection: %v", err)
	}
	defer lockConn.Close()
	defer lockConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1, $2)`, advisoryClassID, advisoryObjectID)
	if _, err := lockConn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, advisoryClassID, advisoryObjectID); err != nil {
		t.Fatalf("hold deployment trigger lock: %v", err)
	}
	if _, err := resource.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION pause_p1_topology_deployment() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(%d, %d);
			RETURN NEW;
		END;
		$$`, advisoryClassID, advisoryObjectID)); err != nil {
		t.Fatalf("create deployment pause function: %v", err)
	}
	if _, err := resource.db.ExecContext(ctx, `
		CREATE TRIGGER pause_p1_topology_deployment
		BEFORE INSERT OR UPDATE ON topology_deployments
		FOR EACH ROW EXECUTE FUNCTION pause_p1_topology_deployment()`); err != nil {
		t.Fatalf("create deployment pause trigger: %v", err)
	}

	deployCtx, cancelDeploy := context.WithTimeout(context.Background(), p1IntegrationTimeout)
	defer cancelDeploy()
	deployDone := make(chan error, 1)
	go func() {
		deployDone <- topologyRepo.DeployPreview(deployCtx, deployment, preview.Version, preview.MaterialHash)
	}()

	waitDeadline := time.Now().Add(p1IntegrationTimeout)
	for {
		select {
		case deployErr := <-deployDone:
			t.Fatalf("deployment ended before the database pause trigger blocked: %v", deployErr)
		default:
		}
		var waiting bool
		if err := resource.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks
				WHERE locktype='advisory' AND classid=$1 AND objid=$2 AND granted=FALSE
			)`, advisoryClassID, advisoryObjectID).Scan(&waiting); err != nil {
			t.Fatalf("observe deployment pause lock: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("deployment did not reach the deterministic pause trigger")
		}
		time.Sleep(10 * time.Millisecond)
	}

	saveEntered := make(chan struct{})
	saveDone := make(chan error, 1)
	go func() {
		close(saveEntered)
		_, saveErr := topologyRepo.SaveDraft(context.Background(), nodeID, nil)
		saveDone <- saveErr
	}()
	<-saveEntered
	select {
	case saveErr := <-saveDone:
		t.Fatalf("draft mutation committed while deployment transaction was paused: %v", saveErr)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := lockConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, advisoryClassID, advisoryObjectID); err != nil {
		t.Fatalf("release deployment trigger lock: %v", err)
	}
	select {
	case deployErr := <-deployDone:
		if deployErr != nil {
			t.Fatalf("serialized deployment: %v", deployErr)
		}
	case <-time.After(p1IntegrationTimeout):
		t.Fatal("serialized deployment did not complete")
	}
	select {
	case saveErr := <-saveDone:
		if saveErr != nil {
			t.Fatalf("serialized draft mutation: %v", saveErr)
		}
	case <-time.After(p1IntegrationTimeout):
		t.Fatal("serialized draft mutation did not complete")
	}

	loadedDeployment, err := topologyRepo.GetDeploymentForAgent(ctx, nodeID)
	if err != nil {
		t.Fatalf("load serialized deployment: %v", err)
	}
	if loadedDeployment.Version != deployment.Version || loadedDeployment.DraftRevision != deployment.DraftRevision {
		t.Fatalf("deployment snapshot changed during concurrent draft mutation: got version=%q revision=%d, want version=%q revision=%d", loadedDeployment.Version, loadedDeployment.DraftRevision, deployment.Version, deployment.DraftRevision)
	}
	loadedDraft, err := topologyRepo.GetDraft(ctx, nodeID)
	if err != nil {
		t.Fatalf("load serialized draft: %v", err)
	}
	if loadedDraft.Revision != draft.Revision+1 {
		t.Fatalf("draft revision = %d, want %d after serialized mutation", loadedDraft.Revision, draft.Revision+1)
	}
}

func directSnapshot(t *testing.T, nodeID, revision int64, name string) (*models.TopologyDeployment, *TopologyPreview) {
	t.Helper()
	topology := sharedconfig.NodeConfig{
		SchemaVersion: sharedconfig.SchemaVersion,
		NodeID:        nodeID,
		NodeName:      name,
		Inbounds:      []sharedconfig.Inbound{},
		Edges:         []sharedconfig.Edge{},
		Outbound:      "direct",
	}
	rendered, err := sharedconfig.RenderRouting(topology)
	if err != nil {
		t.Fatalf("render direct snapshot: %v", err)
	}
	raw, err := json.Marshal(topology)
	if err != nil {
		t.Fatalf("marshal direct snapshot topology: %v", err)
	}
	materialHash := sharedconfig.Hash(raw)
	deployment := &models.TopologyDeployment{
		NodeID: nodeID, Version: rendered.Version, SchemaVersion: rendered.SchemaVersion,
		DraftRevision: revision, Topology: raw, Rendered: rendered.Content,
	}
	preview := &TopologyPreview{
		NodeID: nodeID, Version: rendered.Version, DraftRevision: revision,
		MaterialHash: materialHash, Topology: raw, Rendered: rendered.Content,
	}
	return deployment, preview
}

var _ = sql.ErrNoRows
