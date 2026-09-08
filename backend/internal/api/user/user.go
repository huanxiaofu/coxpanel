// Package user 提供登录用户自助端点。
package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

type SubscriptionStore interface {
	ListByUser(context.Context, int64) ([]models.Subscription, error)
	CreateForUser(context.Context, *models.Subscription) (int64, error)
	GetByIDForUser(context.Context, int64, int64) (*models.Subscription, error)
	DeleteForUser(context.Context, int64, int64) error
	SaveOverrideForUser(context.Context, int64, int64, int64, string, int, string, string, json.RawMessage) error
	SaveInboundOverrideForUser(context.Context, int64, int64, int64, int64, string, int, string, string, json.RawMessage) error
	ListOverridesForUser(context.Context, int64, int64) (map[int64]repo.OverrideRow, error)
	ListInboundOverridesForUser(context.Context, int64, int64) (map[repo.OverrideKey]repo.OverrideRow, error)
	EffectiveOverrideRows(context.Context, int64, int64) (map[repo.OverrideKey]repo.OverrideRow, error)
	SaveOverrideForUserAtRevision(context.Context, int64, int64, int64, int64, repo.OverridePatch) (*repo.OverrideRow, error)
	SaveInboundOverrideForUserAtRevision(context.Context, int64, int64, int64, int64, int64, repo.OverridePatch) (*repo.OverrideRow, error)
	DeleteOverrideForUser(context.Context, int64, int64, int64, int64) error
	DeleteInboundOverrideForUser(context.Context, int64, int64, int64, int64, int64) error
}

type NodeStore interface {
	Get(context.Context, int64) (*models.Node, error)
	ListInbounds(context.Context, int64) ([]models.Inbound, error)
}

type GroupStore interface {
	Exists(context.Context, int64) (bool, error)
	UserHasGroup(context.Context, int64, int64) (bool, error)
	AuthorizedNodeIDs(context.Context, int64, int64) ([]int64, error)
}

type UserStore interface {
	GetUser(context.Context, int64) (*models.User, error)
}

type TemplateStore interface {
	ResolveVersion(context.Context, *int64, *int) (*repo.TemplateVersion, error)
}

// Handler 用户端点依赖。
type Handler struct {
	Subs      SubscriptionStore
	Nodes     NodeStore
	Groups    GroupStore
	Users     UserStore
	Templates TemplateStore
}

// ListMySubs 列出我的订阅。
func (h *Handler) ListMySubs(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	if c == nil || h == nil || h.Subs == nil {
		middleware.Err(w, http.StatusUnauthorized, "missing_claims", "认证信息缺失")
		return
	}
	list, err := h.Subs.ListByUser(r.Context(), c.UserID)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}

func (h *Handler) GetSub(w http.ResponseWriter, r *http.Request) {
	c, id, ok := h.subscriptionPath(w, r)
	if !ok {
		return
	}
	subscription, err := h.Subs.GetByIDForUser(r.Context(), c.UserID, id)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	middleware.JSON(w, http.StatusOK, subscription)
}

func (h *Handler) UpdateSub(w http.ResponseWriter, r *http.Request) {
	c, id, ok := h.subscriptionPath(w, r)
	if !ok {
		return
	}
	var request struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		Name             string `json:"name"`
		NodeGroupID      *int64 `json:"nodeGroupId"`
		TemplateID       *int64 `json:"templateId"`
		TemplateVersion  *int   `json:"templateVersion"`
		Format           string `json:"format"`
	}
	if err := decodeJSON(r, &request); err != nil || request.ExpectedRevision <= 0 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体或 expectedRevision 无效")
		return
	}
	current, err := h.Subs.GetByIDForUser(r.Context(), c.UserID, id)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if request.Name == "" {
		request.Name = current.Name
	}
	if request.Format == "" {
		request.Format = current.Format
	}
	if request.NodeGroupID == nil {
		request.NodeGroupID = current.NodeGroupID
	}
	if err := validateSubscriptionInput(request.Format, request.TemplateID, request.TemplateVersion); err != nil {
		writeSubscriptionError(w, err)
		return
	}
	updated, err := updateSubscription(h.Subs, r.Context(), c.UserID, id, request.ExpectedRevision, request.Name, request.NodeGroupID, request.TemplateID, request.TemplateVersion, request.Format)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, updated)
}

// CreateSub 创建订阅。
func (h *Handler) CreateSub(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	if c == nil {
		middleware.Err(w, 401, "missing_claims", "认证信息缺失")
		return
	}
	var req struct {
		Name            string `json:"name"`
		Format          string `json:"format"`
		NodeGroupID     *int64 `json:"nodeGroupId"`
		TemplateID      *int64 `json:"templateId"`
		TemplateVersion *int   `json:"templateVersion"`
	}
	if err := decodeJSON(r, &req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 || strings.ContainsAny(req.Name, "\r\n\x00") {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "名称必填")
		return
	}
	if req.Format == "" {
		req.Format = "mihomo"
	}
	if req.Format != "mihomo" && req.Format != "sing-box" && req.Format != "base64" {
		middleware.Err(w, http.StatusUnprocessableEntity, "format_unsupported", "不支持的订阅格式")
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
		TemplateID: req.TemplateID, TemplateVersion: req.TemplateVersion,
	}
	id, err := h.Subs.CreateForUser(r.Context(), s)
	if errors.Is(err, repo.ErrNodeNotAuthorized) {
		middleware.Err(w, http.StatusForbidden, "group_not_authorized", "未授权的节点组")
		return
	}
	if writeSubscriptionError(w, err) {
		return
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败")
		return
	}
	s.ID = id
	middleware.JSON(w, http.StatusCreated, map[string]any{"id": id, "token": token, "subscription": s})
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

func genToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeSubscriptionError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, repo.ErrTemplateNotFound):
		middleware.Err(w, 422, "template_unavailable", "模板不可用")
	case errors.Is(err, repo.ErrRevisionConflict):
		middleware.Err(w, http.StatusConflict, "revision_conflict", "资源已被修改，请重新加载")
	case errors.Is(err, repo.ErrFormatUnsupported), errors.Is(err, repo.ErrTemplateVersionWithoutTemplate), errors.Is(err, repo.ErrTemplateFormatMismatch), errors.Is(err, repo.ErrTemplateNotPublished), errors.Is(err, repo.ErrTemplateVersionNotFound), errors.Is(err, repo.ErrTemplateArchived), errors.Is(err, repo.ErrInvalidOverride), errors.Is(err, repo.ErrOverrideFieldForbidden):
		middleware.Err(w, http.StatusUnprocessableEntity, "invalid_subscription", "订阅选择或覆写无效")
	default:
		return false
	}
	return true
}
