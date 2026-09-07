package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/topology"
)

type TopologyHandler struct {
	Service *topology.Service
}

func NewTopologyHandler(service *topology.Service) *TopologyHandler {
	return &TopologyHandler{Service: service}
}

func (h *TopologyHandler) GetDraft(w http.ResponseWriter, r *http.Request) {
	nodeID, err := topologyNodeID(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	draft, err := h.Service.Draft(r.Context(), nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		draft = &models.TopologyDraft{NodeID: nodeID, Edges: make([]models.TopologyEdge, 0)}
	}
	if err != nil && draft == nil {
		writeTopologyError(w, err)
		return
	}
	if draft.Edges == nil {
		draft.Edges = make([]models.TopologyEdge, 0)
	}
	middleware.JSON(w, http.StatusOK, draft)
}

func (h *TopologyHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	nodeID, err := topologyNodeID(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	var request models.TopologyDraft
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if request.NodeID != 0 && request.NodeID != nodeID {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 与路径不匹配")
		return
	}
	if request.Edges == nil {
		request.Edges = make([]models.TopologyEdge, 0)
	}
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	draft, err := h.Service.SaveDraft(r.Context(), nodeID, request.Edges)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, draft)
}

func (h *TopologyHandler) Preview(w http.ResponseWriter, r *http.Request) {
	nodeID, err := topologyNodeID(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	preview, err := h.Service.Preview(r.Context(), nodeID)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, preview)
}

func (h *TopologyHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	nodeID, err := topologyNodeID(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	var request struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Version == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "必须提供预览版本")
		return
	}
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	deployment, err := h.Service.Deploy(r.Context(), nodeID, request.Version)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"version": deployment.Version, "deployed": true})
}

func topologyNodeID(r *http.Request) (int64, error) {
	value := r.PathValue("nodeId")
	nodeID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || nodeID <= 0 {
		return 0, errors.New("invalid node id")
	}
	return nodeID, nil
}

func writeTopologyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, topology.ErrTopologyPreviewStale):
		middleware.Err(w, http.StatusConflict, "stale_preview", "拓扑预览已过期，请重新预览")
	case errors.Is(err, topology.ErrUnsupportedTopology):
		middleware.Err(w, http.StatusUnprocessableEntity, "unsupported_topology", "拓扑编排层级不受支持")
	case errors.Is(err, topology.ErrInvalidTopology):
		middleware.Err(w, http.StatusBadRequest, "invalid_topology", "拓扑引用或配置无效")
	case errors.Is(err, sql.ErrNoRows):
		middleware.Err(w, http.StatusNotFound, "not_found", "拓扑或节点不存在")
	default:
		middleware.Err(w, http.StatusInternalServerError, "internal", "拓扑处理失败")
	}
}
