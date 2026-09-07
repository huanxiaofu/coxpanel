package user

import (
	"context"
	"net/http"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/repo"
)

type AuthorizedGroupStore interface {
	ListAuthorizedGroups(context.Context, int64) ([]repo.AuthorizedGroup, error)
}

// GroupHandler serves current-user group selection data.
type GroupHandler struct {
	Groups AuthorizedGroupStore
}

// ListMyGroups returns the current user's explicitly authorized groups.
func (h *GroupHandler) ListMyGroups(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Groups == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户组服务不可用")
		return
	}
	claims := middleware.ClaimsFrom(r.Context())
	if claims == nil {
		middleware.Err(w, http.StatusUnauthorized, "missing_claims", "认证信息缺失")
		return
	}
	groups, err := h.Groups.ListAuthorizedGroups(r.Context(), claims.UserID)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "用户组查询失败")
		return
	}
	if groups == nil {
		groups = make([]repo.AuthorizedGroup, 0)
	}
	middleware.JSON(w, http.StatusOK, groups)
}
