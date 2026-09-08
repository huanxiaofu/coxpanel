package templates

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coxpanel/backend/internal/api/middleware"
)

func (h *Handler) SaveGroupDefaults(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	if user == nil || user.Role != "owner" && user.Role != "admin" {
		middleware.Err(w, 403, "forbidden", "需要管理权限")
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, 400, "bad_request", "节点组 id 无效")
		return
	}
	var request struct {
		ExpectedRevision int64           `json:"expectedRevision"`
		Defaults         json.RawMessage `json:"defaults"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.ExpectedRevision < 1 {
		middleware.Err(w, 400, "bad_request", "请求体无效")
		return
	}
	store, ok := h.Templates.(interface {
		SaveGroupDefaults(context.Context, int64, int64, json.RawMessage) (int64, error)
	})
	if !ok {
		middleware.Err(w, 503, "unavailable", "默认值服务不可用")
		return
	}
	revision, err := store.SaveGroupDefaults(r.Context(), id, request.ExpectedRevision, request.Defaults)
	if err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"revision": revision, "defaults": request.Defaults})
}
