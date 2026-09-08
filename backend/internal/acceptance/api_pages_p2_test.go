package acceptance

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestP2TemplatePaginationAndMailTrafficHTTPBoundaries(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	group := harness.createGroup(owner, "p2-http-permissions")
	person := harness.register("p2-http-user", "Synthetic-password-7!", "p2-http@example.test", harness.createInvite(owner, group))
	for _, name := range []string{"first-page", "second-page"} {
		harness.doRaw(http.MethodPost, "/api/templates", owner.Token, map[string]any{"name": name, "format": "mihomo", "definition": map[string]any{"schemaVersion": 1}}, 201)
	}
	var page struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor string            `json:"nextCursor"`
	}
	harness.doJSON(http.MethodGet, "/api/templates?includeDrafts=true&limit=1", owner.Token, nil, 200, &page)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("first template page invalid")
	}
	harness.doJSON(http.MethodGet, "/api/templates?includeDrafts=true&limit=1&cursor="+page.NextCursor, owner.Token, nil, 200, &page)
	if len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatal("template continuation invalid")
	}
	harness.doJSON(http.MethodGet, "/api/templates?includeDrafts=true", person.Token, nil, 200, &page)
	if len(page.Items) != 0 {
		t.Fatal("draft visible to ordinary user")
	}
	harness.expectError(http.MethodGet, "/api/templates?limit=201", owner.Token, nil, 422, "invalid_pagination")
	harness.doRaw(http.MethodGet, "/api/settings/smtp", owner.Token, nil, 200)
	harness.expectError(http.MethodGet, "/api/settings/smtp", person.Token, nil, 403, "forbidden")
	harness.doJSON(http.MethodGet, "/api/alert-rules?limit=1", owner.Token, nil, 200, &page)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("rules pagination invalid")
	}
	harness.expectError(http.MethodPost, "/api/alert-rules", owner.Token, map[string]any{"name": "invalid channel", "kind": "traffic_limit", "scopeType": "all", "thresholds": []int{80}, "channel": "webhook"}, 422, "channel_unsupported")
	harness.doRaw(http.MethodGet, "/api/my/notification-preferences", person.Token, nil, 200)
	harness.doJSON(http.MethodGet, "/api/my/notifications", person.Token, nil, 200, &page)
	if len(page.Items) != 0 {
		t.Fatal("unexpected notification")
	}
	harness.doRaw(http.MethodGet, "/api/my/traffic", person.Token, nil, 200)
	harness.expectError(http.MethodGet, "/api/traffic/999999", person.Token, nil, 404, "not_found")
}
