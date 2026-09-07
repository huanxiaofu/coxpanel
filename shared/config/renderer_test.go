package config

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSupportedProtocolsAndStableHash(t *testing.T) {
	node := NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        7,
		NodeName:      "synthetic-node",
		Version:       "legacy-version",
		UpdatedAt:     time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		Inbounds: []Inbound{
			{ID: 3, Name: "hy2", Protocol: "hysteria2", Role: "entry", Listen: "127.0.0.1", Port: 8443, Params: map[string]string{
				"password": "synthetic-hy2-password", "certificatePath": "/tmp/synthetic.crt", "keyPath": "/tmp/synthetic.key", "sni": "example.test",
			}},
			{ID: 1, Name: "reality", Protocol: "vless-reality", Role: "entry", Listen: "::1", Port: 443, Params: map[string]string{
				"uuid": "00000000-0000-4000-8000-000000000001", "sni": "example.test", "target": "example.test:443", "privateKey": "synthetic-private-key", "shortId": "0123456789abcdef",
			}},
			{ID: 2, Name: "ss2022", Protocol: "shadowsocks", Role: "landing", Listen: "127.0.0.1", Port: 8388, Params: map[string]string{
				"method": "2022-blake3-aes-128-gcm", "password": "AAAAAAAAAAAAAAAAAAAAAA==",
			}},
		},
		Credentials: []UserCredential{
			{UserID: 11, InboundID: 1, Name: "reality-user", Protocol: "vless-reality", UUID: "00000000-0000-4000-8000-000000000011"},
			{UserID: 12, InboundID: 3, Name: "hy2-user", Protocol: "hysteria2", Password: "synthetic-hy2-user-password"},
		},
	}

	first, err := Render(node)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if first.Version != first.SHA256 || first.Version != Hash(first.Content) {
		t.Fatalf("rendered hash metadata does not match content")
	}
	if first.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", first.SchemaVersion, SchemaVersion)
	}
	if strings.Contains(string(first.Content), "min_client_ver") || strings.Contains(string(first.Content), "minClientVer") {
		t.Fatalf("renderer emitted removed minClientVer field: %s", first.Content)
	}

	var decoded SingBoxConfig
	if err := json.Unmarshal(first.Content, &decoded); err != nil {
		t.Fatalf("rendered JSON is invalid: %v", err)
	}
	if len(decoded.Inbounds) != 3 {
		t.Fatalf("inbound count = %d, want 3", len(decoded.Inbounds))
	}
	if decoded.Inbounds[0].Type != "vless" || decoded.Inbounds[1].Type != "shadowsocks" || decoded.Inbounds[2].Type != "hysteria2" {
		t.Fatalf("inbounds were not deterministically sorted or rendered: %+v", decoded.Inbounds)
	}
	if decoded.Inbounds[0].TLS == nil || decoded.Inbounds[0].TLS.Reality == nil || decoded.Inbounds[0].TLS.Reality.Handshake.ServerPort != 443 {
		t.Fatalf("Reality fields are incomplete: %+v", decoded.Inbounds[0].TLS)
	}
	if len(decoded.Inbounds[0].Users) != 1 || decoded.Inbounds[0].Users[0].UUID != "00000000-0000-4000-8000-000000000011" {
		t.Fatalf("Reality users did not use the active credential roster: %+v", decoded.Inbounds[0].Users)
	}
	if decoded.Inbounds[1].Method != "2022-blake3-aes-128-gcm" || decoded.Inbounds[1].Password == "" {
		t.Fatalf("SS2022 fields are incomplete: %+v", decoded.Inbounds[1])
	}
	if decoded.Inbounds[2].TLS == nil || decoded.Inbounds[2].TLS.CertificatePath == "" || decoded.Inbounds[2].TLS.KeyPath == "" {
		t.Fatalf("HY2 TLS fields are incomplete: %+v", decoded.Inbounds[2].TLS)
	}
	if len(decoded.Inbounds[2].Users) != 1 || decoded.Inbounds[2].Users[0].Password != "synthetic-hy2-user-password" {
		t.Fatalf("HY2 users did not use the active credential roster: %+v", decoded.Inbounds[2].Users)
	}

	node.Version = "another-legacy-value"
	node.UpdatedAt = time.Now().UTC().Add(24 * time.Hour)
	second, err := Render(node)
	if err != nil {
		t.Fatalf("Render() with legacy metadata error = %v", err)
	}
	if !bytes.Equal(first.Content, second.Content) || first.Version != second.Version {
		t.Fatalf("legacy version/timestamp changed rendered identity")
	}
}

func TestRenderRejectsUnsupportedOrIncompleteProtocol(t *testing.T) {
	tests := []struct {
		name string
		node NodeConfig
	}{
		{
			name: "unsupported protocol",
			node: NodeConfig{NodeID: 1, Inbounds: []Inbound{{ID: 1, Protocol: "vmess", Port: 1000}}},
		},
		{
			name: "missing Reality fields",
			node: NodeConfig{NodeID: 1, Inbounds: []Inbound{{ID: 1, Protocol: "vless-reality", Port: 1000, Params: map[string]string{"uuid": "u"}}}},
		},
		{
			name: "missing HY2 TLS",
			node: NodeConfig{NodeID: 1, Inbounds: []Inbound{{ID: 1, Protocol: "hysteria2", Port: 1000, Params: map[string]string{"password": "p"}}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Render(test.node); err == nil {
				t.Fatalf("Render() error = nil, want explicit failure")
			}
		})
	}
}

func TestRenderManagedEntryDoesNotFallbackToSharedCredentials(t *testing.T) {
	tests := []struct {
		name    string
		inbound Inbound
	}{
		{
			name: "Reality",
			inbound: Inbound{ID: 1, Protocol: "vless-reality", Role: "entry", Port: 443, Params: map[string]string{
				"uuid": "shared-reality-uuid", "sni": "example.test", "target": "example.test:443", "privateKey": "synthetic-private-key", "shortId": "0123456789abcdef",
			}},
		},
		{
			name: "SS2022",
			inbound: Inbound{ID: 1, Protocol: "shadowsocks", Role: "entry", Port: 8388, Params: map[string]string{
				"method": "2022-blake3-aes-128-gcm", "password": "shared-ss-password",
			}},
		},
		{
			name: "Hysteria2",
			inbound: Inbound{ID: 1, Protocol: "hysteria2", Role: "entry", Port: 8443, Params: map[string]string{
				"password": "shared-hy2-password", "certificatePath": "/tmp/synthetic.crt", "keyPath": "/tmp/synthetic.key", "sni": "example.test",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Render(NodeConfig{SchemaVersion: SchemaVersion, NodeID: 1, Inbounds: []Inbound{test.inbound}})
			if err == nil {
				t.Fatal("Render() error = nil, want missing active credential roster failure")
			}
		})
	}
}

func TestRenderRuntimeRemovesEntryWithoutAuthorizedCredential(t *testing.T) {
	node := NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        18,
		Inbounds: []Inbound{{
			ID: 1, Protocol: "shadowsocks", Role: "entry", Listen: "127.0.0.1", Port: 8388,
			Params: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "server-psk"},
		}},
		Edges:    []Edge{},
		Outbound: "direct",
	}
	rendered, err := RenderRuntime(node)
	if err != nil {
		t.Fatalf("RenderRuntime() error = %v", err)
	}
	var decoded SingBoxConfig
	if err := json.Unmarshal(rendered.Content, &decoded); err != nil {
		t.Fatalf("runtime output is not JSON: %v", err)
	}
	if len(decoded.Inbounds) != 0 {
		t.Fatalf("runtime output retained revoked entry listeners: %+v", decoded.Inbounds)
	}
	if strings.Contains(string(rendered.Content), "server-psk") || strings.Contains(string(rendered.Content), "disabled") {
		t.Fatalf("runtime output leaked or introduced a disabled credential: %s", rendered.Content)
	}
}

func TestRenderRuntimeRetainsAuthorizedEntryAndStrictRenderStillFails(t *testing.T) {
	node := NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        19,
		Inbounds: []Inbound{{
			ID: 1, Protocol: "shadowsocks", Role: "entry", Listen: "127.0.0.1", Port: 8388,
			Params: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "server-psk"},
		}},
		Credentials: []UserCredential{{
			UserID: 2, InboundID: 1, Name: "active", Protocol: "shadowsocks", Password: "active-credential",
		}},
		Edges:    []Edge{},
		Outbound: "direct",
	}
	if _, err := Render(node); err != nil {
		t.Fatalf("strict Render() rejected active credential: %v", err)
	}
	rendered, err := RenderRuntime(node)
	if err != nil {
		t.Fatalf("RenderRuntime() error = %v", err)
	}
	if !strings.Contains(string(rendered.Content), "active-credential") {
		t.Fatalf("runtime output omitted authorized credential: %s", rendered.Content)
	}
}

func TestRenderManagedEntryRetainsRequiredServerPSKWithRoster(t *testing.T) {
	rendered, err := Render(NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        1,
		Inbounds: []Inbound{{
			ID: 1, Protocol: "shadowsocks", Role: "entry", Port: 8388,
			Params: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "shared-password"},
		}},
		Credentials: []UserCredential{{
			UserID: 4, InboundID: 1, Name: "active-user", Protocol: "shadowsocks", Password: "active-password",
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var decoded SingBoxConfig
	if err := json.Unmarshal(rendered.Content, &decoded); err != nil {
		t.Fatalf("rendered JSON is invalid: %v", err)
	}
	if len(decoded.Inbounds) != 1 || decoded.Inbounds[0].Password != "shared-password" {
		t.Fatalf("required SS2022 server PSK was not retained: %+v", decoded.Inbounds)
	}
	if len(decoded.Inbounds[0].Users) != 1 || decoded.Inbounds[0].Users[0].Password != "active-password" {
		t.Fatalf("active SS2022 roster was not rendered: %+v", decoded.Inbounds[0].Users)
	}
}

func TestRenderManagedSS2022RejectsRosterWithoutServerPSK(t *testing.T) {
	_, err := Render(NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        1,
		Inbounds: []Inbound{{
			ID: 1, Protocol: "shadowsocks", Role: "entry", Port: 8388,
			Params: map[string]string{"method": "2022-blake3-aes-128-gcm"},
		}},
		Credentials: []UserCredential{{
			UserID: 4, InboundID: 1, Name: "active-user", Protocol: "shadowsocks", Password: "BBBBBBBBBBBBBBBBBBBBBB==",
		}},
	})
	if err == nil {
		t.Fatal("Render() error = nil, want missing SS2022 server PSK failure")
	}
	if !strings.Contains(err.Error(), "missing password") {
		t.Fatalf("Render() error = %q, want missing password context", err)
	}
}

func TestRenderManagedSS2022PassesPinnedSingBoxCheck(t *testing.T) {
	const singBoxPath = "/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box"
	if _, err := os.Stat(singBoxPath); err != nil {
		t.Skip("pinned sing-box binary is unavailable")
	}
	rendered, err := Render(NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        1,
		Inbounds: []Inbound{{
			ID: 1, Protocol: "shadowsocks", Role: "entry", Listen: "127.0.0.1", Port: 8388,
			Params: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "AAAAAAAAAAAAAAAAAAAAAA=="},
		}},
		Credentials: []UserCredential{{
			UserID: 4, InboundID: 1, Name: "synthetic-user", Protocol: "shadowsocks", Password: "BBBBBBBBBBBBBBBBBBBBBB==",
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "sing-box.json")
	if err := os.WriteFile(configPath, rendered.Content, 0600); err != nil {
		t.Fatalf("write synthetic sing-box config: %v", err)
	}
	command := exec.Command(singBoxPath, "check", "-c", configPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		t.Fatalf("pinned sing-box rejected rendered SS2022 roster: %v", err)
	}
}

func TestRenderRedactedRecursivelyRemovesSecretAndIdentityFields(t *testing.T) {
	node := NodeConfig{
		SchemaVersion: SchemaVersion,
		NodeID:        9,
		NodeName:      "redaction-node",
		Inbounds: []Inbound{
			{ID: 1, Name: "reality-entry", Protocol: "vless-reality", Role: "entry", Listen: "::", Port: 443, Params: map[string]string{
				"uuid": "entry-uuid-secret", "sni": "example.test", "target": "example.test:443",
				"privateKey": "entry-private-key-secret", "shortId": "entry-short-id-secret",
			}},
			{ID: 2, Name: "ss-landing", Protocol: "shadowsocks", Role: "landing", Listen: "::", Port: 8388, Params: map[string]string{
				"method": "2022-blake3-aes-128-gcm", "password": "landing-password-secret",
			}},
			{ID: 3, Name: "hy2-landing", Protocol: "hysteria2", Role: "landing", Listen: "::", Port: 8443, Params: map[string]string{
				"password": "hy2-password-secret", "obfsPassword": "obfs-password-secret",
				"certificatePath": "/secrets/certificate.pem", "keyPath": "/secrets/private-key.pem",
			}},
		},
		Edges: []Edge{{
			ID: 1, FromInboundID: 1, ToNodeID: 10, ToInboundID: 4, ToServer: "landing.example.test", ToPort: 443, ToProtocol: "vless-reality",
			ToParams: map[string]string{"uuid": "edge-uuid-secret", "sni": "landing.example.test", "publicKey": "edge-public-key-secret", "shortId": "edge-short-id-secret"},
		}},
		Credentials: []UserCredential{{UserID: 42, InboundID: 1, Name: "alice", Protocol: "vless-reality", UUID: "user-uuid-secret"}},
	}

	rendered, err := RenderRedacted(node)
	if err != nil {
		t.Fatalf("RenderRedacted() error = %v", err)
	}
	if rendered.Version != Hash(rendered.Content) || rendered.SHA256 != rendered.Version {
		t.Fatalf("redacted hash metadata does not match content")
	}
	var value any
	if err := json.Unmarshal(rendered.Content, &value); err != nil {
		t.Fatalf("redacted output is not JSON: %v", err)
	}
	assertNoRedactedPreviewKeys(t, value)
	for _, secret := range []string{
		"entry-uuid-secret", "entry-private-key-secret", "entry-short-id-secret", "landing-password-secret",
		"hy2-password-secret", "obfs-password-secret", "/secrets/certificate.pem", "/secrets/private-key.pem",
		"edge-uuid-secret", "edge-public-key-secret", "edge-short-id-secret", "user-uuid-secret", "alice",
	} {
		if strings.Contains(string(rendered.Content), secret) {
			t.Fatalf("redacted output leaked %q: %s", secret, rendered.Content)
		}
	}
}

func TestRedactRenderedRecursesThroughUnknownObjects(t *testing.T) {
	content, err := redactRendered([]byte(`{"outer":{"privateKey":"private","public_key":"public","shortId":"short","uuid":"uuid","password":"password","users":[{"name":"alice","credential":"credential"}],"nested":[{"key_path":"/secret/key","certificate_path":"/secret/cert","access_token":"access-token","user_uuid":"user-uuid","private_key_file":"private-file","client_secret":"client-secret","api_key":"api-key"}]}}`))
	if err != nil {
		t.Fatalf("redactRendered() error = %v", err)
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		t.Fatalf("redacted nested output is not JSON: %v", err)
	}
	assertNoRedactedPreviewKeys(t, value)
	for _, secret := range []string{"private", "public", "short", "uuid", "password", "alice", "credential", "/secret/key", "/secret/cert", "access-token", "user-uuid", "private-file", "client-secret", "api-key"} {
		if strings.Contains(string(content), secret) {
			t.Fatalf("nested redacted output leaked %q: %s", secret, content)
		}
	}
}

func assertNoRedactedPreviewKeys(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isRedactedPreviewKey(key) {
				t.Fatalf("redacted output retained sensitive key %q", key)
			}
			assertNoRedactedPreviewKeys(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoRedactedPreviewKeys(t, child)
		}
	}
}
