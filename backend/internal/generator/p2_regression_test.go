package generator

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/coxpanel/backend/internal/models"
)

func TestP2TemplateRejectsUnsafeRulesAndFingerprints(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":1,"rules":[{"type":"DOMAIN","value":"example.test,DIRECT","target":"DIRECT"}]}`, `{"schemaVersion":1,"rules":[{"type":"IP-CIDR","value":"bad-network","target":"DIRECT"}]}`, `{"schemaVersion":1,"defaults":{"params":{"fingerprint":"arbitrary"}}}`} {
		if ValidateTemplateDefinition("mihomo", json.RawMessage(raw)) == nil {
			t.Fatal("unsafe template accepted")
		}
	}
	if ValidateTemplateDefinition("mihomo", json.RawMessage(`{"schemaVersion":1,"defaults":{"params":{"sni":""}}}`)) != nil {
		t.Fatal("empty SNI must mean inheritance")
	}
}

func TestP2PreviewRedactsNestedAndOverrideObfuscation(t *testing.T) {
	proxies := []Proxy{{Name: "hy2", Node: &models.Node{ID: 1, Type: "managed", PublicIP: "127.0.0.1"}, Inbound: &models.Inbound{ID: 1, Role: "entry", Protocol: "hysteria2", ListenPort: 443, Config: json.RawMessage(`{"obfs":{"type":"salamander","password":"nested-test-secret"}}`)}, Credential: "test-user-secret", Override: &OverrideData{Params: map[string]any{"obfs": "salamander", "obfsPassword": "override-test-secret"}}}}
	for _, format := range []string{"mihomo", "sing-box"} {
		body, err := GenerateSubscriptionPreview(format, proxies, "preview", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"nested-test-secret", "test-user-secret", "override-test-secret"} {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatal("preview leaked secret")
			}
		}
	}
}

func TestP2EmptySingBoxSelectorAndRejectRoute(t *testing.T) {
	definition := json.RawMessage(`{"schemaVersion":1,"groups":[{"id":"main","name":"main","type":"select","members":["$authorizedProxies","DIRECT"]}],"rules":[{"type":"DOMAIN","value":"blocked.test","target":"REJECT"},{"type":"MATCH","target":"DIRECT"}],"dns":{"enabled":false}}`)
	body, err := GenerateClient("sing-box", nil, "empty", definition)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"outbound": "DIRECT"`)) || bytes.Contains(body, []byte(`"outbound": "REJECT"`)) || bytes.Contains(body, []byte(`"dns"`)) || !bytes.Contains(body, []byte(`"action": "reject"`)) {
		t.Fatal("invalid sing-box route or DNS")
	}
}
