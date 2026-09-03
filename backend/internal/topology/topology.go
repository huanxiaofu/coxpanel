// Package topology 把画布图（入站+连线）翻译成 sing-box 配置。
package topology

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/coxpanel/backend/internal/models"
)

// SingboxConfig 生成的 sing-box 配置（结构与 sing-box schema 对齐）。
type SingboxConfig struct {
	Log       *LogConfig    `json:"log,omitempty"`
	Inbounds  []SBInbound   `json:"inbounds"`
	Outbounds []SBOutbound  `json:"outbounds"`
	Route     *SBRoute      `json:"route,omitempty"`
}

// LogConfig 日志。
type LogConfig struct {
	Level  string `json:"level"`
	Output string `json:"output,omitempty"`
}

// SBInbound sing-box 入站。
type SBInbound struct {
	Type       string         `json:"type"` // vless / shadowsocks / hysteria2
	Tag        string         `json:"tag"`
	Listen     string         `json:"listen,omitempty"`
	ListenPort int            `json:"listen_port"`
	Users      []SBVLESSUser  `json:"users,omitempty"`
	TLS        *SBInboundTLS  `json:"tls,omitempty"`
	Method     string         `json:"method,omitempty"`     // ss
	Password   string         `json:"password,omitempty"`   // ss
	UpMbps     int            `json:"up_mbps,omitempty"`    // hy2
	DownMbps   int            `json:"down_mbps,omitempty"`  // hy2
	Obfs       *SBHysteriaObfs `json:"obfs,omitempty"`      // hy2
}

// SBVLESSUser vless 用户。
type SBVLESSUser struct {
	UUID string `json:"uuid"`
	Flow string `json:"flow,omitempty"`
}

// SBInboundTLS 入站 TLS/Reality。
type SBInboundTLS struct {
	Enabled    bool        `json:"enabled"`
	ServerName string      `json:"server_name,omitempty"`
	Reality    *SBReality  `json:"reality,omitempty"`
	ALPN       []string    `json:"alpn,omitempty"`
}

// SBReality Reality 配置。
type SBReality struct {
	Enabled    bool     `json:"enabled"`
	PrivateKey string   `json:"private_key"`
	ShortID    []string `json:"short_id,omitempty"`
	Handshake  *SBHandshake `json:"handshake,omitempty"`
	MinClientVer string `json:"min_client_ver,omitempty"`
}

// SBHandshake Reality 伪装目标。
type SBHandshake struct {
	Server string `json:"server"`
	ServerName string `json:"server_name"`
}

// SBHysteriaObfs hy2 混淆。
type SBHysteriaObfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}

// SBOutbound sing-box 出站。
type SBOutbound struct {
	Type       string         `json:"type"` // direct / shadowsocks / hysteria2
	Tag        string         `json:"tag"`
	Server     string         `json:"server,omitempty"`
	ServerPort int            `json:"server_port,omitempty"`
	Method     string         `json:"method,omitempty"`     // ss
	Password   string         `json:"password,omitempty"`   // ss
	UpMbps     int            `json:"up_mbps,omitempty"`    // hy2
	DownMbps   int            `json:"down_mbps,omitempty"`  // hy2
	Obfs       *SBHysteriaObfs `json:"obfs,omitempty"`      // hy2
	TLS        *SBOutboundTLS `json:"tls,omitempty"`        // hy2 sni
}

// SBOutboundTLS 出站 TLS（hy2 sni）。
type SBOutboundTLS struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name,omitempty"`
	Insecure   bool   `json:"insecure,omitempty"`
}

// SBRoute 路由。
type SBRoute struct {
	Final string    `json:"final"`
	Rules []SBRule  `json:"rules,omitempty"`
}

// SBRule 路由规则。
type SBRule struct {
	Inbound  []string `json:"inbound,omitempty"`
	Outbound string   `json:"outbound"`
}

// Edge 拓扑连线（与 DB edges 表对应 + 落地参数）。
type Edge struct {
	FromInboundID int64
	ToNodeID      int64
	ToInboundID   int64
	ToServer      string
	ToPort        int
	ToProtocol    string
	ToParams      map[string]string
}

// Build 生成 sing-box 配置。
// P1 范围：单节点视角（入站 + 出站直连/落地）。
// inbounds: 本节点入站；edges: 连线；默认出站 direct。
func Build(node *models.Node, inbounds []models.Inbound, edges []Edge) (*SingboxConfig, error) {
	cfg := &SingboxConfig{
		Log:       &LogConfig{Level: "warn"},
		Inbounds:  []SBInbound{},
		Outbounds: []SBOutbound{{Type: "direct", Tag: "direct"}},
		Route:     &SBRoute{Final: "direct", Rules: []SBRule{}},
	}

	tagByID := map[int64]string{}
	for _, ib := range inbounds {
		tag := fmt.Sprintf("in-%d-%s", ib.ID, ib.Protocol)
		tagByID[ib.ID] = tag
		sb, err := inboundToSingbox(ib, tag)
		if err != nil {
			return nil, err
		}
		cfg.Inbounds = append(cfg.Inbounds, *sb)
	}

	// 连线 → 出站 + 路由规则
	for _, e := range edges {
		fromTag, ok := tagByID[e.FromInboundID]
		if !ok {
			continue
		}
		obTag := fmt.Sprintf("to-%d-%s", e.ToInboundID, e.ToProtocol)
		cfg.Outbounds = append(cfg.Outbounds, edgeToOutbound(e, obTag))
		cfg.Route.Rules = append(cfg.Route.Rules, SBRule{Inbound: []string{fromTag}, Outbound: obTag})
	}

	return cfg, nil
}

// inboundToSingbox 入站模型 → sing-box inbound。
func inboundToSingbox(ib models.Inbound, tag string) (*SBInbound, error) {
	params := map[string]string{}
	if len(ib.Config) > 0 {
		_ = json.Unmarshal(ib.Config, &params)
	}
	sb := &SBInbound{
		Tag:        tag,
		Listen:     ib.ListenAddr,
		ListenPort: ib.ListenPort,
	}
	switch ib.Protocol {
	case "vless-reality":
		sb.Type = "vless"
		sb.Users = []SBVLESSUser{{UUID: params["uuid"], Flow: firstNonEmpty(params["flow"], "xtls-rprx-vision")}}
		sb.TLS = &SBInboundTLS{
			Enabled:    true,
			ServerName: params["sni"],
			Reality: &SBReality{
				Enabled:      true,
				PrivateKey:   params["privateKey"],
				ShortID:      []string{params["shortId"]},
				MinClientVer: ib.MinClientVer,
				Handshake: &SBHandshake{
					Server:     params["target"],
					ServerName: params["sni"],
				},
			},
		}
	case "shadowsocks":
		sb.Type = "shadowsocks"
		sb.Method = firstNonEmpty(params["method"], "2022-blake3-aes-128-gcm")
		sb.Password = params["password"]
	case "hysteria2":
		sb.Type = "hysteria2"
		sb.Password = params["password"]
		sb.UpMbps = 100
		sb.DownMbps = 100
		sb.Obfs = &SBHysteriaObfs{Type: "salamander", Password: params["obfsPassword"]}
	default:
		return nil, fmt.Errorf("不支持的入站协议: %s", ib.Protocol)
	}
	return sb, nil
}

// edgeToOutbound 连线 → sing-box outbound。
func edgeToOutbound(e Edge, tag string) SBOutbound {
	ob := SBOutbound{Tag: tag, Server: e.ToServer, ServerPort: e.ToPort}
	switch e.ToProtocol {
	case "shadowsocks":
		ob.Type = "shadowsocks"
		ob.Method = firstNonEmpty(e.ToParams["method"], "2022-blake3-aes-128-gcm")
		ob.Password = e.ToParams["password"]
	case "hysteria2":
		ob.Type = "hysteria2"
		ob.Password = e.ToParams["password"]
		ob.UpMbps = 100
		ob.DownMbps = 100
		if sni := e.ToParams["sni"]; sni != "" {
			ob.TLS = &SBOutboundTLS{Enabled: true, ServerName: sni, Insecure: true}
		}
	default:
		ob.Type = "direct"
	}
	return ob
}

// Marshal 序列化。
func (c *SingboxConfig) Marshal() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// SortInbounds 按端口排序（保证输出稳定）。
func SortInbounds(ibs []SBInbound) {
	sort.Slice(ibs, func(i, j int) bool { return ibs[i].ListenPort < ibs[j].ListenPort })
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
