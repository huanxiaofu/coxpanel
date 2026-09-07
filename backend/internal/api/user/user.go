// Package user 提供登录用户自助端点。
package user

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/generator"
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
	if req.Format != "mihomo" && req.Format != "base64" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "不支持的订阅格式")
		return
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
	id, err := h.Subs.CreateForUser(r.Context(), s)
	if errors.Is(err, repo.ErrNodeNotAuthorized) {
		middleware.Err(w, http.StatusForbidden, "group_not_authorized", "未授权的节点组")
		return
	}
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
	if _, err := h.Subs.GetByIDForUser(r.Context(), c.UserID, id); err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if err := h.Subs.DeleteForUser(r.Context(), c.UserID, id); err != nil {
		if errors.Is(err, repo.ErrSubscriptionNotFound) {
			middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
			return
		}
		middleware.Err(w, http.StatusInternalServerError, "internal", "删除失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SaveOverride 保存兼容的节点级覆写。
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
	if _, err := h.Subs.GetByIDForUser(r.Context(), c.UserID, subID); err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	req, err := decodeOverrideRequest(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	params, err := sanitizeOverrideParams(req.Params)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "params 无效")
		return
	}
	err = h.Subs.SaveOverrideForUser(r.Context(), c.UserID, subID, nodeID, req.DisplayName, req.SortOrder, req.Icon, req.ProxyGroup, params)
	if errors.Is(err, repo.ErrSubscriptionNotFound) {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if errors.Is(err, repo.ErrNodeNotAuthorized) {
		middleware.Err(w, http.StatusForbidden, "node_not_authorized", "节点未授权给该用户")
		return
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "保存失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SaveInboundOverride saves an override for one entry inbound without sharing
// a node-level row with the node's other inbounds.
func (h *Handler) SaveInboundOverride(w http.ResponseWriter, r *http.Request) {
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
	inboundID, err := strconv.ParseInt(r.PathValue("inboundId"), 10, 64)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "inboundId 无效")
		return
	}
	if _, err := h.Subs.GetByIDForUser(r.Context(), c.UserID, subID); err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	req, err := decodeOverrideRequest(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	params, err := sanitizeOverrideParams(req.Params)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "params 无效")
		return
	}
	err = h.Subs.SaveInboundOverrideForUser(r.Context(), c.UserID, subID, nodeID, inboundID, req.DisplayName, req.SortOrder, req.Icon, req.ProxyGroup, params)
	if errors.Is(err, repo.ErrSubscriptionNotFound) {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if errors.Is(err, repo.ErrNodeNotAuthorized) {
		middleware.Err(w, http.StatusForbidden, "node_not_authorized", "入站未授权给该用户")
		return
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "保存失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

type overrideRequest struct {
	DisplayName string          `json:"displayName"`
	SortOrder   int             `json:"sortOrder"`
	Icon        string          `json:"icon"`
	ProxyGroup  string          `json:"proxyGroup"`
	Params      json.RawMessage `json:"params"`
}

func decodeOverrideRequest(r *http.Request) (overrideRequest, error) {
	var req overrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return overrideRequest{}, err
	}
	return req, nil
}

func sanitizeOverrideParams(raw json.RawMessage) (json.RawMessage, error) {
	var params map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}
	}
	return json.Marshal(generator.SanitizeOverrideParams("", params))
}

func genToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
