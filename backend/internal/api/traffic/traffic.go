package traffic

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/coxpanel/backend/internal/api/middleware"
	meter "github.com/coxpanel/backend/internal/traffic"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
	"time"
)

type Handler struct{ Service *meter.Service }

func fail(w http.ResponseWriter, err error) {
	status, code := 500, "internal"
	if errors.Is(err, meter.ErrInvalid) {
		status, code = 422, "traffic_invalid"
	}
	if errors.Is(err, meter.ErrConflict) {
		status, code = 409, "revision_conflict"
	}
	if errors.Is(err, sql.ErrNoRows) {
		status, code = 404, "not_found"
	}
	middleware.Err(w, status, code, "流量操作未完成，请检查参数或刷新版本")
}
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	h.query(w, r, middleware.UserFrom(r.Context()).ID, false)
}
func (h *Handler) User(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil || id <= 0 || (user.Role != "owner" && user.Role != "admin" && user.ID != id) {
		fail(w, sql.ErrNoRows)
		return
	}
	h.query(w, r, id, false)
}
func (h *Handler) Node(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, sql.ErrNoRows)
		return
	}
	h.query(w, r, id, true)
}
func (h *Handler) query(w http.ResponseWriter, r *http.Request, id int64, node bool) {
	query, err := meter.ParseQuery(r.URL.Query(), time.Now(), node)
	if err != nil {
		fail(w, err)
		return
	}
	result, err := h.Service.Query(r.Context(), id, node, query)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, result)
}
func (h *Handler) Quota(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedRevision  int64      `json:"expectedRevision"`
		TrafficLimitBytes int64      `json:"trafficLimitBytes"`
		ExpireAt          *time.Time `json:"expireAt"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request) != nil {
		fail(w, meter.ErrInvalid)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.Service.SetQuota(r.Context(), id, middleware.UserFrom(r.Context()).ID, request.ExpectedRevision, request.TrafficLimitBytes, request.ExpireAt); err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"revision": request.ExpectedRevision + 1})
}
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedQuotaEpoch int64  `json:"expectedQuotaEpoch"`
		Reason             string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request) != nil {
		fail(w, meter.ErrInvalid)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.Service.Reset(r.Context(), id, middleware.UserFrom(r.Context()).ID, request.ExpectedQuotaEpoch, request.Reason); err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"quotaEpoch": request.ExpectedQuotaEpoch + 1})
}
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var status string
	var lastSeen, lastReceived, lastComplete *time.Time
	var freshness *string
	if err := h.Service.DB.QueryRowContext(r.Context(), `SELECT n.status,n.last_seen_at,s.status,s.last_received_at,s.last_complete_at FROM nodes n LEFT JOIN traffic_node_state s ON s.node_id=n.id WHERE n.id=$1`, id).Scan(&status, &lastSeen, &freshness, &lastReceived, &lastComplete); err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"status": status, "lastSeenAt": lastSeen, "trafficFreshness": map[string]any{"status": freshness, "lastReceivedAt": lastReceived, "lastCompleteAt": lastComplete}})
}
