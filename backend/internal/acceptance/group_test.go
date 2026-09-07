package acceptance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPanelAuthorizedGroupsDriveSubscriptionCreation(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	authorizedGroupID := harness.createGroup(owner, "p1-authorized-user-group")
	unassignedGroupID := harness.createGroup(owner, "p1-unassigned-user-group")
	node := harness.createNode(owner, "p1-user-group-node", "127.0.0.1")
	harness.setGroupNodes(owner, authorizedGroupID, []int64{node.ID})

	user := harness.register(
		"p1-authorized-group-user",
		"Authorized-group-password-7!",
		"p1-authorized-group-user@example.invalid",
		harness.createInvite(owner, authorizedGroupID),
	)

	groupsBody := harness.doRaw(http.MethodGet, "/api/my/groups", user.Token, nil, http.StatusOK)
	var groups []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(groupsBody, &groups); err != nil {
		t.Fatalf("authorized group response is not JSON: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != authorizedGroupID || groups[0].Name != "p1-authorized-user-group" {
		t.Fatalf("authorized groups = %+v, want only the explicit authorized group", groups)
	}
	if bytes.Contains(groupsBody, []byte("nodeIds")) || bytes.Contains(groupsBody, []byte("agentCredential")) || bytes.Contains(groupsBody, []byte("secret")) {
		t.Fatal("authorized group response exposed node or secret material")
	}

	harness.createSubscription(user, authorizedGroupID, "p1-authorized-group-subscription")
	harness.expectError(
		http.MethodPost,
		"/api/my/subscriptions",
		user.Token,
		map[string]any{"name": "p1-unassigned-group-subscription", "format": "mihomo", "nodeGroupId": unassignedGroupID},
		http.StatusForbidden,
		"group_not_authorized",
	)

	harness.setUserGroups(owner, user.ID, []int64{})
	emptyBody := harness.doRaw(http.MethodGet, "/api/my/groups", user.Token, nil, http.StatusOK)
	var emptyGroups []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(emptyBody, &emptyGroups); err != nil {
		t.Fatalf("empty authorized group response is not JSON: %v", err)
	}
	if emptyGroups == nil || len(emptyGroups) != 0 {
		t.Fatalf("authorized groups after revocation = %+v, want an empty array", emptyGroups)
	}
	harness.expectError(
		http.MethodPost,
		"/api/my/subscriptions",
		user.Token,
		map[string]any{"name": "p1-revoked-group-subscription", "format": "mihomo", "nodeGroupId": authorizedGroupID},
		http.StatusForbidden,
		"group_not_authorized",
	)
}
