package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/backend/internal/topology"
	"github.com/go-chi/chi/v5"
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
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	var request struct {
		models.TopologyDraft
		ExpectedRevision *int64                  `json:"expectedRevision"`
		InboundModes     []repo.GraphInboundMode `json:"inboundModes"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
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
	if _, persistent := h.Service.Drafts.(*repo.TopologyRepo); persistent {
		graph, graphErr := h.Service.Graph(r.Context())
		if graphErr != nil {
			writeTopologyError(w, graphErr)
			return
		}
		var current *repo.GraphSnapshotNode
		for index := range graph.Nodes {
			if graph.Nodes[index].NodeID == nodeID {
				current = &graph.Nodes[index]
				break
			}
		}
		if current == nil {
			writeTopologyError(w, sql.ErrNoRows)
			return
		}
		expected := current.Revision
		if request.ExpectedRevision != nil {
			expected = *request.ExpectedRevision
		} else if request.Revision > 0 {
			expected = request.Revision
		} else if len(request.InboundModes) == 0 {
			for _, inbound := range current.Inbounds {
				if inbound.Role != "entry" {
					continue
				}
				mode := "direct"
				for _, edge := range request.Edges {
					if edge.FromInboundID == inbound.ID {
						mode = "chain"
					}
				}
				request.InboundModes = append(request.InboundModes, repo.GraphInboundMode{InboundID: inbound.ID, EgressMode: mode})
			}
		}
		if len(request.Layout) == 0 {
			request.Layout = current.Layout
		}
		result, saveErr := h.Service.SaveGraph(r.Context(), graph.GraphRevision, []repo.GraphChange{{NodeID: nodeID, ExpectedRevision: expected, Edges: request.Edges, Layout: request.Layout, InboundModes: request.InboundModes}})
		if saveErr != nil {
			if errors.Is(saveErr, repo.ErrTopologyPreviewStale) {
				middleware.Err(w, 409, "revision_conflict", "草稿版本已变化，请重新读取")
				return
			}
			writeTopologyError(w, saveErr)
			return
		}
		for _, node := range result.Nodes {
			if node.NodeID == nodeID {
				middleware.JSON(w, 200, node)
				return
			}
		}
		return
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
	if _, persistent := h.Service.Drafts.(*repo.TopologyRepo); persistent {
		graph, graphErr := h.Service.Graph(r.Context())
		if graphErr != nil {
			writeTopologyError(w, graphErr)
			return
		}
		chain := graphHasDependencies(graph, nodeID)
		for _, node := range graph.Nodes {
			if node.NodeID == nodeID {
				for _, capability := range node.Capabilities {
					if capability == "topology-chain-v2" {
						chain = true
					}
				}
			}
		}
		if chain {
			preview, previewErr := h.Service.PreviewChain(r.Context(), nodeID)
			if previewErr != nil {
				writeTopologyError(w, previewErr)
				return
			}
			middleware.JSON(w, 200, preview)
			return
		}
	}
	preview, err := h.Service.Preview(r.Context(), nodeID)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, preview)
}

func (h *TopologyHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	nodeID, err := topologyNodeID(r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	var request struct {
		Version               string `json:"version"`
		PreviewID             string `json:"previewId"`
		ExpectedGraphRevision int64  `json:"expectedGraphRevision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || (request.Version == "" && request.PreviewID == "") {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "必须提供预览版本")
		return
	}
	if request.PreviewID != "" {
		store, ok := h.Service.Drafts.(*repo.TopologyRepo)
		if !ok {
			writeTopologyError(w, topology.ErrInvalidTopology)
			return
		}
		graph, err := store.Graph(r.Context())
		if err != nil {
			writeTopologyError(w, err)
			return
		}
		if graph.GraphRevision != request.ExpectedGraphRevision {
			writeTopologyError(w, repo.ErrTopologyPreviewStale)
			return
		}
		release, err := store.CreateTopologyRelease(r.Context(), request.PreviewID, nodeID, middleware.UserFrom(r.Context()).ID)
		if err != nil {
			writeTopologyError(w, err)
			return
		}
		middleware.JSON(w, 202, map[string]any{"releaseId": release.ID, "id": release.ID, "status": release.Status})
		return
	}
	if h == nil || h.Service == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "拓扑服务不可用")
		return
	}
	if _, persistent := h.Service.Drafts.(*repo.TopologyRepo); persistent {
		graph, graphErr := h.Service.Graph(r.Context())
		if graphErr != nil {
			writeTopologyError(w, graphErr)
			return
		}
		if graphHasDependencies(graph, nodeID) {
			middleware.Err(w, 409, "upgrade_required", "跨节点依赖必须使用全图预览发布")
			return
		}
	}
	deployment, err := h.Service.Deploy(r.Context(), nodeID, request.Version)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"version": deployment.Version, "deployed": true})
}

func graphHasDependencies(graph *repo.GraphSnapshot, nodeID int64) bool {
	for _, node := range graph.Nodes {
		for _, edge := range node.Edges {
			if node.NodeID == nodeID || edge.ToNodeID == nodeID {
				return true
			}
		}
		if node.NodeID == nodeID {
			for _, inbound := range node.Inbounds {
				if inbound.Role == "relay" || inbound.EgressMode == "chain" {
					return true
				}
			}
		}
	}
	return false
}

func topologyNodeID(r *http.Request) (int64, error) {
	value := r.PathValue("nodeId")
	if value == "" {
		value = chi.URLParam(r, "nodeId")
	}
	nodeID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || nodeID <= 0 {
		return 0, errors.New("invalid node id")
	}
	return nodeID, nil
}

func writeTopologyError(w http.ResponseWriter, err error) {
	switch {
	case strings.Contains(err.Error(), "agent_upgrade_required"):
		middleware.Err(w, 409, "agent_upgrade_required", "依赖节点需要 topology-chain-v2 能力")
	case errors.Is(err, repo.ErrTopologyReleaseConflict), errors.Is(err, repo.ErrTopologyGenerationStale):
		middleware.Err(w, 409, "release_conflict", "节点已参与发布或版本变化")
	case errors.Is(err, repo.ErrTopologySecureStorageUnavailable):
		middleware.Err(w, 503, "secure_storage_unavailable", "拓扑候选加密配置不可用")
	case errors.Is(err, topology.ErrTopologyPreviewStale):
		middleware.Err(w, http.StatusConflict, "stale_preview", "拓扑预览已过期，请重新预览")
	case errors.Is(err, topology.ErrUnsupportedTopology):
		middleware.Err(w, http.StatusUnprocessableEntity, "unsupported_topology", "拓扑编排层级不受支持")
	case errors.Is(err, topology.ErrInvalidTopology):
		middleware.Err(w, http.StatusUnprocessableEntity, "invalid_topology", "拓扑引用或配置无效")
	case errors.Is(err, sql.ErrNoRows):
		middleware.Err(w, http.StatusNotFound, "not_found", "拓扑或节点不存在")
	default:
		middleware.Err(w, http.StatusInternalServerError, "internal", "拓扑处理失败")
	}
}

func (h *TopologyHandler) Graph(w http.ResponseWriter, r *http.Request) {
	graph, err := h.Service.Graph(r.Context())
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, 200, graph)
}
func (h *TopologyHandler) SaveGraph(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedGraphRevision int64              `json:"expectedGraphRevision"`
		Changes               []repo.GraphChange `json:"changes"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		middleware.Err(w, 422, "invalid_graph", "全图请求无效")
		return
	}
	graph, err := h.Service.SaveGraph(r.Context(), request.ExpectedGraphRevision, request.Changes)
	if err != nil {
		if errors.Is(err, repo.ErrTopologyPreviewStale) {
			middleware.Err(w, 409, "revision_conflict", "全图版本已变化，请重新读取")
			return
		}
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, 200, graph)
}
func (h *TopologyHandler) Release(w http.ResponseWriter, r *http.Request) {
	store := h.Service.Drafts.(*repo.TopologyRepo)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	release, err := store.GetRelease(r.Context(), id)
	if err != nil {
		writeTopologyError(w, err)
		return
	}
	middleware.JSON(w, 200, release)
}
func (h *TopologyHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	store := h.Service.Drafts.(*repo.TopologyRepo)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := store.RollbackRelease(r.Context(), id); err != nil {
		writeTopologyError(w, err)
		return
	}
	h.Release(w, r)
}
