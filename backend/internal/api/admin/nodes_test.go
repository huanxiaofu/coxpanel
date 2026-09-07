package admin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateInboundRejectsInvalidProtocolMaterialBeforePersistence(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		role     string
		config   string
	}{
		{
			name:     "Reality target port above range",
			protocol: "vless-reality",
			role:     "entry",
			config:   `{"sni":"example.test","privateKey":"synthetic-private","shortId":"0123456789abcdef","target":"example.test","targetPort":"65536"}`,
		},
		{
			name:     "Reality target port below range",
			protocol: "vless-reality",
			role:     "entry",
			config:   `{"sni":"example.test","privateKey":"synthetic-private","shortId":"0123456789abcdef","target":"example.test","targetPort":"0"}`,
		},
		{
			name:     "HY2 negative bandwidth",
			protocol: "hysteria2",
			role:     "entry",
			config:   `{"certificatePath":"/synthetic/cert","keyPath":"/synthetic/key","upMbps":"-1"}`,
		},
		{
			name:     "trailing JSON",
			protocol: "shadowsocks",
			role:     "entry",
			config:   `{"method":"2022-blake3-aes-128-gcm","password":"synthetic-server-psk"}{"unexpected":true}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateInbound(test.protocol, test.role, json.RawMessage(test.config))
			if err == nil {
				t.Fatal("validateInbound() error = nil, want invalid protocol material rejection")
			}
		})
	}
}

func TestValidateInboundAcceptsSupportedP1ProtocolRoles(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		role     string
		config   string
	}{
		{
			name:     "Reality entry",
			protocol: "vless-reality",
			role:     "entry",
			config:   `{"sni":"example.test","privateKey":"synthetic-private","shortId":"0123456789abcdef","target":"example.test:443"}`,
		},
		{
			name:     "Reality landing",
			protocol: "vless-reality",
			role:     "landing",
			config:   `{"uuid":"00000000-0000-4000-8000-000000000001","sni":"example.test","privateKey":"synthetic-private","shortId":"0123456789abcdef","target":"example.test:443"}`,
		},
		{
			name:     "SS2022 entry",
			protocol: "shadowsocks",
			role:     "entry",
			config:   `{"method":"2022-blake3-aes-128-gcm","password":"synthetic-server-psk"}`,
		},
		{
			name:     "HY2 entry",
			protocol: "hysteria2",
			role:     "entry",
			config:   `{"certificatePath":"/synthetic/cert","keyPath":"/synthetic/key"}`,
		},
		{
			name:     "HY2 landing",
			protocol: "hysteria2",
			role:     "landing",
			config:   `{"certificatePath":"/synthetic/cert","keyPath":"/synthetic/key","password":"synthetic-landing-password"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateInbound(test.protocol, test.role, json.RawMessage(test.config)); err != nil {
				t.Fatalf("validateInbound() error = %v, want supported protocol accepted", err)
			}
		})
	}
}

func TestDecodeProtocolParamsRejectsTrailingJSONAndNonObjects(t *testing.T) {
	for name, raw := range map[string]string{
		"trailing value":  `{"server":"example.test"}{"server":"other.test"}`,
		"trailing scalar": `{"server":"example.test"} true`,
		"array":           `[{"server":"example.test"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocolParams(json.RawMessage(raw)); err == nil {
				t.Fatal("decodeProtocolParams() error = nil, want rejected JSON")
			}
		})
	}
}

func TestValidateNodeProtocolRejectsUnsupportedExternalProtocolAndPort(t *testing.T) {
	config := json.RawMessage(`{"server":"example.test","port":443,"password":"synthetic"}`)
	if err := validateNodeProtocol("external", "unsupported", config); err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("unsupported external protocol error = %v, want explicit rejection", err)
	}
	for _, port := range []string{"0", "65536", `"443"`} {
		raw := json.RawMessage(`{"server":"example.test","port":` + port + `,"password":"synthetic"}`)
		if err := validateNodeProtocol("external", "shadowsocks", raw); err == nil {
			t.Fatalf("external port %s was accepted", port)
		}
	}
}
