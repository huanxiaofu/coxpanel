// Package admin 提供管理员/认证相关 HTTP handler。
package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/repo"
)

// Handler 依赖集合。
type Handler struct {
	Auth   *auth.Service
	Users  *repo.UserRepo
}

// Register 注册（需邀请码）。
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Invite   string `json:"inviteCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if req.Username == "" || len(req.Password) < 8 || req.Email == "" || req.Invite == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "用户名/密码(≥8位)/邮箱/邀请码必填")
		return
	}

	// 消费邀请码
	ic, err := h.Users.UseInvite(r.Context(), req.Invite)
	if err != nil {
		middleware.Err(w, http.StatusForbidden, "invalid_invite", "邀请码无效或已用完")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "密码处理失败")
		return
	}
	id, err := h.Users.CreateUser(r.Context(), req.Username, hash, req.Email, "user")
	if err != nil {
		middleware.Err(w, http.StatusConflict, "conflict", "用户名或邮箱已存在")
		return
	}

	token, err := h.Auth.IssueToken(id, req.Username, "user")
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "token 签发失败")
		return
	}

	_ = ic // 邀请码节点组绑定（P2 再关联默认订阅）
	middleware.JSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  map[string]any{"id": id, "username": req.Username, "role": "user"},
	})
}

// Login 登录。
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	u, err := h.Users.GetByUsername(r.Context(), req.Username)
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		middleware.Err(w, http.StatusUnauthorized, "bad_credentials", "用户名或密码错误")
		return
	}
	token, err := h.Auth.IssueToken(u.ID, u.Username, u.Role)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "token 签发失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]any{"id": u.ID, "username": u.Username, "role": u.Role},
	})
}

// Me 当前用户信息。
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	u, err := h.Users.GetUser(r.Context(), c.UserID)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"email":    u.Email,
		"role":     u.Role,
	})
}

// GenInviteCode 生成邀请码（admin）。
func (h *Handler) GenInviteCode(w http.ResponseWriter, r *http.Request) {
	c := middleware.ClaimsFrom(r.Context())
	buf := make([]byte, 6)
	rand.Read(buf)
	code := "CXP-" + hex.EncodeToString(buf)
	if _, err := h.Users.CreateInvite(r.Context(), code, 1, nil, nil, c.UserID); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "创建失败")
		return
	}
	middleware.JSON(w, http.StatusCreated, map[string]any{"code": code})
}

// ListInvites 列出邀请码（admin）。
func (h *Handler) ListInvites(w http.ResponseWriter, r *http.Request) {
	list, err := h.Users.ListInvites(r.Context())
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}
	middleware.JSON(w, http.StatusOK, list)
}
