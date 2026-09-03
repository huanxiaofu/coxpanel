package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

// NodeHandler 节点管理 handler。
type NodeHandler struct {
	Nodes *repo.NodeRepo
}

// CreateNode 创建节点（受管或外部）。
func (h *NodeHandler) CreateNode(w http.ResponseWriter, r *http.Request) {
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
	if req.Type == "external" && (req.ExtProtocol == "" || len(req.ExtParams) == 0) {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "外部节点必须填 extProtocol 和 extParams")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	n := &models.Node{
		Name: req.Name, Type: req.Type, PublicIP: req.PublicIP, EasyIP: req.EasyIP,
		SSHHost: req.SSHHost, SSHUser: req.SSHUser, SSHPort: req.SSHPort,
		ExtProtocol: req.ExtProtocol, ExtParams: req.ExtParams,
	}
	id, err := h.Nodes.Create(r.Context(), n)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败: "+err.Error())
		return
	}
	middleware.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListNodes 列出节点。
func (h *NodeHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	list, err := h.Nodes.List(r.Context())
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

// GetNode 节点详情。
func (h *NodeHandler) GetNode(w http.ResponseWriter, r *http.Request) {
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
	if req.Role == "" {
		req.Role = "entry"
	}
	if req.ListenAddr == "" {
		req.ListenAddr = "::"
	}
	if req.MinClientVer == "" {
		req.MinClientVer = "1.8.2"
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
	nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	list, err := h.Nodes.ListInbounds(r.Context(), nodeID)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

// DeleteInbound 删除入站。
func (h *NodeHandler) DeleteInbound(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("inboundId"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	if err := h.Nodes.DeleteInbound(r.Context(), id); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
