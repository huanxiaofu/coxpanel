// Package middleware 提供 HTTP 中间件。
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coxpanel/backend/internal/auth"
)

type ctxKey string

const claimsKey ctxKey = "claims"

// RequireAuth 校验 JWT，注入 Claims。
func RequireAuth(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

// RequireAdmin 要求 admin/owner 角色（在 RequireAuth 之后使用）。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := ClaimsFrom(r.Context())
		if c == nil || (c.Role != "admin" && c.Role != "owner") {
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
