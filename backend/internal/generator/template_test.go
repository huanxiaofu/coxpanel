package generator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/coxpanel/backend/internal/models"
)

func TestGenerateSingBoxClientUsesProtocolSpecificOutbounds(t *testing.T) {
	proxies := []Proxy{
		{
			Name:       "Reality entry",
			Node:       &models.Node{ID: 1, Type: "managed", PublicIP: "reality.example"},
			Inbound:    &models.Inbound{ID: 11, Protocol: "vless-reality", Role: "entry", ListenPort: 443, Config: json.RawMessage(`{"sni":"site.example","publicKey":"public-key","shortId":"short-id"}`)},
			Credential: "user-uuid",
		},
		{
			Name:       "SS entry",
			Node:       &models.Node{ID: 2, Type: "managed", PublicIP: "ss.example"},
			Inbound:    &models.Inbound{ID: 12, Protocol: "shadowsocks", Role: "entry", ListenPort: 8443, Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"server-psk"}`)},
			Credential: "user-psk",
		},
		{
			Name:       "HY2 entry",
			Node:       &models.Node{ID: 3, Type: "managed", PublicIP: "hy2.example"},
			Inbound:    &models.Inbound{ID: 13, Protocol: "hysteria2", Role: "entry", ListenPort: 2053, Config: json.RawMessage(`{"sni":"hy2.example","certificatePath":"server.crt","keyPath":"server.key"}`)},
			Credential: "user-password",
		},
	}

	body, err := GenerateSingBox(proxies, "all-nodes")
	if err != nil {
		t.Fatalf("GenerateSingBox() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("sing-box output is not JSON: %v", err)
	}
	outbounds, ok := document["outbounds"].([]any)
	if !ok || len(outbounds) != 5 {
		t.Fatalf("outbounds = %#v, want three proxies, selector, and direct", document["outbounds"])
	}
	output := string(body)
	for _, value := range []string{"\"type\": \"vless\"", "\"type\": \"shadowsocks\"", "\"type\": \"hysteria2\"", "user-uuid", "user-psk", "user-password", "\"type\": \"selector\""} {
		if !strings.Contains(output, value) {
			t.Fatalf("sing-box output omitted %q:\n%s", value, body)
		}
	}
	if !strings.Contains(output, "server-psk:user-psk") {
		t.Fatalf("sing-box output omitted the SS2022 server/user credential chain:\n%s", body)
	}
}

func TestTemplateValidationRejectsUnsafeFields(t *testing.T) {
	unsafe := json.RawMessage(`{"schemaVersion":1,"defaults":{"params":{"uuid":"not-editable"}}}`)
	if err := ValidateTemplateDefinition("mihomo", unsafe); err == nil {
		t.Fatal("template identity field was accepted")
	}
	unknown := json.RawMessage(`{"schemaVersion":1,"notAllowed":true}`)
	if err := ValidateTemplateDefinition("sing-box", unknown); err == nil {
		t.Fatal("unknown template field was accepted")
	}
	valid := json.RawMessage(`{"schemaVersion":1,"defaults":{"displayNamePattern":"${node.name}-${inbound.name}"},"groups":[{"id":"main","name":"Main","type":"select","members":["$authorizedProxies","DIRECT"]}],"rules":[{"type":"MATCH","target":"group:main"}]}`)
	if err := ValidateTemplateDefinition("mihomo", valid); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
}

func TestGenerateSubscriptionPreviewRedactsCredentials(t *testing.T) {
	proxies := []Proxy{{
		Name:       "managed",
		Node:       &models.Node{ID: 1, Type: "managed", PublicIP: "node.example"},
		Inbound:    &models.Inbound{ID: 11, Protocol: "shadowsocks", Role: "entry", ListenPort: 8443, Config: json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"server-secret"}`)},
		Credential: "user-secret",
	}}
	body, err := GenerateSubscriptionPreview("sing-box", proxies, "preview", nil)
	if err != nil {
		t.Fatalf("GenerateSubscriptionPreview() error = %v", err)
	}
	output := string(body)
	if strings.Contains(output, "server-secret") || strings.Contains(output, "user-secret") {
		t.Fatalf("preview leaked credential material: %s", output)
	}
	if !strings.Contains(output, "credential") {
		t.Fatalf("preview did not contain an authentication placeholder: %s", output)
	}
}
