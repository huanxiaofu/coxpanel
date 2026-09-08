package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/shared/contract"
)

type DeploymentAuthStore interface {
	AuthenticateAgent(context.Context, int64, string) error
}

type TopologyDeploymentStore interface {
	GetAgentDeployment(context.Context, int64, int64, string) (*contract.TopologyDeploymentDocument, error)
	AcknowledgeAgentDeployment(context.Context, *contract.DeploymentAck) error
}

type PendingDeploymentStore interface {
	GetPendingAgentDeployment(context.Context, int64) (*contract.PendingDeployment, error)
}

type DeploymentHandler struct {
	Auth  DeploymentAuthStore
	Store TopologyDeploymentStore
}

func NewDeploymentHandler(auth DeploymentAuthStore, store TopologyDeploymentStore) *DeploymentHandler {
	return &DeploymentHandler{Auth: auth, Store: store}
}

func (h *DeploymentHandler) Pending(ctx context.Context, nodeID int64) (*contract.PendingDeployment, error) {
	if h == nil || h.Store == nil || nodeID <= 0 {
		return nil, nil
	}
	store, ok := h.Store.(PendingDeploymentStore)
	if !ok {
		return nil, nil
	}
	pending, err := store.GetPendingAgentDeployment(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pending == nil {
		return nil, nil
	}
	if err := pending.Validate(); err != nil {
		return nil, err
	}
	return pending, nil
}

func (h *DeploymentHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	phase := strings.TrimSpace(r.URL.Query().Get("phase"))
	releaseID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("releaseId")), 10, 64)
	if err != nil || releaseID <= 0 || phase == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "部署参数无效")
		return
	}
	if h == nil || h.Store == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "部署服务不可用")
		return
	}
	document, err := h.Store.GetAgentDeployment(r.Context(), nodeID, releaseID, phase)
	if err != nil {
		writeDeploymentError(w, err)
		return
	}
	if document == nil || document.NodeID != nodeID || document.ReleaseID != releaseID || document.Phase != phase {
		middleware.Err(w, http.StatusInternalServerError, "internal", "部署文档绑定无效")
		return
	}
	if err := document.Validate(true); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "部署文档无效")
		return
	}
	body, err := document.JSONBytes()
	if err != nil {
		middleware.Err(w, 500, "internal", "部署文档编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *DeploymentHandler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var ack contract.DeploymentAck
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&ack); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "回执请求体无效")
		return
	}
	if ack.NodeID != nodeID {
		middleware.Err(w, http.StatusForbidden, "node_mismatch", "回执节点与凭据不匹配")
		return
	}
	if err := ack.Validate(); err != nil {
		middleware.Err(w, http.StatusUnprocessableEntity, "deployment_ack_invalid", "部署回执无效")
		return
	}
	if h == nil || h.Store == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "部署服务不可用")
		return
	}
	if err := h.Store.AcknowledgeAgentDeployment(r.Context(), &ack); err != nil {
		writeDeploymentError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *DeploymentHandler) authenticate(w http.ResponseWriter, r *http.Request) (int64, bool) {
	nodeID, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(contract.AgentNodeIDHeader)), 10, 64)
	if err != nil || nodeID <= 0 {
		middleware.Err(w, http.StatusUnauthorized, "agent_auth_required", "需要节点认证")
		return 0, false
	}
	credential := strings.TrimSpace(r.Header.Get(contract.AgentCredentialHeader))
	if credential == "" || h == nil || h.Auth == nil || h.Auth.AuthenticateAgent(r.Context(), nodeID, credential) != nil {
		middleware.Err(w, http.StatusUnauthorized, "invalid_agent_credentials", "节点凭据无效")
		return 0, false
	}
	return nodeID, true
}

func writeDeploymentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrTopologyReleaseNotFound), errors.Is(err, http.ErrServerClosed):
		middleware.Err(w, http.StatusNotFound, "not_found", "部署发布不存在")
	case errors.Is(err, repo.ErrTopologyGenerationStale), errors.Is(err, repo.ErrTopologyReleaseConflict):
		middleware.Err(w, http.StatusConflict, "deployment_conflict", "部署状态已变化")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		middleware.Err(w, http.StatusRequestTimeout, "timeout", "部署请求超时")
	default:
		middleware.Err(w, http.StatusInternalServerError, "internal", "部署处理失败")
	}
}
