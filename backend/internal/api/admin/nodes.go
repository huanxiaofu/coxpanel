package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/go-chi/chi/v5"
)

// NodeHandler 节点管理 handler。
type NodeHandler struct {
	Nodes *repo.NodeRepo
}

// CreateNode 创建节点（受管或外部）。
func (h *NodeHandler) CreateNode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	var req struct {
		Name        string          `json:"name"`
		Type        string          `json:"type"` // managed / external
		PublicIP    string          `json:"publicIp"`
		EasyIP      string          `json:"easyIp"`
		SSHHost     string          `json:"sshHost"`
		SSHUser     string          `json:"sshUser"`
		SSHPort     int             `json:"sshPort"`
		ExtProtocol string          `json:"extProtocol"`
		ExtParams   json.RawMessage `json:"extParams"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if req.Name == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "名称必填")
		return
	}
	if req.Type == "" {
		req.Type = "managed"
	}
	if req.Type != "managed" && req.Type != "external" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "type 只能是 managed/external")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.SSHPort < 1 || req.SSHPort > 65535 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "SSH 端口必须为 1-65535")
		return
	}
	if err := validateNodeProtocol(req.Type, req.ExtProtocol, req.ExtParams); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	n := &models.Node{
		Name: req.Name, Type: req.Type, PublicIP: req.PublicIP, EasyIP: req.EasyIP,
		SSHHost: req.SSHHost, SSHUser: req.SSHUser, SSHPort: req.SSHPort,
		ExtProtocol: req.ExtProtocol, ExtParams: req.ExtParams,
	}
	var id int64
	var err error
	var agentCredential string
	if req.Type == "managed" {
		rawCredential, genErr := newAgentCredential()
		if genErr != nil {
			middleware.Err(w, http.StatusInternalServerError, "internal", "节点凭据生成失败")
			return
		}
		hash, hashErr := auth.HashPassword(rawCredential)
		if hashErr != nil {
			middleware.Err(w, http.StatusInternalServerError, "internal", "节点凭据处理失败")
			return
		}
		id, err = h.Nodes.CreateWithAgentCredential(r.Context(), n, hash)
		agentCredential = rawCredential
	} else {
		id, err = h.Nodes.Create(r.Context(), n)
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败: "+err.Error())
		return
	}
	response := map[string]any{"id": id}
	if agentCredential != "" {
		response["agentCredential"] = agentCredential
	}
	middleware.JSON(w, http.StatusCreated, response)
}

func newAgentCredential() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ListNodes 列出节点。
func (h *NodeHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	list, err := h.Nodes.List(r.Context())
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

// GetNode 节点详情。
func (h *NodeHandler) GetNode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	n, err := h.Nodes.Get(r.Context(), id)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	middleware.JSON(w, http.StatusOK, n)
}

// UpdateNode 更新节点。
func (h *NodeHandler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	var req struct {
		Name        string          `json:"name"`
		PublicIP    string          `json:"publicIp"`
		EasyIP      string          `json:"easyIp"`
		SSHHost     string          `json:"sshHost"`
		SSHUser     string          `json:"sshUser"`
		SSHPort     int             `json:"sshPort"`
		ExtProtocol string          `json:"extProtocol"`
		ExtParams   json.RawMessage `json:"extParams"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "名称必填")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.SSHPort < 1 || req.SSHPort > 65535 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "SSH 端口必须为 1-65535")
		return
	}
	current, err := h.Nodes.Get(r.Context(), id)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if err := validateNodeProtocol(current.Type, req.ExtProtocol, req.ExtParams); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	n := &models.Node{
		Name: req.Name, PublicIP: req.PublicIP, EasyIP: req.EasyIP,
		SSHHost: req.SSHHost, SSHUser: req.SSHUser, SSHPort: req.SSHPort,
		ExtProtocol: req.ExtProtocol, ExtParams: req.ExtParams,
	}
	if err := h.Nodes.Update(r.Context(), id, n); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "更新失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteNode 删除节点。
func (h *NodeHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	if err := h.Nodes.Delete(r.Context(), id); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// CreateInbound 创建入站。
func (h *NodeHandler) CreateInbound(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	var req struct {
		Name         string          `json:"name"`
		Protocol     string          `json:"protocol"`
		Role         string          `json:"role"`
		ListenAddr   string          `json:"listenAddr"`
		ListenPort   int             `json:"listenPort"`
		Config       json.RawMessage `json:"config"`
		MinClientVer string          `json:"minClientVer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if req.Name == "" || req.Protocol == "" || req.ListenPort <= 0 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "名称/协议/端口必填")
		return
	}
	if req.ListenPort > 65535 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "端口必须为 1-65535")
		return
	}
	if req.Role == "" {
		req.Role = "entry"
	}
	if err := validateInbound(req.Protocol, req.Role, req.Config); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ListenAddr == "" {
		req.ListenAddr = "::"
	}
	if len(req.Config) == 0 || string(req.Config) == "null" {
		req.Config = json.RawMessage(`{}`)
	}
	ib := &models.Inbound{
		NodeID: nodeID, Name: req.Name, Protocol: req.Protocol, Role: req.Role,
		ListenAddr: req.ListenAddr, ListenPort: req.ListenPort, Config: req.Config, MinClientVer: req.MinClientVer,
	}
	id, err := h.Nodes.CreateInbound(r.Context(), ib)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败")
		return
	}
	middleware.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListInbounds 列出入站。
func (h *NodeHandler) ListInbounds(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	list, err := h.Nodes.ListInbounds(r.Context(), nodeID)
	if err == nil {
		if revisions, revisionErr := h.Nodes.InboundRevisions(r.Context(), nodeID); revisionErr == nil {
			for index := range list {
				list[index].Revision = revisions[list[index].ID].Revision
				list[index].EgressMode = revisions[list[index].ID].EgressMode
			}
		}
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

func (h *NodeHandler) UpdateInbound(w http.ResponseWriter, r *http.Request) {
	var request struct {
		models.Inbound
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		middleware.Err(w, 422, "invalid_inbound", "入站字段无效")
		return
	}
	request.NodeID, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	request.ID, _ = strconv.ParseInt(chi.URLParam(r, "inboundId"), 10, 64)
	if request.Name == "" || request.ListenPort < 1 || request.ListenPort > 65535 || request.EgressMode == "" || validateInbound(request.Protocol, request.Role, request.Config) != nil {
		middleware.Err(w, 422, "invalid_inbound", "入站配置无效")
		return
	}
	if err := h.Nodes.UpdateInbound(r.Context(), request.Inbound, request.ExpectedRevision); err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"revision": request.ExpectedRevision + 1, "requiresPreview": true})
}

// DeleteInbound 删除入站。
func (h *NodeHandler) DeleteInbound(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Nodes == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "节点服务不可用")
		return
	}
	nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "node id 无效")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("inboundId"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	if err := h.Nodes.DeleteInboundForNode(r.Context(), nodeID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			middleware.Err(w, http.StatusNotFound, "not_found", "入站不存在")
			return
		}
		middleware.Err(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validateNodeProtocol(nodeType, protocol string, raw json.RawMessage) error {
	if nodeType != "external" {
		return nil
	}
	if protocol == "" {
		return errors.New("外部节点必须填写协议")
	}
	params, err := decodeProtocolParams(raw)
	if err != nil {
		return err
	}
	if err := validateServerAndPort(params); err != nil {
		return fmt.Errorf("外部节点参数无效: %w", err)
	}
	switch protocol {
	case "vless-reality":
		return requireParams(params, "uuid", "sni", "publicKey", "shortId")
	case "shadowsocks":
		if err := validateSS2022(params); err != nil {
			return fmt.Errorf("外部节点参数无效: %w", err)
		}
	case "hysteria2":
		if stringParam(params, "password") == "" || stringParam(params, "sni") == "" {
			return errors.New("外部节点 HY2 必须填写 password")
		}
	default:
		return fmt.Errorf("外部节点协议不支持: %s", protocol)
	}
	return nil
}

func validateInbound(protocol, role string, raw json.RawMessage) error {
	switch role {
	case "entry", "landing", "relay":
	default:
		return fmt.Errorf("入站角色不支持: %s", role)
	}
	params, err := decodeProtocolParams(raw)
	if err != nil {
		return err
	}
	switch protocol {
	case "vless-reality":
		if role == "entry" {
			if err := requireParams(params, "sni", "privateKey", "shortId", "target"); err != nil {
				return fmt.Errorf("Reality 参数无效: %w", err)
			}
		} else {
			if err := requireParams(params, "uuid", "sni", "privateKey", "shortId", "target"); err != nil {
				return fmt.Errorf("Reality 参数无效: %w", err)
			}
		}
		if err := validateRealityParams(params); err != nil {
			return fmt.Errorf("Reality 参数无效: %w", err)
		}
	case "shadowsocks":
		if err := validateSS2022(params); err != nil {
			return fmt.Errorf("SS2022 参数无效: %w", err)
		}
	case "hysteria2":
		if stringParam(params, "certificatePath") == "" || stringParam(params, "keyPath") == "" {
			return errors.New("HY2 必须填写 certificatePath 和 keyPath")
		}
		if role != "entry" && stringParam(params, "password") == "" {
			return errors.New("非入口 HY2 必须填写 password")
		}
		if err := validateNonNegativeParam(params, "upMbps"); err != nil {
			return fmt.Errorf("HY2 参数无效: %w", err)
		}
		if err := validateNonNegativeParam(params, "downMbps"); err != nil {
			return fmt.Errorf("HY2 参数无效: %w", err)
		}
	default:
		return fmt.Errorf("入站协议不支持: %s", protocol)
	}
	return nil
}

func decodeProtocolParams(raw json.RawMessage) (map[string]any, error) {
	params := make(map[string]any)
	if len(raw) == 0 || string(raw) == "null" {
		return params, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil || params == nil {
		return nil, errors.New("协议参数必须是 JSON 对象")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("协议参数必须是单个 JSON 对象")
	}
	return params, nil
}

func validateRealityParams(params map[string]any) error {
	if portValue, present := params["targetPort"]; present {
		port, ok := integerParam(map[string]any{"targetPort": portValue}, "targetPort")
		if !ok || port < 1 || port > 65535 {
			return errors.New("targetPort 必须为 1-65535")
		}
	}
	target := stringParam(params, "target")
	if strings.Contains(target, ":") {
		host, portText, err := net.SplitHostPort(target)
		if err != nil || strings.TrimSpace(host) == "" {
			if strings.Count(target, ":") > 1 && !strings.HasPrefix(target, "[") {
				return errors.New("target 必须为 host 或 host:port")
			}
			return nil
		}
		port, parseErr := strconv.Atoi(portText)
		if parseErr != nil || port < 1 || port > 65535 {
			return errors.New("target 端口必须为 1-65535")
		}
	}
	return nil
}

func validateNonNegativeParam(params map[string]any, name string) error {
	value, present := params[name]
	if !present {
		return nil
	}
	parsed, ok := integerParam(map[string]any{name: value}, name)
	if !ok || parsed < 0 {
		return fmt.Errorf("%s 必须为非负整数", name)
	}
	return nil
}

func validateServerAndPort(params map[string]any) error {
	if stringParam(params, "server") == "" {
		return errors.New("server 必填")
	}
	port, ok := integerParam(params, "port")
	if !ok || port < 1 || port > 65535 {
		return errors.New("port 必须为 1-65535")
	}
	return nil
}

func validateSS2022(params map[string]any) error {
	method := stringParam(params, "method")
	if method == "" {
		return errors.New("method 必填")
	}
	switch method {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
	default:
		return fmt.Errorf("不支持的 method: %s", method)
	}
	if stringParam(params, "password") == "" {
		return errors.New("password 必填")
	}
	return nil
}

func requireParams(params map[string]any, names ...string) error {
	for _, name := range names {
		if stringParam(params, name) == "" {
			return fmt.Errorf("%s 必填", name)
		}
	}
	return nil
}

func stringParam(params map[string]any, name string) string {
	value, _ := params[name].(string)
	return strings.TrimSpace(value)
}

func integerParam(params map[string]any, name string) (int, bool) {
	value := params[name]
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	case float64:
		parsed := int(typed)
		return parsed, typed == float64(parsed)
	case int:
		return typed, true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}
