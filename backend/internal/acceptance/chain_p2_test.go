package acceptance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

func TestP2ThreeHopRealAgentsPrepareApplyAndHandshake(t *testing.T) {
	binary := acceptanceAgentRuntimeBinary(t)
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p2-chain-runtime")
	nodes := make([]idResponse, 3)
	inbounds := make([]int64, 3)
	for index, role := range []string{"entry", "relay", "landing"} {
		nodes[index] = harness.createNode(owner, fmt.Sprintf("chain-%d", index), "127.0.0.1")
		password := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(0x31 + index)}, 16))
		inbounds[index] = harness.createInbound(owner, nodes[index].ID, role, "shadowsocks", role, "127.0.0.1", reserveAcceptanceTCPPort(t), json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, password)))
		startAgentRuntimeProcess(t, binary, harness.server.URL, nodes[index], filepath.Join(t.TempDir(), "agent.json"))
	}
	harness.setGroupNodes(owner, groupID, []int64{nodes[0].ID})
	person := harness.register("p2-chain-user", "Synthetic-password-4!", "p2-chain@example.test", harness.createInvite(owner, groupID))
	subscription := harness.createSubscription(person, groupID, "three-hop")
	deadline := time.Now().Add(12 * time.Second)
	for {
		var count int
		err := harness.db.QueryRow(`SELECT count(*) FROM nodes WHERE agent_capabilities ? 'topology-chain-v2'`).Scan(&count)
		if err == nil && count == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real agents failed capability negotiation")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var graph repo.GraphSnapshot
	harness.doJSON(http.MethodGet, "/api/topology/graph", owner.Token, nil, 200, &graph)
	changes := []repo.GraphChange{}
	for index, node := range nodes {
		change := repo.GraphChange{NodeID: node.ID, Layout: json.RawMessage(`{}`)}
		if index < 2 {
			change.Edges = append(change.Edges, models.TopologyEdge{FromInboundID: inbounds[index], ToNodeID: nodes[index+1].ID, ToInboundID: inbounds[index+1]})
		}
		if index == 0 {
			change.InboundModes = []repo.GraphInboundMode{{InboundID: inbounds[index], EgressMode: "chain"}}
		}
		changes = append(changes, change)
	}
	harness.doJSON(http.MethodPut, "/api/topology/graph", owner.Token, map[string]any{"expectedGraphRevision": graph.GraphRevision, "changes": changes}, 200, &graph)
	var preview struct {
		PreviewID     string `json:"previewId"`
		GraphRevision int64  `json:"graphRevision"`
	}
	harness.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/preview", nodes[0].ID), owner.Token, nil, 200, &preview)
	var release struct {
		ID int64 `json:"releaseId"`
	}
	harness.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/deploy", nodes[0].ID), owner.Token, map[string]any{"previewId": preview.PreviewID, "expectedGraphRevision": preview.GraphRevision}, 202, &release)
	deadline = time.Now().Add(25 * time.Second)
	for {
		var status string
		if err := harness.db.QueryRow(`SELECT status FROM topology_releases WHERE id=$1`, release.ID).Scan(&status); err != nil {
			t.Fatal("release status unavailable")
		}
		if status == "succeeded" {
			break
		}
		if status == "failed" || status == "manual_required" || time.Now().After(deadline) {
			t.Fatalf("real chain release did not succeed: %s", status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	marker := "p2-three-hop-real-marker"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/p1-ready" {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(marker))
	}))
	defer target.Close()
	body := harness.subscriptionBody(subscription.Token, "")
	client := startAcceptanceMihomo(t, acceptanceMihomoConfig(t, body, "shadowsocks", reserveAcceptanceTCPPort(t), nil))
	waitForAcceptanceProxyReadiness(t, client.proxy, target.URL+"/p1-ready")
	status, content, err := acceptanceProxyRequest(client.proxy, http.MethodGet, target.URL+"/p1-marker")
	if err != nil || status != 200 || string(content) != marker {
		t.Fatal("three-hop real handshake failed")
	}
}
