package repo

import (
	"context"
	"testing"

	"github.com/coxpanel/backend/internal/db"
)

func TestP2TopologyRetentionProtectsRecentReferencedAndRecovery(t *testing.T) {
	resource := openP1IntegrationDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("migration failed")
	}
	_, err := resource.db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role) VALUES(1,'retention-owner','synthetic','owner'); INSERT INTO nodes(id,name,type) VALUES(1,'retention-node','managed');
	INSERT INTO topology_releases(id,root_node_id,graph_revision,material_revision,revision_vector,status,created_by,created_at,finished_at) SELECT number,1,1,1,'{}','succeeded',1,now()-interval '60 days'+number*interval '1 minute',now()-interval '60 days'+number*interval '1 minute' FROM generate_series(1,12) number;
	INSERT INTO topology_release_nodes(release_id,node_id,phase,candidate_topology,routing_version,expected_generation,previous_generation,apply_order,status) SELECT id,1,'apply','{}','synthetic',1,0,0,'applied' FROM topology_releases;
	UPDATE topology_releases SET status='manual_required' WHERE id=1`)
	if err != nil {
		t.Fatal("retention fixture failed")
	}
	store := NewTopologyRepo(resource.db)
	if err = store.PruneReleaseHistory(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = resource.db.QueryRow(`SELECT count(*) FROM topology_releases`).Scan(&count); err != nil || count != 12 {
		t.Fatal("recovery dependencies were pruned")
	}
	if _, err = resource.db.Exec(`UPDATE topology_releases SET status='rolled_back' WHERE id=1`); err != nil {
		t.Fatal("fixture transition failed")
	}
	if err = store.PruneReleaseHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if err = resource.db.QueryRow(`SELECT count(*),min(id) FROM topology_releases`).Scan(&count, new(int)); err != nil || count != 10 {
		t.Fatal("retention did not preserve exactly ten latest releases")
	}
	if err = store.PruneReleaseHistory(ctx); err != nil {
		t.Fatal("retention was not idempotent")
	}
}
