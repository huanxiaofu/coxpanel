// Package generator 生成客户端订阅内容。
package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/coxpanel/backend/internal/models"
)

// Proxy 一条订阅条目（节点）。
type Proxy struct {
	Name       string          // 显示名（可被覆写）
	Node       *models.Node    // 来源节点
	Inbound    *models.Inbound // 受管节点的 entry 入站；外部节点为 nil
	Override   *OverrideData   // 用户覆写
}

// OverrideData 用户覆写数据。
type OverrideData struct {
	DisplayName string
	SortOrder   int
	Icon        string
	Params      map[string]any // 覆写参数
	ProxyGroup  string
}

// GenerateMihomo 生成 mihomo YAML。
func GenerateMihomo(proxies []Proxy, subName string) ([]byte, error) {
	sort.SliceStable(proxies, func(i, j int) bool {
		a, b := proxies[i].Override, proxies[j].Override
		ai, bi := 0, 0
		if a != nil {
			ai = a.SortOrder
		}
		if b != nil {
			bi = b.SortOrder
		}
		return ai < bi
	})

	var buf bytes.Buffer
	buf.WriteString("# Coxpanel 订阅\n")
	buf.WriteString("# 更新订阅即同步，勿在本文件手动修改\n\n")
	buf.WriteString("proxies:\n")

	var names []string
	for _, p := range proxies {
		name := p.Name
		if p.Override != nil && p.Override.DisplayName != "" {
			name = p.Override.DisplayName
		}
		names = append(names, name)

		if p.Node.Type == "external" {
			if err := writeExternalMihomo(&buf, name, p); err != nil {
				return nil, err
			}
			continue
		}
		if p.Inbound == nil {
			continue
		}
		if err := writeInboundMihomo(&buf, name, p); err != nil {
			return nil, err
		}
	}

	buf.WriteString("\nproxy-groups:\n")
	buf.WriteString("  - name: " + subName + "\n")
	buf.WriteString("    type: select\n")
	buf.WriteString("    proxies:\n")
	for _, n := range names {
		buf.WriteString("      - " + n + "\n")
	}
	buf.WriteString("\nrules:\n")
	buf.WriteString("  - MATCH," + subName + "\n")
	return buf.Bytes(), nil
}

// writeInboundMihomo 输出受管节点 entry 入站。
func writeInboundMihomo(buf *bytes.Buffer, name string, p Proxy) error {
	ib := p.Inbound
	cfg := map[string]any{}
	if len(ib.Config) > 0 {
		_ = json.Unmarshal(ib.Config, &cfg)
	}
	params := mergeParams(cfg, p.Override)

	server := p.Node.PublicIP
	if server == "" {
		server = p.Node.EasyIP
	}
	port := ib.ListenPort
	if v, ok := intParam(params, "port"); ok {
		port = v
	}

	switch ib.Protocol {
	case "vless-reality":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: vless\n")
		fmt.Fprintf(buf, "    server: %s\n", server)
		fmt.Fprintf(buf, "    port: %d\n", port)
		fmt.Fprintf(buf, "    uuid: %s\n", strParam(params, "uuid"))
		fmt.Fprintf(buf, "    network: tcp\n")
		fmt.Fprintf(buf, "    tls: true\n")
		fmt.Fprintf(buf, "    udp: true\n")
		fmt.Fprintf(buf, "    flow: %s\n", strParamDef(params, "flow", "xtls-rprx-vision"))
		fmt.Fprintf(buf, "    servername: %s\n", strParam(params, "sni"))
		fmt.Fprintf(buf, "    reality-opts:\n")
		fmt.Fprintf(buf, "      public-key: %s\n", strParam(params, "publicKey"))
		fmt.Fprintf(buf, "      short-id: %s\n", strParamDef(params, "shortId", ""))
		fmt.Fprintf(buf, "    client-fingerprint: %s\n", strParamDef(params, "fingerprint", "chrome"))
	case "shadowsocks":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: ss\n")
		fmt.Fprintf(buf, "    server: %s\n", server)
		fmt.Fprintf(buf, "    port: %d\n", port)
		fmt.Fprintf(buf, "    cipher: %s\n", strParamDef(params, "method", "2022-blake3-aes-128-gcm"))
		fmt.Fprintf(buf, "    password: %s\n", strParam(params, "password"))
	case "hysteria2":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: hysteria2\n")
		fmt.Fprintf(buf, "    server: %s\n", server)
		fmt.Fprintf(buf, "    port: %d\n", port)
		fmt.Fprintf(buf, "    password: %s\n", strParam(params, "password"))
		if sni := strParam(params, "sni"); sni != "" {
			fmt.Fprintf(buf, "    sni: %s\n", sni)
		}
		fmt.Fprintf(buf, "    skip-cert-verify: true\n")
	default:
		return fmt.Errorf("不支持的协议: %s", ib.Protocol)
	}
	return nil
}

// writeExternalMihomo 输出外部节点（协议参数全部来自 ext_params）。
func writeExternalMihomo(buf *bytes.Buffer, name string, p Proxy) error {
	var ext map[string]any
	if len(p.Node.ExtParams) > 0 {
		_ = json.Unmarshal(p.Node.ExtParams, &ext)
	}
	if ext == nil {
		ext = map[string]any{}
	}
	proto := p.Node.ExtProtocol
	if proto == "" {
		proto = strParam(ext, "protocol")
	}
	switch proto {
	case "vless-reality", "vless":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: vless\n")
		fmt.Fprintf(buf, "    server: %s\n", strParam(ext, "server"))
		fmt.Fprintf(buf, "    port: %s\n", strParam(ext, "port"))
		fmt.Fprintf(buf, "    uuid: %s\n", strParam(ext, "uuid"))
		fmt.Fprintf(buf, "    network: tcp\n")
		fmt.Fprintf(buf, "    tls: true\n")
		fmt.Fprintf(buf, "    udp: true\n")
		fmt.Fprintf(buf, "    flow: %s\n", strParamDef(ext, "flow", "xtls-rprx-vision"))
		fmt.Fprintf(buf, "    servername: %s\n", strParam(ext, "sni"))
		fmt.Fprintf(buf, "    reality-opts:\n")
		fmt.Fprintf(buf, "      public-key: %s\n", strParam(ext, "publicKey"))
		fmt.Fprintf(buf, "      short-id: %s\n", strParamDef(ext, "shortId", ""))
		fmt.Fprintf(buf, "    client-fingerprint: %s\n", strParamDef(ext, "fingerprint", "chrome"))
	case "shadowsocks", "ss":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: ss\n")
		fmt.Fprintf(buf, "    server: %s\n", strParam(ext, "server"))
		fmt.Fprintf(buf, "    port: %s\n", strParam(ext, "port"))
		fmt.Fprintf(buf, "    cipher: %s\n", strParamDef(ext, "method", "2022-blake3-aes-128-gcm"))
		fmt.Fprintf(buf, "    password: %s\n", strParam(ext, "password"))
	case "hysteria2", "hy2":
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    type: hysteria2\n")
		fmt.Fprintf(buf, "    server: %s\n", strParam(ext, "server"))
		fmt.Fprintf(buf, "    port: %s\n", strParam(ext, "port"))
		fmt.Fprintf(buf, "    password: %s\n", strParam(ext, "password"))
		if sni := strParam(ext, "sni"); sni != "" {
			fmt.Fprintf(buf, "    sni: %s\n", sni)
		}
		fmt.Fprintf(buf, "    skip-cert-verify: true\n")
	default:
		return fmt.Errorf("外部节点不支持协议: %s", proto)
	}
	return nil
}

// mergeParams 合并配置与覆写（覆写优先）。
func mergeParams(base map[string]any, ov *OverrideData) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	if ov != nil {
		for k, v := range ov.Params {
			out[k] = v
		}
	}
	return out
}

func strParam(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func strParamDef(m map[string]any, key, def string) string {
	if v := strParam(m, key); v != "" {
		return v
	}
	return def
}

func intParam(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

// EnsureTrailingNewline 兼容检查（占位，避免未用 import）。
var _ = strings.TrimSpace
