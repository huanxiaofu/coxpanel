package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/repo"
)

// AccessHandler serves administrator-only group and user authorization APIs.
type AccessHandler struct {
	Groups *repo.GroupRepo
	Users  *repo.UserRepo
}

func (h *AccessHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Groups == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户组服务不可用")
		return
	}
	groups, err := h.Groups.List(r.Context())
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "用户组查询失败")
		return
	}
	if groups == nil {
		groups = make([]repo.NodeGroup, 0)
	}
	middleware.JSON(w, http.StatusOK, groups)
}

func (h *AccessHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Groups == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户组服务不可用")
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Name) == "" || len(request.Name) > 128 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "用户组名称无效")
		return
	}
	id, err := h.Groups.Create(r.Context(), strings.TrimSpace(request.Name))
	if err != nil {
		middleware.Err(w, http.StatusConflict, "conflict", "用户组创建失败")
		return
	}
	middleware.JSON(w, http.StatusCreated, map[string]any{"id": id, "name": strings.TrimSpace(request.Name), "nodeIds": []int64{}})
}

func (h *AccessHandler) SetGroupNodes(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Groups == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户组服务不可用")
		return
	}
	groupID, err := parsePositiveID(r.PathValue("id"))
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "用户组 id 无效")
		return
	}
	var request struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.NodeIDs == nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeIds 必须是数组")
		return
	}
	if err := h.Groups.SetNodeIDs(r.Context(), groupID, request.NodeIDs); err != nil {
		if errors.Is(err, repo.ErrNodeGroupNotFound) || errors.Is(err, sql.ErrNoRows) {
			middleware.Err(w, http.StatusNotFound, "not_found", "用户组或节点不存在")
			return
		}
		middleware.Err(w, http.StatusBadRequest, "bad_request", "节点授权无效")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"id": groupID, "nodeIds": request.NodeIDs})
}

func (h *AccessHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Users == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户服务不可用")
		return
	}
	users, err := h.Users.ListAdminUsers(r.Context())
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "用户查询失败")
		return
	}
	if users == nil {
		users = make([]repo.AdminUser, 0)
	}
	middleware.JSON(w, http.StatusOK, users)
}

func (h *AccessHandler) SetUserGroups(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Users == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "用户服务不可用")
		return
	}
	userID, err := parsePositiveID(r.PathValue("id"))
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "用户 id 无效")
		return
	}
	var request struct {
		GroupIDs []int64 `json:"groupIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.GroupIDs == nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "groupIds 必须是数组")
		return
	}
	if err := h.Users.SetUserGroupIDs(r.Context(), userID, request.GroupIDs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			middleware.Err(w, http.StatusNotFound, "not_found", "用户或用户组不存在")
			return
		}
		middleware.Err(w, http.StatusBadRequest, "bad_request", "用户组授权无效")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"id": userID, "groupIds": request.GroupIDs})
}

func parsePositiveID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
