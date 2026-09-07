// Package middleware 提供 HTTP 中间件。
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/models"
)

type ctxKey string

const claimsKey ctxKey = "claims"

const userKey ctxKey = "user"

type UserStore interface {
	GetUser(context.Context, int64) (*models.User, error)
}

// RequireAuth 校验 JWT，注入 Claims。
func RequireAuth(authSvc *auth.Service, users UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authSvc == nil || users == nil {
				writeErr(w, http.StatusUnauthorized, "authentication_unavailable", "认证服务不可用")
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeErr(w, http.StatusUnauthorized, "missing_token", "缺少认证头")
				return
			}
			claims, err := authSvc.ParseToken(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "invalid_token", "token 无效或已过期")
				return
			}
			user, err := users.GetUser(r.Context(), claims.UserID)
			if err != nil || user == nil || !user.Active || (user.ExpireAt != nil && !time.Now().Before(*user.ExpireAt)) {
				writeErr(w, http.StatusUnauthorized, "inactive_user", "用户不存在或已停用")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = context.WithValue(ctx, userKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin 要求 admin/owner 角色（在 RequireAuth 之后使用）。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil || (u.Role != "admin" && u.Role != "owner") {
			writeErr(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFrom 取上下文中的 Claims。
func ClaimsFrom(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsKey).(*auth.Claims)
	return c
}

func UserFrom(ctx context.Context) *models.User {
	u, _ := ctx.Value(userKey).(*models.User)
	return u
}

// JSON 写响应。
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	JSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

// Err 通用错误响应（供 handler 用）。
func Err(w http.ResponseWriter, code int, errCode, msg string) {
	writeErr(w, code, errCode, msg)
}
