package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// RenderedConfig is the immutable output consumed by the agent and HTTP layer.
type RenderedConfig struct {
	SchemaVersion string
	Version       string
	SHA256        string
	Content       []byte
}

// SingBoxConfig contains fields verified against the v1.13.21 upstream schema.
type SingBoxConfig struct {
	Log       *LogConfig   `json:"log,omitempty"`
	Inbounds  []SBInbound  `json:"inbounds"`
	Outbounds []SBOutbound `json:"outbounds"`
	Route     *SBRoute     `json:"route,omitempty"`
}

type LogConfig struct {
	Level string `json:"level"`
}

type SBInbound struct {
	Type       string          `json:"type"`
	Tag        string          `json:"tag"`
	Listen     string          `json:"listen,omitempty"`
	ListenPort int             `json:"listen_port"`
	Users      []SBUser        `json:"users,omitempty"`
	TLS        *SBInboundTLS   `json:"tls,omitempty"`
	Method     string          `json:"method,omitempty"`
	Password   string          `json:"password,omitempty"`
	UpMbps     int             `json:"up_mbps,omitempty"`
	DownMbps   int             `json:"down_mbps,omitempty"`
	Obfs       *SBHysteriaObfs `json:"obfs,omitempty"`
}

type SBUser struct {
	Name     string `json:"name,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Flow     string `json:"flow,omitempty"`
}

type SBInboundTLS struct {
	Enabled         bool       `json:"enabled"`
	ServerName      string     `json:"server_name,omitempty"`
	CertificatePath string     `json:"certificate_path,omitempty"`
	KeyPath         string     `json:"key_path,omitempty"`
	Reality         *SBReality `json:"reality,omitempty"`
}

type SBReality struct {
	Enabled    bool         `json:"enabled"`
	Handshake  *SBHandshake `json:"handshake,omitempty"`
	PrivateKey string       `json:"private_key,omitempty"`
	ShortID    []string     `json:"short_id,omitempty"`
}

type SBHandshake struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}

type SBHysteriaObfs struct {
	Type     string `json:"type,omitempty"`
	Password string `json:"password,omitempty"`
}

type SBOutbound struct {
	Type       string          `json:"type"`
	Tag        string          `json:"tag"`
	Server     string          `json:"server,omitempty"`
	ServerPort int             `json:"server_port,omitempty"`
	UUID       string          `json:"uuid,omitempty"`
	Flow       string          `json:"flow,omitempty"`
	Method     string          `json:"method,omitempty"`
	Password   string          `json:"password,omitempty"`
	UpMbps     int             `json:"up_mbps,omitempty"`
	DownMbps   int             `json:"down_mbps,omitempty"`
	Obfs       *SBHysteriaObfs `json:"obfs,omitempty"`
	TLS        *SBOutboundTLS  `json:"tls,omitempty"`
}

type SBOutboundTLS struct {
	Enabled    bool               `json:"enabled"`
	ServerName string             `json:"server_name,omitempty"`
	Insecure   bool               `json:"insecure,omitempty"`
	Reality    *SBOutboundReality `json:"reality,omitempty"`
}

type SBOutboundReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
}

type SBRoute struct {
	Final string   `json:"final"`
	Rules []SBRule `json:"rules,omitempty"`
}

type SBRule struct {
	Inbound  []string `json:"inbound,omitempty"`
	Outbound string   `json:"outbound"`
}

// Hash returns the stable SHA-256 identity of actual rendered bytes.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ValidateRendered verifies the JSON shape emitted by Render.
func ValidateRendered(content []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return fmt.Errorf("rendered config is not an object")
	}
	for _, name := range []string{"inbounds", "outbounds"} {
		value, ok := fields[name]
		if !ok {
			return fmt.Errorf("rendered config missing %s", name)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(value, &list); err != nil {
			return fmt.Errorf("rendered config %s is invalid", name)
		}
	}
	return nil
}

// Render translates the shared model into deterministic sing-box JSON.
func Render(node NodeConfig) (*RenderedConfig, error) {
	if node.NodeID <= 0 {
		return nil, fmt.Errorf("invalid nodeId")
	}
	if node.SchemaVersion != "" && node.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported schema version: %s", node.SchemaVersion)
	}
	inbounds := append([]Inbound(nil), node.Inbounds...)
	sort.SliceStable(inbounds, func(i, j int) bool { return inbounds[i].ID < inbounds[j].ID })
	credentials := append([]UserCredential(nil), node.Credentials...)
	sort.SliceStable(credentials, func(i, j int) bool {
		if credentials[i].InboundID != credentials[j].InboundID {
			return credentials[i].InboundID < credentials[j].InboundID
		}
		return credentials[i].UserID < credentials[j].UserID
	})
	edges := append([]Edge(nil), node.Edges...)
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	cfg := SingBoxConfig{
		Log:       &LogConfig{Level: "warn"},
		Inbounds:  make([]SBInbound, 0, len(inbounds)),
		Outbounds: []SBOutbound{{Type: "direct", Tag: "direct"}},
		Route:     &SBRoute{Final: "direct", Rules: []SBRule{}},
	}
	seenIDs := make(map[int64]struct{}, len(inbounds))
	tags := make(map[int64]string, len(inbounds))
	for _, inbound := range inbounds {
		if inbound.ID <= 0 {
			return nil, fmt.Errorf("inbound id is invalid: %d", inbound.ID)
		}
		if _, exists := seenIDs[inbound.ID]; exists {
			return nil, fmt.Errorf("inbound id is duplicated: %d", inbound.ID)
		}
		seenIDs[inbound.ID] = struct{}{}
		tag := fmt.Sprintf("in-%d-%s", inbound.ID, inbound.Protocol)
		tags[inbound.ID] = tag
		users := credentialsFor(credentials, inbound.ID)
		sb, err := renderInbound(inbound, users)
		if err != nil {
			return nil, fmt.Errorf("inbound %d: %w", inbound.ID, err)
		}
		sb.Tag = tag
		cfg.Inbounds = append(cfg.Inbounds, *sb)
	}
	for _, edge := range edges {
		fromTag, ok := tags[edge.FromInboundID]
		if !ok {
			return nil, fmt.Errorf("edge source inbound does not exist: %d", edge.FromInboundID)
		}
		outbound, err := renderEdge(edge)
		if err != nil {
			return nil, fmt.Errorf("edge %d: %w", edge.ID, err)
		}
		outbound.Tag = fmt.Sprintf("to-%d-%s", edge.ToInboundID, edge.ToProtocol)
		cfg.Outbounds = append(cfg.Outbounds, outbound)
		cfg.Route.Rules = append(cfg.Route.Rules, SBRule{Inbound: []string{fromTag}, Outbound: outbound.Tag})
	}
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sing-box config: %w", err)
	}
	hash := Hash(content)
	return &RenderedConfig{SchemaVersion: SchemaVersion, Version: hash, SHA256: hash, Content: content}, nil
}

func hasCredentialForInbound(credentials []UserCredential, inboundID int64) bool {
	for _, credential := range credentials {
		if credential.InboundID == inboundID {
			return true
		}
	}
	return false
}

// RuntimeNode returns the deployed topology after applying the current
// authorized credential roster. An entry with no authorized users is removed
// instead of receiving a static credential that could be guessed or reused.
func RuntimeNode(node NodeConfig) (NodeConfig, error) {
	if err := ValidateTopologyMaterial(node); err != nil {
		return NodeConfig{}, err
	}

	runtime := node
	runtime.Inbounds = make([]Inbound, 0, len(node.Inbounds))
	activeInboundIDs := make(map[int64]struct{}, len(node.Inbounds))
	for _, inbound := range node.Inbounds {
		if inbound.Role == "entry" && !hasCredentialForInbound(node.Credentials, inbound.ID) {
			continue
		}
		runtime.Inbounds = append(runtime.Inbounds, inbound)
		activeInboundIDs[inbound.ID] = struct{}{}
	}
	runtime.Credentials = make([]UserCredential, 0, len(node.Credentials))
	for _, credential := range node.Credentials {
		if _, ok := activeInboundIDs[credential.InboundID]; ok {
			runtime.Credentials = append(runtime.Credentials, credential)
		}
	}
	runtime.Edges = make([]Edge, 0, len(node.Edges))
	for _, edge := range node.Edges {
		if _, ok := activeInboundIDs[edge.FromInboundID]; ok {
			runtime.Edges = append(runtime.Edges, edge)
		}
	}
	return runtime, nil
}

// RenderRuntime renders an agent configuration while keeping the strict
// Render contract for generators and previews. Empty authorized rosters are
// represented by a valid configuration with no entry listeners.
func RenderRuntime(node NodeConfig) (*RenderedConfig, error) {
	runtime, err := RuntimeNode(node)
	if err != nil {
		return nil, err
	}
	return Render(runtime)
}

// ValidateTopologyMaterial validates server-owned routing material without
// requiring the active user credential roster.
func ValidateTopologyMaterial(node NodeConfig) error {
	if node.NodeID <= 0 {
		return fmt.Errorf("invalid nodeId")
	}
	if node.SchemaVersion != "" && node.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version: %s", node.SchemaVersion)
	}
	seen := make(map[int64]struct{}, len(node.Inbounds))
	for _, inbound := range node.Inbounds {
		if inbound.ID <= 0 {
			return fmt.Errorf("inbound id is invalid: %d", inbound.ID)
		}
		if _, exists := seen[inbound.ID]; exists {
			return fmt.Errorf("inbound id is duplicated: %d", inbound.ID)
		}
		seen[inbound.ID] = struct{}{}
		if inbound.Port < 1 || inbound.Port > 65535 {
			return fmt.Errorf("inbound %d port is invalid: %d", inbound.ID, inbound.Port)
		}
		switch inbound.Role {
		case "entry", "landing", "relay":
		default:
			return fmt.Errorf("inbound %d role is unsupported: %s", inbound.ID, inbound.Role)
		}
		params := inbound.Params
		if params == nil {
			params = map[string]string{}
		}
		switch inbound.Protocol {
		case "vless-reality":
			if inbound.Role == "entry" {
				if firstNonEmpty(params["sni"]) == "" || firstNonEmpty(params["privateKey"], params["private_key"]) == "" || firstNonEmpty(params["shortId"], params["short_id"]) == "" || firstNonEmpty(params["target"], params["handshakeServer"]) == "" {
					return fmt.Errorf("inbound %d Reality material is incomplete", inbound.ID)
				}
			} else if firstNonEmpty(params["uuid"]) == "" || firstNonEmpty(params["sni"]) == "" || firstNonEmpty(params["privateKey"], params["private_key"]) == "" || firstNonEmpty(params["shortId"], params["short_id"]) == "" || firstNonEmpty(params["target"], params["handshakeServer"]) == "" {
				return fmt.Errorf("inbound %d Reality material is incomplete", inbound.ID)
			}
		case "shadowsocks":
			if !isSS2022Method(firstNonEmpty(params["method"], "2022-blake3-aes-128-gcm")) || firstNonEmpty(params["password"]) == "" {
				return fmt.Errorf("inbound %d SS2022 material is incomplete", inbound.ID)
			}
		case "hysteria2":
			if firstNonEmpty(params["certificatePath"], params["certificate_path"]) == "" || firstNonEmpty(params["keyPath"], params["key_path"]) == "" || (inbound.Role != "entry" && firstNonEmpty(params["password"]) == "") {
				return fmt.Errorf("inbound %d HY2 TLS material is incomplete", inbound.ID)
			}
		default:
			return fmt.Errorf("unsupported protocol: %s", inbound.Protocol)
		}
	}
	for _, edge := range node.Edges {
		from, exists := findConfigInbound(node.Inbounds, edge.FromInboundID)
		if !exists || from.Role != "entry" {
			return fmt.Errorf("edge source inbound is invalid: %d", edge.FromInboundID)
		}
		if edge.ToServer == "" || edge.ToPort < 1 || edge.ToPort > 65535 {
			return fmt.Errorf("edge destination server or port is invalid")
		}
		params := edge.ToParams
		if params == nil {
			params = map[string]string{}
		}
		switch edge.ToProtocol {
		case "vless-reality":
			if params["uuid"] == "" || params["sni"] == "" || firstNonEmpty(params["publicKey"], params["public_key"]) == "" || firstNonEmpty(params["shortId"], params["short_id"]) == "" {
				return fmt.Errorf("Reality edge material is incomplete")
			}
		case "shadowsocks":
			if !isSS2022Method(firstNonEmpty(params["method"], "2022-blake3-aes-128-gcm")) || params["password"] == "" {
				return fmt.Errorf("SS2022 edge material is incomplete")
			}
		case "hysteria2":
			if params["password"] == "" {
				return fmt.Errorf("HY2 edge material is incomplete")
			}
		default:
			return fmt.Errorf("unsupported destination protocol: %s", edge.ToProtocol)
		}
	}
	return nil
}

// RenderRouting renders the stable deployment identity with deterministic
// placeholders for user credentials. Server material remains part of the
// resulting hash, while credential roster changes do not require redeploy.
func RenderRouting(node NodeConfig) (*RenderedConfig, error) {
	if err := ValidateTopologyMaterial(node); err != nil {
		return nil, err
	}
	routing := node
	routing.Credentials = make([]UserCredential, 0)
	for _, inbound := range routing.Inbounds {
		if inbound.Role != "entry" {
			continue
		}
		credential := UserCredential{UserID: -1, InboundID: inbound.ID, Name: "routing", Protocol: inbound.Protocol}
		switch inbound.Protocol {
		case "vless-reality":
			credential.UUID = "00000000-0000-4000-8000-000000000000"
		case "shadowsocks", "hysteria2":
			credential.Password = "routing-placeholder"
		}
		routing.Credentials = append(routing.Credentials, credential)
	}
	return Render(routing)
}

func findConfigInbound(inbounds []Inbound, id int64) (Inbound, bool) {
	for _, inbound := range inbounds {
		if inbound.ID == id {
			return inbound, true
		}
	}
	return Inbound{}, false
}

// RenderRedacted renders the same shared schema for administrator previews
// without including server identity material or the active user roster.
func RenderRedacted(node NodeConfig) (*RenderedConfig, error) {
	redacted := node
	redacted.Credentials = nil
	redacted.Inbounds = append([]Inbound(nil), node.Inbounds...)
	for index := range redacted.Inbounds {
		redacted.Inbounds[index].Params = cloneParams(redacted.Inbounds[index].Params)
		redacted.Inbounds[index].Params["uuid"] = "redacted-00000000-0000-4000-8000-000000000000"
		redacted.Inbounds[index].Params["password"] = "redacted"
		redacted.Inbounds[index].Params["privateKey"] = "redacted"
		redacted.Inbounds[index].Params["shortId"] = "redacted"
	}
	redacted.Edges = append([]Edge(nil), node.Edges...)
	for index := range redacted.Edges {
		redacted.Edges[index].ToParams = cloneParams(redacted.Edges[index].ToParams)
		redacted.Edges[index].ToParams["uuid"] = "redacted-00000000-0000-4000-8000-000000000000"
		redacted.Edges[index].ToParams["password"] = "redacted"
		redacted.Edges[index].ToParams["privateKey"] = "redacted"
		redacted.Edges[index].ToParams["shortId"] = "redacted"
	}
	redacted.Credentials = redactedCredentials(redacted.Inbounds)
	rendered, err := Render(redacted)
	if err != nil {
		return nil, err
	}
	content, err := redactRendered(rendered.Content)
	if err != nil {
		return nil, err
	}
	rendered.Content = content
	rendered.Version = Hash(content)
	rendered.SHA256 = rendered.Version
	return rendered, nil
}

func redactedCredentials(inbounds []Inbound) []UserCredential {
	credentials := make([]UserCredential, 0)
	for _, inbound := range inbounds {
		if inbound.Role != "entry" {
			continue
		}
		credential := UserCredential{UserID: -1, InboundID: inbound.ID, Name: "redacted", Protocol: inbound.Protocol}
		switch inbound.Protocol {
		case "vless-reality":
			credential.UUID = "redacted-00000000-0000-4000-8000-000000000000"
		case "shadowsocks", "hysteria2":
			credential.Password = "redacted"
		default:
			continue
		}
		credentials = append(credentials, credential)
	}
	return credentials
}

func cloneParams(params map[string]string) map[string]string {
	cloned := make(map[string]string, len(params)+4)
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

func redactRendered(content []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, err
	}
	redactValue(value)
	return json.MarshalIndent(value, "", "  ")
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			if isRedactedPreviewKey(key) {
				delete(typed, key)
				continue
			}
			redactValue(typed[key])
		}
	case []any:
		for _, item := range typed {
			redactValue(item)
		}
	}
}

func isRedactedPreviewKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	for _, marker := range []string{
		"password", "privatekey", "publickey", "uuid", "shortid", "users", "credential",
		"keypath", "certificatepath", "obfspassword", "psk", "secret", "token", "apikey", "user",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func renderInbound(inbound Inbound, credentials []UserCredential) (*SBInbound, error) {
	if inbound.Port < 1 || inbound.Port > 65535 {
		return nil, fmt.Errorf("port is invalid: %d", inbound.Port)
	}
	if inbound.Role == "entry" && len(credentials) == 0 {
		return nil, fmt.Errorf("entry inbound has no active user credentials")
	}
	params := inbound.Params
	if params == nil {
		params = map[string]string{}
	}
	sb := &SBInbound{Listen: firstNonEmpty(params["listen"], inbound.Listen, "::"), ListenPort: inbound.Port}
	switch inbound.Protocol {
	case "vless-reality":
		return renderReality(sb, params, credentials)
	case "shadowsocks":
		return renderShadowsocks(sb, params, credentials)
	case "hysteria2":
		return renderHysteria2(sb, params, credentials)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", inbound.Protocol)
	}
}

func renderReality(sb *SBInbound, params map[string]string, credentials []UserCredential) (*SBInbound, error) {
	sb.Type = "vless"
	flow := firstNonEmpty(params["flow"], "xtls-rprx-vision")
	if len(credentials) > 0 {
		for _, credential := range credentials {
			if credential.Protocol != "" && credential.Protocol != "vless-reality" {
				return nil, fmt.Errorf("credential protocol does not match Reality")
			}
			if credential.UUID == "" {
				return nil, fmt.Errorf("Reality user uuid is empty")
			}
			sb.Users = append(sb.Users, SBUser{Name: credential.Name, UUID: credential.UUID, Flow: flow})
		}
	} else if uuid := params["uuid"]; uuid != "" {
		sb.Users = []SBUser{{UUID: uuid, Flow: flow}}
	} else {
		return nil, fmt.Errorf("Reality is missing uuid")
	}
	sni := params["sni"]
	privateKey := firstNonEmpty(params["privateKey"], params["private_key"])
	shortID := firstNonEmpty(params["shortId"], params["short_id"])
	target := firstNonEmpty(params["target"], params["handshakeServer"])
	if sni == "" || privateKey == "" || shortID == "" || target == "" {
		return nil, fmt.Errorf("Reality is missing sni/privateKey/shortId/target")
	}
	server, port, err := splitTarget(target, params["targetPort"])
	if err != nil {
		return nil, fmt.Errorf("Reality handshake: %w", err)
	}
	sb.TLS = &SBInboundTLS{
		Enabled:    true,
		ServerName: sni,
		Reality: &SBReality{
			Enabled:    true,
			PrivateKey: privateKey,
			ShortID:    []string{shortID},
			Handshake:  &SBHandshake{Server: server, ServerPort: port},
		},
	}
	return sb, nil
}

func renderShadowsocks(sb *SBInbound, params map[string]string, credentials []UserCredential) (*SBInbound, error) {
	sb.Type = "shadowsocks"
	sb.Method = firstNonEmpty(params["method"], "2022-blake3-aes-128-gcm")
	if !isSS2022Method(sb.Method) {
		return nil, fmt.Errorf("only SS2022 methods are supported: %s", sb.Method)
	}
	sb.Password = params["password"]
	if sb.Password == "" {
		return nil, fmt.Errorf("SS2022 is missing password")
	}
	for _, credential := range credentials {
		if credential.Protocol != "" && credential.Protocol != "shadowsocks" {
			return nil, fmt.Errorf("credential protocol does not match SS2022")
		}
		if credential.Password == "" {
			return nil, fmt.Errorf("SS2022 user password is empty")
		}
		sb.Users = append(sb.Users, SBUser{Name: credential.Name, Password: credential.Password})
	}
	return sb, nil
}

func renderHysteria2(sb *SBInbound, params map[string]string, credentials []UserCredential) (*SBInbound, error) {
	sb.Type = "hysteria2"
	sb.Password = params["password"]
	for _, credential := range credentials {
		if credential.Protocol != "" && credential.Protocol != "hysteria2" {
			return nil, fmt.Errorf("credential protocol does not match HY2")
		}
		if credential.Password == "" {
			return nil, fmt.Errorf("HY2 user password is empty")
		}
		sb.Users = append(sb.Users, SBUser{Name: credential.Name, Password: credential.Password})
	}
	if len(credentials) > 0 {
		sb.Password = ""
	}
	if sb.Password == "" && len(sb.Users) == 0 {
		return nil, fmt.Errorf("HY2 is missing password or user credentials")
	}
	sb.UpMbps = intParam(params, "upMbps", 100)
	sb.DownMbps = intParam(params, "downMbps", 100)
	if sb.UpMbps < 0 || sb.DownMbps < 0 {
		return nil, fmt.Errorf("HY2 bandwidth cannot be negative")
	}
	if obfsPassword := params["obfsPassword"]; obfsPassword != "" {
		sb.Obfs = &SBHysteriaObfs{Type: "salamander", Password: obfsPassword}
	}
	certificatePath := firstNonEmpty(params["certificatePath"], params["certificate_path"])
	keyPath := firstNonEmpty(params["keyPath"], params["key_path"])
	if certificatePath == "" || keyPath == "" {
		return nil, fmt.Errorf("HY2 TLS is missing certificatePath/keyPath")
	}
	sb.TLS = &SBInboundTLS{Enabled: true, ServerName: params["sni"], CertificatePath: certificatePath, KeyPath: keyPath}
	return sb, nil
}

func renderEdge(edge Edge) (SBOutbound, error) {
	if edge.ToServer == "" || edge.ToPort < 1 || edge.ToPort > 65535 {
		return SBOutbound{}, fmt.Errorf("destination server or port is invalid")
	}
	params := edge.ToParams
	if params == nil {
		params = map[string]string{}
	}
	outbound := SBOutbound{Server: edge.ToServer, ServerPort: edge.ToPort}
	switch edge.ToProtocol {
	case "shadowsocks":
		outbound.Type = "shadowsocks"
		outbound.Method = firstNonEmpty(params["method"], "2022-blake3-aes-128-gcm")
		outbound.Password = params["password"]
		if !isSS2022Method(outbound.Method) || outbound.Password == "" {
			return SBOutbound{}, fmt.Errorf("invalid SS2022 method or missing password")
		}
	case "hysteria2":
		outbound.Type = "hysteria2"
		outbound.Password = params["password"]
		if outbound.Password == "" {
			return SBOutbound{}, fmt.Errorf("HY2 is missing password")
		}
		outbound.UpMbps = intParam(params, "upMbps", 100)
		outbound.DownMbps = intParam(params, "downMbps", 100)
		if outbound.UpMbps < 0 || outbound.DownMbps < 0 {
			return SBOutbound{}, fmt.Errorf("HY2 bandwidth cannot be negative")
		}
		outbound.TLS = &SBOutboundTLS{Enabled: true, ServerName: params["sni"], Insecure: params["insecure"] == "true"}
	case "vless-reality":
		outbound.Type = "vless"
		outbound.UUID = params["uuid"]
		publicKey := firstNonEmpty(params["publicKey"], params["public_key"])
		shortID := firstNonEmpty(params["shortId"], params["short_id"])
		if outbound.UUID == "" || params["sni"] == "" || publicKey == "" || shortID == "" {
			return SBOutbound{}, fmt.Errorf("Reality outbound is missing uuid/sni/publicKey/shortId")
		}
		outbound.Flow = firstNonEmpty(params["flow"], "xtls-rprx-vision")
		outbound.TLS = &SBOutboundTLS{Enabled: true, ServerName: params["sni"], Reality: &SBOutboundReality{Enabled: true, PublicKey: publicKey, ShortID: shortID}}
	default:
		return SBOutbound{}, fmt.Errorf("unsupported destination protocol: %s", edge.ToProtocol)
	}
	return outbound, nil
}

func credentialsFor(credentials []UserCredential, inboundID int64) []UserCredential {
	matched := make([]UserCredential, 0)
	for _, credential := range credentials {
		if credential.InboundID == inboundID {
			matched = append(matched, credential)
		}
	}
	return matched
}

func splitTarget(target, portValue string) (string, int, error) {
	if portValue != "" {
		port, err := strconv.Atoi(portValue)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("target port is invalid")
		}
		return strings.TrimSpace(target), port, nil
	}
	if host, portText, err := net.SplitHostPort(target); err == nil {
		port, portErr := strconv.Atoi(portText)
		if portErr != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("target port is invalid")
		}
		return host, port, nil
	}
	if strings.Count(target, ":") > 1 && !strings.HasPrefix(target, "[") {
		return "", 0, fmt.Errorf("target must be host or host:port")
	}
	return strings.Trim(target, "[]"), 443, nil
}

func intParam(params map[string]string, name string, defaultValue int) int {
	value := params[name]
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}

func isSS2022Method(method string) bool {
	switch method {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
