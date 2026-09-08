package acceptance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func runP2SingboxClientExchange(t *testing.T, body []byte, protocol, target, marker string) {
	t.Helper()
	var document map[string]any
	if json.Unmarshal(body, &document) != nil {
		t.Fatal("sing-box subscription invalid JSON")
	}
	var selected map[string]any
	for _, value := range document["outbounds"].([]any) {
		outbound := value.(map[string]any)
		if outbound["type"] == protocol {
			selected = outbound
			break
		}
	}
	if selected == nil {
		t.Fatal("client protocol missing")
	}
	port := reserveAcceptanceTCPPort(t)
	document["outbounds"] = []any{selected}
	document["route"] = map[string]any{"final": selected["tag"]}
	document["inbounds"] = []any{map[string]any{"type": "mixed", "tag": "test-client", "listen": "127.0.0.1", "listen_port": port}}
	content, _ := json.Marshal(document)
	if err := runAcceptanceSingboxCheck(t, content); err != nil {
		t.Fatalf("%s generated client rejected", protocol)
	}
	startAcceptanceSingbox(t, content)
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	waitForAcceptanceProxyReadiness(t, client, target+"/p1-ready")
	status, response, err := acceptanceProxyRequest(client, http.MethodGet, target+"/p1-marker")
	if err != nil || status != 200 || string(response) != marker {
		t.Fatalf("%s real client handshake failed", protocol)
	}
}

func TestP2TemplateOverrideHTTPAuthorizationAndPreview(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p2-template-http")
	node := harness.createNode(owner, "p2-client", "127.0.0.1")
	serverPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 16))
	inboundID := harness.createInbound(owner, node.ID, "entry", "shadowsocks", "entry", "127.0.0.1", reserveAcceptanceTCPPort(t), json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, serverPSK)))
	harness.setGroupNodes(owner, groupID, []int64{node.ID})
	person := harness.register("p2-template-person", "Synthetic-password-2!", "p2-person@example.test", harness.createInvite(owner, groupID))
	subscription := harness.createSubscription(person, groupID, "P2 sample")
	definition := map[string]any{"schemaVersion": 1, "groups": []any{map[string]any{"id": "main", "name": "选择", "type": "select", "members": []string{"$authorizedProxies", "DIRECT"}}}, "rules": []any{map[string]any{"type": "MATCH", "target": "group:main"}}, "dns": map[string]any{"enabled": true, "resolverId": "cloudflare"}}
	created := harness.doRaw(http.MethodPost, "/api/templates", owner.Token, map[string]any{"name": "client-template", "format": "sing-box", "definition": definition}, 201)
	var template struct {
		ID       int64 `json:"id"`
		Revision int64 `json:"revision"`
	}
	if json.Unmarshal(created, &template) != nil || template.ID == 0 {
		t.Fatal("template create response invalid")
	}
	path := fmt.Sprintf("/api/templates/%d", template.ID)
	harness.expectError(http.MethodPut, path, person.Token, map[string]any{}, 403, "forbidden")
	harness.doRaw(http.MethodPost, path+"/publish", owner.Token, map[string]any{"expectedRevision": template.Revision}, 201)
	subPath := fmt.Sprintf("/api/my/subscriptions/%d", subscription.ID)
	harness.doRaw(http.MethodPut, subPath, person.Token, map[string]any{"expectedRevision": 1, "format": "sing-box", "templateId": template.ID}, 200)
	harness.expectError(http.MethodPut, subPath, person.Token, map[string]any{"expectedRevision": 1, "format": "sing-box", "templateId": template.ID}, 409, "revision_conflict")
	overridePath := fmt.Sprintf("%s/overrides/%d/inbounds/%d", subPath, node.ID, inboundID)
	harness.expectError(http.MethodPut, overridePath, person.Token, map[string]any{"expectedRevision": 0, "params": map[string]any{"uuid": "synthetic-forbidden"}}, 422, "override_field_forbidden")
	harness.doRaw(http.MethodPut, overridePath, person.Token, map[string]any{"expectedRevision": 0, "displayName": "my entry", "sortOrder": 0, "proxyGroup": "main", "params": map[string]any{}}, 200)
	harness.expectError(http.MethodPut, overridePath, person.Token, map[string]any{"expectedRevision": 0, "params": map[string]any{}}, 409, "revision_conflict")
	listed := harness.doRaw(http.MethodGet, subPath+"/overrides", person.Token, nil, 200)
	if bytes.Contains(listed, []byte(serverPSK)) || bytes.Contains(listed, []byte(subscription.Token)) {
		t.Fatal("override list leaks credential")
	}
	preview := harness.doRaw(http.MethodPost, subPath+"/preview", person.Token, map[string]any{"overrides": []any{map[string]any{"nodeId": node.ID, "inboundId": inboundID, "displayName": "unsaved", "params": map[string]any{}}}}, 200)
	if bytes.Contains(preview, []byte(serverPSK)) || bytes.Contains(preview, []byte(subscription.Token)) || !bytes.Contains(preview, []byte("unsaved")) {
		t.Fatal("preview leaks secret or ignores unsaved changes")
	}
	content := harness.subscriptionBody(subscription.Token, "")
	if !bytes.Contains(content, []byte(serverPSK)) || !bytes.Contains(content, []byte("my entry")) || bytes.Contains(content, []byte("unsaved")) {
		t.Fatal("download not isolated from preview or missing client identity")
	}
	if err := runAcceptanceSingboxCheck(t, content); err != nil {
		t.Fatal("sing-box client check rejected generated document")
	}
	current := harness.doRaw(http.MethodGet, path, owner.Token, nil, 200)
	_ = json.Unmarshal(current, &template)
	harness.doRaw(http.MethodPut, path, owner.Token, map[string]any{"expectedRevision": template.Revision, "name": "client-template", "format": "sing-box", "definition": map[string]any{"schemaVersion": 1}}, 200)
	harness.expectError(http.MethodPost, path+"/publish", owner.Token, map[string]any{"expectedRevision": template.Revision + 1}, 409, "template_in_use")
	harness.doRaw(http.MethodDelete, overridePath, person.Token, map[string]any{"expectedRevision": 1}, 200)
	harness.doRaw(http.MethodPost, path+"/publish", owner.Token, map[string]any{"expectedRevision": template.Revision + 1}, 201)
}
