// Package generator 生成客户端订阅内容。
package generator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/models"
	"gopkg.in/yaml.v3"
)

// Proxy 一条订阅条目（节点）。
type Proxy struct {
	Name       string          // 显示名（可被覆写）
	Node       *models.Node    // 来源节点
	Inbound    *models.Inbound // 受管节点的 entry 入站；外部节点为 nil
	Credential string          // 请求用户在该入站上的独立凭据
	Override   *OverrideData   // 用户覆写
}

// OverrideData 用户覆写数据。
type OverrideData struct {
	DisplayName  string
	SortOrder    int
	SortOrderSet bool
	Icon         string
	Params       map[string]any // 覆写参数
	ProxyGroup   string
}

type mihomoDocument struct {
	Proxies []map[string]any `yaml:"proxies"`
	Groups  []mihomoGroup    `yaml:"proxy-groups"`
	Rules   []string         `yaml:"rules"`
}

type mihomoGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// GenerateMihomo 生成 mihomo YAML。
func GenerateMihomo(proxies []Proxy, subName string) ([]byte, error) {
	ordered := append([]Proxy(nil), proxies...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return proxySortOrder(ordered[i]) < proxySortOrder(ordered[j])
	})

	document := mihomoDocument{
		Proxies: make([]map[string]any, 0, len(ordered)),
		Groups:  []mihomoGroup{{Name: subName, Type: "select", Proxies: []string{}}},
		Rules:   []string{"MATCH," + subName},
	}
	for _, proxy := range ordered {
		name, rendered, include, err := renderMihomoProxy(proxy)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		document.Proxies = append(document.Proxies, rendered)
		document.Groups[0].Proxies = append(document.Groups[0].Proxies, name)
	}

	body, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal mihomo YAML: %w", err)
	}
	var output bytes.Buffer
	output.WriteString("# Coxpanel 订阅\n")
	output.WriteString("# 更新订阅即同步，勿在本文件手动修改\n\n")
	output.Write(body)
	return output.Bytes(), nil
}

func proxySortOrder(proxy Proxy) int {
	if proxy.Override != nil {
		return proxy.Override.SortOrder
	}
	return 0
}

func renderMihomoProxy(proxy Proxy) (string, map[string]any, bool, error) {
	if proxy.Node == nil {
		return "", nil, false, fmt.Errorf("proxy node is nil")
	}
	name := proxy.Name
	if proxy.Override != nil && proxy.Override.DisplayName != "" {
		name = proxy.Override.DisplayName
	}
	switch proxy.Node.Type {
	case "external":
		rendered, err := externalMihomoMap(name, proxy)
		return name, rendered, err == nil, err
	case "managed":
		if proxy.Inbound == nil || strings.TrimSpace(proxy.Credential) == "" {
			return "", nil, false, nil
		}
		rendered, err := inboundMihomoMap(name, proxy)
		return name, rendered, err == nil, err
	default:
		if proxy.Inbound != nil {
			if strings.TrimSpace(proxy.Credential) == "" {
				return "", nil, false, nil
			}
			rendered, err := inboundMihomoMap(name, proxy)
			return name, rendered, err == nil, err
		}
		return "", nil, false, fmt.Errorf("unsupported node type: %s", proxy.Node.Type)
	}
}

func inboundMihomoMap(name string, proxy Proxy) (map[string]any, error) {
	inbound := proxy.Inbound
	params, err := inboundParams(inbound.Config)
	if err != nil {
		return nil, err
	}
	params = mergeParams(params, proxy.Override)
	server := firstNonEmpty(strParam(params, "server"), proxy.Node.PublicIP, proxy.Node.EasyIP)
	port := inbound.ListenPort
	if value, ok := intParam(params, "port"); ok {
		port = value
	}
	if server == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid managed proxy server or port")
	}

	result := map[string]any{"name": name, "server": server, "port": port}
	switch inbound.Protocol {
	case "vless-reality":
		result["type"] = "vless"
		result["uuid"] = proxy.Credential
		result["network"] = "tcp"
		result["tls"] = true
		result["udp"] = true
		result["flow"] = strParamDef(params, "flow", "xtls-rprx-vision")
		result["servername"] = strParam(params, "sni")
		result["reality-opts"] = map[string]any{
			"public-key": strParam(params, "publicKey"),
			"short-id":   strParamDef(params, "shortId", ""),
		}
		result["client-fingerprint"] = strParamDef(params, "fingerprint", "chrome")
	case "shadowsocks":
		result["type"] = "ss"
		result["cipher"] = strParamDef(params, "method", "2022-blake3-aes-128-gcm")
		password, err := ss2022SubscriptionCredential(params, proxy.Credential)
		if err != nil {
			return nil, err
		}
		result["password"] = password
	case "hysteria2":
		result["type"] = "hysteria2"
		result["password"] = proxy.Credential
		if sni := strParam(params, "sni"); sni != "" {
			result["sni"] = sni
		}
		result["skip-cert-verify"] = boolParamDef(params, "insecure", false)
		obfs, obfsPassword := hysteriaObfs(params)
		if obfs != "" {
			result["obfs"] = obfs
		}
		if obfsPassword != "" {
			result["obfs-password"] = obfsPassword
		}
		addHysteriaBandwidth(result, params)
	default:
		return nil, fmt.Errorf("不支持的协议: %s", inbound.Protocol)
	}
	return result, nil
}

func externalMihomoMap(name string, proxy Proxy) (map[string]any, error) {
	params, err := inboundParams(proxy.Node.ExtParams)
	if err != nil {
		return nil, err
	}
	params = mergeParams(params, proxy.Override)
	protocol := proxy.Node.ExtProtocol
	if protocol == "" {
		protocol = strParam(params, "protocol")
	}
	server := strParam(params, "server")
	port, ok := intParam(params, "port")
	if server == "" || !ok || port < 1 || port > 65535 {
		return nil, fmt.Errorf("外部节点缺少有效的 server/port")
	}
	result := map[string]any{"name": name, "server": server, "port": port}
	switch protocol {
	case "vless-reality", "vless":
		result["type"] = "vless"
		result["uuid"] = strParam(params, "uuid")
		result["network"] = "tcp"
		result["tls"] = true
		result["udp"] = true
		result["flow"] = strParamDef(params, "flow", "xtls-rprx-vision")
		result["servername"] = strParam(params, "sni")
		result["reality-opts"] = map[string]any{
			"public-key": strParam(params, "publicKey"),
			"short-id":   strParamDef(params, "shortId", ""),
		}
		result["client-fingerprint"] = strParamDef(params, "fingerprint", "chrome")
	case "shadowsocks", "ss":
		result["type"] = "ss"
		result["cipher"] = strParamDef(params, "method", "2022-blake3-aes-128-gcm")
		result["password"] = strParam(params, "password")
	case "hysteria2", "hy2":
		result["type"] = "hysteria2"
		result["password"] = strParam(params, "password")
		if sni := strParam(params, "sni"); sni != "" {
			result["sni"] = sni
		}
		result["skip-cert-verify"] = boolParamDef(params, "insecure", false)
		obfs, obfsPassword := hysteriaObfs(params)
		if obfs != "" {
			result["obfs"] = obfs
		}
		if obfsPassword != "" {
			result["obfs-password"] = obfsPassword
		}
		addHysteriaBandwidth(result, params)
	default:
		return nil, fmt.Errorf("外部节点不支持协议: %s", protocol)
	}
	return result, nil
}

// GenerateURIList generates one standard protocol URI per usable proxy.
func GenerateURIList(proxies []Proxy) ([]byte, error) {
	ordered := append([]Proxy(nil), proxies...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return proxySortOrder(ordered[i]) < proxySortOrder(ordered[j])
	})
	lines := make([]string, 0, len(ordered))
	for _, proxy := range ordered {
		line, include, err := proxyURI(proxy)
		if err != nil {
			return nil, err
		}
		if include {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func proxyURI(proxy Proxy) (string, bool, error) {
	if proxy.Node == nil {
		return "", false, fmt.Errorf("proxy node is nil")
	}
	name := proxy.Name
	if proxy.Override != nil && proxy.Override.DisplayName != "" {
		name = proxy.Override.DisplayName
	}
	if proxy.Node.Type == "external" {
		return externalURI(proxy, name)
	}
	if proxy.Inbound == nil || strings.TrimSpace(proxy.Credential) == "" {
		return "", false, nil
	}
	return managedURI(proxy, name)
}

func managedURI(proxy Proxy, name string) (string, bool, error) {
	params, err := inboundParams(proxy.Inbound.Config)
	if err != nil {
		return "", false, err
	}
	params = mergeParams(params, proxy.Override)
	server := firstNonEmpty(strParam(params, "server"), proxy.Node.PublicIP, proxy.Node.EasyIP)
	port := proxy.Inbound.ListenPort
	if value, ok := intParam(params, "port"); ok {
		port = value
	}
	if server == "" || port < 1 || port > 65535 {
		return "", false, fmt.Errorf("invalid managed proxy server or port")
	}

	switch proxy.Inbound.Protocol {
	case "vless-reality":
		return vlessURI(proxy.Credential, server, port, name, params)
	case "shadowsocks":
		credential, err := ss2022SubscriptionCredential(params, proxy.Credential)
		if err != nil {
			return "", false, err
		}
		return shadowsocksURI(credential, server, port, name, params)
	case "hysteria2":
		return hysteriaURI(proxy.Credential, server, port, name, params)
	default:
		return "", false, fmt.Errorf("不支持的协议: %s", proxy.Inbound.Protocol)
	}
}

func externalURI(proxy Proxy, name string) (string, bool, error) {
	params, err := inboundParams(proxy.Node.ExtParams)
	if err != nil {
		return "", false, err
	}
	params = mergeParams(params, proxy.Override)
	protocol := proxy.Node.ExtProtocol
	if protocol == "" {
		protocol = strParam(params, "protocol")
	}
	server := strParam(params, "server")
	port, ok := intParam(params, "port")
	if server == "" || !ok || port < 1 || port > 65535 {
		return "", false, fmt.Errorf("外部节点缺少有效的 server/port")
	}
	switch protocol {
	case "vless-reality", "vless":
		return vlessURI(strParam(params, "uuid"), server, port, name, params)
	case "shadowsocks", "ss":
		return shadowsocksURI(strParam(params, "password"), server, port, name, params)
	case "hysteria2", "hy2":
		return hysteriaURI(strParam(params, "password"), server, port, name, params)
	default:
		return "", false, fmt.Errorf("外部节点不支持协议: %s", protocol)
	}
}

func vlessURI(credential, server string, port int, name string, params map[string]any) (string, bool, error) {
	if credential == "" {
		return "", false, nil
	}
	query := url.Values{
		"encryption": []string{"none"},
		"security":   []string{"reality"},
		"type":       []string{"tcp"},
	}
	query.Set("flow", strParamDef(params, "flow", "xtls-rprx-vision"))
	for key, queryKey := range map[string]string{"sni": "sni", "fingerprint": "fp", "publicKey": "pbk", "shortId": "sid"} {
		if value := strParam(params, key); value != "" {
			query.Set(queryKey, value)
		}
	}
	if query.Get("sni") == "" || query.Get("pbk") == "" {
		return "", false, fmt.Errorf("Reality URI is missing sni or public key")
	}
	return standardURI("vless", credential, server, port, name, query), true, nil
}

func shadowsocksURI(credential, server string, port int, name string, params map[string]any) (string, bool, error) {
	if credential == "" {
		return "", false, nil
	}
	method := strParamDef(params, "method", "2022-blake3-aes-128-gcm")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + credential))
	return standardURI("ss", encoded, server, port, name, nil), true, nil
}

func hysteriaURI(credential, server string, port int, name string, params map[string]any) (string, bool, error) {
	if credential == "" {
		return "", false, nil
	}
	query := url.Values{}
	if sni := strParam(params, "sni"); sni != "" {
		query.Set("sni", sni)
	}
	if boolParamDef(params, "insecure", false) {
		query.Set("insecure", "1")
	}
	obfs, obfsPassword := hysteriaObfs(params)
	if obfs != "" {
		query.Set("obfs", obfs)
	}
	if obfsPassword != "" {
		query.Set("obfs-password", obfsPassword)
	}
	return standardURI("hy2", credential, server, port, name, query), true, nil
}

func ss2022SubscriptionCredential(params map[string]any, userCredential string) (string, error) {
	if strings.TrimSpace(userCredential) == "" {
		return "", nil
	}
	serverCredential := strings.TrimSpace(strParam(params, "password"))
	if serverCredential == "" {
		return "", fmt.Errorf("SS2022 is missing server password")
	}
	return serverCredential + ":" + userCredential, nil
}

func hysteriaObfs(params map[string]any) (string, string) {
	obfs := strParam(params, "obfs")
	obfsPassword := strParam(params, "obfsPassword")
	if value, ok := params["obfs"].(map[string]any); ok {
		obfs = strParam(value, "type")
		if obfsPassword == "" {
			obfsPassword = strParam(value, "password")
		}
	}
	if obfs == "" && obfsPassword != "" {
		obfs = "salamander"
	}
	return obfs, obfsPassword
}

func addHysteriaBandwidth(result map[string]any, params map[string]any) {
	if upMbps, ok := nonNegativeIntParam(params, "upMbps"); ok {
		result["up"] = fmt.Sprintf("%d Mbps", upMbps)
	}
	if downMbps, ok := nonNegativeIntParam(params, "downMbps"); ok {
		result["down"] = fmt.Sprintf("%d Mbps", downMbps)
	}
}

func standardURI(scheme, user, server string, port int, name string, query url.Values) string {
	uri := &url.URL{
		Scheme:   scheme,
		Host:     net.JoinHostPort(server, strconv.Itoa(port)),
		User:     url.User(user),
		RawQuery: query.Encode(),
		Fragment: name,
	}
	return uri.String()
}

func inboundParams(raw json.RawMessage) (map[string]any, error) {
	params := map[string]any{}
	if len(raw) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid protocol parameters: %w", err)
	}
	return params, nil
}

// SanitizeOverrideParams returns only supported, type-safe client overrides.
// Identity and key material are intentionally absent from this set.
func SanitizeOverrideParams(protocol string, input map[string]any) map[string]any {
	result := make(map[string]any)
	for key, value := range input {
		if ValidateOverrideParams("mihomo", protocol, map[string]any{key: value}) != nil {
			continue
		}
		switch key {
		case "flow":
			result[key] = value
		case "sni", "server", "obfs", "obfsPassword", "fingerprint":
			if text, ok := value.(string); ok && safeOverrideString(text) {
				result[key] = text
			}
		case "port":
			if number, ok := safeOverrideInt(value, 1, 65535); ok {
				if _, isJSONNumber := value.(float64); isJSONNumber {
					result[key] = value
				} else {
					result[key] = number
				}
			}
		case "upMbps", "downMbps":
			if number, ok := safeOverrideInt(value, 0, 1000000); ok {
				if _, isJSONNumber := value.(float64); isJSONNumber {
					result[key] = value
				} else {
					result[key] = number
				}
			}
		case "insecure":
			if insecure, ok := value.(bool); ok {
				result[key] = insecure
			}
		}
	}
	return result
}

func safeOverrideString(value string) bool {
	return value != "" && len(value) <= 2048 && !strings.ContainsAny(value, "\r\n\x00")
}

func safeOverrideInt(value any, min, max int) (int, bool) {
	var number int
	switch typed := value.(type) {
	case int:
		number = typed
	case int64:
		number = int(typed)
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		number = int(typed)
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, number >= min && number <= max
}

// mergeParams merges only supported overrides into protocol parameters.
func mergeParams(base map[string]any, ov *OverrideData) map[string]any {
	out := make(map[string]any, len(base))
	for key, value := range base {
		out[key] = value
	}
	if ov != nil {
		for key, value := range SanitizeOverrideParams("", ov.Params) {
			out[key] = value
		}
	}
	return out
}

func strParam(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func strParamDef(m map[string]any, key, def string) string {
	if key == "flow" {
		if value, exists := m[key].(string); exists {
			return value
		}
	}
	if value := strParam(m, key); value != "" {
		return value
	}
	return def
}

func intParam(m map[string]any, key string) (int, bool) {
	value := m[key]
	if text, ok := value.(string); ok {
		number, err := strconv.Atoi(text)
		return number, err == nil && number >= 1 && number <= 65535
	}
	return safeOverrideInt(value, 1, 65535)
}

func nonNegativeIntParam(m map[string]any, key string) (int, bool) {
	value := m[key]
	if text, ok := value.(string); ok {
		number, err := strconv.Atoi(text)
		return number, err == nil && number >= 0
	}
	return safeOverrideInt(value, 0, 1000000)
}

func boolParam(m map[string]any, key string) (bool, bool) {
	value, ok := m[key].(bool)
	return value, ok
}

func boolParamDef(m map[string]any, key string, defaultValue bool) bool {
	if value, ok := boolParam(m, key); ok {
		return value
	}
	return defaultValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
