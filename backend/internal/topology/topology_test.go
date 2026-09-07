package topology

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/coxpanel/backend/internal/models"
)

func TestBuildRealityPlusSS(t *testing.T) {
	node := &models.Node{ID: 1, Name: "SG", Type: "managed", PublicIP: "45.89.219.222"}

	realityCfg := `{"uuid":"e1706bf2-bdff-4f3c-afcc-cc4bb6a0d8d8","sni":"xtom.com","target":"xtom.com:443","privateKey":"PKEY","shortId":"5bdb8d7b6c228767","flow":"xtls-rprx-vision"}`
	ssCfg := `{"method":"2022-blake3-aes-128-gcm","password":"SSPASS"}`

	inbounds := []models.Inbound{
		{ID: 1, NodeID: 1, Name: "reality-entry", Protocol: "vless-reality", Role: "entry", ListenAddr: "::", ListenPort: 5443, Config: json.RawMessage(realityCfg), MinClientVer: "1.8.2"},
		{ID: 2, NodeID: 1, Name: "ss-landing", Protocol: "shadowsocks", Role: "landing", ListenAddr: "::", ListenPort: 8388, Config: json.RawMessage(ssCfg)},
	}

	cfg, err := Build(node, inbounds, nil)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(cfg.Inbounds) != 2 {
		t.Fatalf("期望 2 个入站，得到 %d", len(cfg.Inbounds))
	}
	// 校验 reality 入站
	r := cfg.Inbounds[0]
	if r.Type != "vless" || r.ListenPort != 5443 {
		t.Errorf("reality 入站错误: %+v", r)
	}
	if r.TLS == nil || r.TLS.Reality == nil || r.TLS.Reality.Handshake.ServerPort != 443 {
		t.Errorf("Reality handshake 配置缺失: %+v", r.TLS)
	}
	if r.TLS.Reality.Handshake.Server != "xtom.com" {
		t.Errorf("Reality handshake 目标错误: %+v", r.TLS.Reality.Handshake)
	}
	// 校验 ss 入站
	s := cfg.Inbounds[1]
	if s.Type != "shadowsocks" || s.ListenPort != 8388 {
		t.Errorf("ss 入站错误: %+v", s)
	}

	// 校验序列化
	out, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if len(out) < 100 {
		t.Errorf("输出过短: %s", out)
	}
	if strings.Contains(string(out), "min_client_ver") {
		t.Errorf("输出包含已移除的 min_client_ver 字段: %s", out)
	}
	t.Logf("生成配置:\n%s", out)
}

func TestBuildWithEdge(t *testing.T) {
	node := &models.Node{ID: 1, Name: "SG", Type: "managed", PublicIP: "45.89.219.222"}
	realityCfg := `{"uuid":"u1","sni":"xtom.com","target":"xtom.com:443","privateKey":"PK","shortId":"sid","flow":"xtls-rprx-vision"}`
	inbounds := []models.Inbound{
		{ID: 1, NodeID: 1, Name: "entry", Protocol: "vless-reality", Role: "entry", ListenAddr: "::", ListenPort: 5443, Config: json.RawMessage(realityCfg), MinClientVer: "0.0.0"},
	}
	edges := []Edge{
		{
			FromInboundID: 1, ToNodeID: 2, ToInboundID: 5,
			ToServer: "10.14.14.30", ToPort: 8388,
			ToProtocol: "shadowsocks", ToParams: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "P2"},
		},
	}
	cfg, err := Build(node, inbounds, edges)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(cfg.Outbounds) != 2 { // direct + 落地
		t.Fatalf("期望 2 个出站，得到 %d", len(cfg.Outbounds))
	}
	if cfg.Outbounds[1].Type != "shadowsocks" || cfg.Outbounds[1].Server != "10.14.14.30" {
		t.Errorf("落地出站错误: %+v", cfg.Outbounds[1])
	}
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Outbound != cfg.Outbounds[1].Tag {
		t.Errorf("路由规则错误: %+v", cfg.Route.Rules)
	}
}
