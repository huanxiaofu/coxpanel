// Package user 提供登录用户自助端点。
package user

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

// Handler 用户端点依赖。
type Handler struct {
	Subs  *repo.SubscriptionRepo
	Nodes *repo.NodeRepo
}

// ListMySubs 列出我的订阅。
func (h *Handler) ListMySubs(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	list, err := h.Subs.ListByUser(r.Context(), c.UserID)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

// CreateSub 创建订阅。
func (h *Handler) CreateSub(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	var req struct {
		Name        string `json:"name"`
		Format      string `json:"format"`
		NodeGroupID *int64 `json:"nodeGroupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if req.Name == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "名称必填")
		return
	}
	if req.Format == "" {
		req.Format = "mihomo"
	}
	token, err := genToken()
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "token 生成失败")
		return
	}
	s := &models.Subscription{
		UserID: c.UserID, Name: req.Name, Token: token,
		Format: req.Format, NodeGroupID: req.NodeGroupID,
	}
	id, err := h.Subs.Create(r.Context(), s)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败")
		return
	}
	middleware.JSON(w, http.StatusCreated, map[string]any{"id": id, "token": token})
}

// DeleteSub 删除订阅。
func (h *Handler) DeleteSub(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	s, err := h.Subs.GetByID(r.Context(), id)
	if err != nil || s.UserID != c.UserID {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if err := h.Subs.Delete(r.Context(), id); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SaveOverride 保存节点覆写。
func (h *Handler) SaveOverride(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	subID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	nodeID, err := strconv.ParseInt(r.PathValue("nodeId"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	s, err := h.Subs.GetByID(r.Context(), subID)
	if err != nil || s.UserID != c.UserID {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	var req struct {
		DisplayName string          `json:"displayName"`
		SortOrder   int             `json:"sortOrder"`
		Icon        string          `json:"icon"`
		ProxyGroup  string          `json:"proxyGroup"`
		Params      json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if err := h.Subs.SaveOverride(r.Context(), subID, nodeID, req.DisplayName, req.SortOrder, req.Icon, req.ProxyGroup, req.Params); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "保存失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func genToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
