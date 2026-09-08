// Package agent 提供 agent 通信端点（配置拉取、心跳、流量上报）。
package agent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/traffic"
	sharedconfig "github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

// NodeStore 是 agent 端点需要的节点数据访问接口。
type NodeStore interface {
	AuthenticateAgent(context.Context, int64, string) error
	Get(context.Context, int64) (*models.Node, error)
	ListInbounds(context.Context, int64) ([]models.Inbound, error)
	UpdateStatus(context.Context, int64, string, string) error
}

type UserCredentialStore interface {
	ListUserCredentialsForNode(context.Context, int64) ([]models.UserCredential, error)
}

type DeploymentStore interface {
	GetDeploymentForAgent(context.Context, int64) (*models.TopologyDeployment, error)
}

// Handler agent 端点依赖。
type Handler struct {
	Nodes        NodeStore
	Users        UserCredentialStore
	Deployments  DeploymentStore
	DeploymentV2 *DeploymentHandler
	Traffic      *traffic.Service
	StatsListen  string
}

// GetConfig agent 拉取本节点完整配置。
// 认证：X-Node-Id + X-Agent-Credential，凭据只绑定一个节点。
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("phase") != "" || r.URL.Query().Get("releaseId") != "" {
		if h == nil || h.DeploymentV2 == nil {
			middleware.Err(w, http.StatusServiceUnavailable, "deployment_unavailable", "部署服务不可用")
			return
		}
		h.DeploymentV2.GetConfig(w, r)
		return
	}
	nodeID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	n, err := h.Nodes.Get(r.Context(), nodeID)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if n == nil || n.ID != nodeID {
		middleware.Err(w, http.StatusInternalServerError, "internal", "节点绑定无效")
		return
	}
	if n.Type != "managed" {
		middleware.Err(w, http.StatusUnprocessableEntity, "unsupported_deployment", "节点部署类型不支持 agent 配置应用")
		return
	}
	if h.Deployments == nil {
		middleware.Err(w, http.StatusNotFound, "not_deployed", "节点尚未明确部署拓扑")
		return
	}
	deployment, err := h.Deployments.GetDeploymentForAgent(r.Context(), nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.Err(w, http.StatusNotFound, "not_deployed", "节点尚未明确部署拓扑")
		return
	}
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "已部署拓扑查询失败")
		return
	}
	node, err := snapshotNode(n, deployment)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "已部署拓扑无效")
		return
	}

	var credentials []models.UserCredential
	if h.Users != nil {
		credentials, err = h.Users.ListUserCredentialsForNode(r.Context(), nodeID)
		if err != nil {
			middleware.Err(w, http.StatusInternalServerError, "internal", "用户凭据查询失败")
			return
		}
	}
	node.Credentials, err = deployedCredentials(node, credentials)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "用户凭据协议无效")
		return
	}
	runtimeNode, err := sharedconfig.RuntimeNode(node)
	if err != nil {
		middleware.Err(w, http.StatusUnprocessableEntity, "invalid_config", "配置生成失败")
		return
	}
	runtimeNode.TrafficStatsListen = h.StatsListen
	rendered, err := sharedconfig.RenderRuntime(runtimeNode)
	if err != nil {
		middleware.Err(w, http.StatusUnprocessableEntity, "invalid_config", "配置生成失败")
		return
	}
	document := contract.NewConfigDocument(runtimeNode, *rendered, true)
	writeConfigDocument(w, document)
}

func deployedCredentials(node sharedconfig.NodeConfig, credentials []models.UserCredential) ([]sharedconfig.UserCredential, error) {
	inbounds := make(map[int64]sharedconfig.Inbound, len(node.Inbounds))
	for _, inbound := range node.Inbounds {
		inbounds[inbound.ID] = inbound
	}
	result := make([]sharedconfig.UserCredential, 0, len(credentials))
	for _, credential := range credentials {
		inbound, ok := inbounds[credential.InboundID]
		if !ok || inbound.Role != "entry" || inbound.Protocol != credential.Protocol {
			continue
		}
		sharedCredential := sharedconfig.UserCredential{
			UserID: credential.UserID, InboundID: credential.InboundID,
			Name: credential.Username, Protocol: credential.Protocol,
		}
		switch credential.Protocol {
		case "vless-reality":
			sharedCredential.UUID = credential.Credential
		case "shadowsocks", "hysteria2":
			sharedCredential.Password = credential.Credential
		default:
			return nil, errors.New("unsupported deployed credential protocol")
		}
		result = append(result, sharedCredential)
	}
	return result, nil
}

func snapshotNode(node *models.Node, deployment *models.TopologyDeployment) (sharedconfig.NodeConfig, error) {
	if node == nil || deployment == nil || deployment.NodeID != node.ID || deployment.NodeID <= 0 {
		return sharedconfig.NodeConfig{}, errors.New("deployment node binding mismatch")
	}
	if deployment.SchemaVersion != sharedconfig.SchemaVersion || len(deployment.Topology) == 0 || len(deployment.Rendered) == 0 {
		return sharedconfig.NodeConfig{}, errors.New("deployment snapshot is incomplete")
	}
	if err := sharedconfig.ValidateRendered(deployment.Rendered); err != nil {
		return sharedconfig.NodeConfig{}, err
	}
	if sharedconfig.Hash(deployment.Rendered) != deployment.Version {
		return sharedconfig.NodeConfig{}, errors.New("deployment version hash mismatch")
	}
	var topology sharedconfig.NodeConfig
	if err := json.Unmarshal(deployment.Topology, &topology); err != nil {
		return sharedconfig.NodeConfig{}, err
	}
	if topology.NodeID != deployment.NodeID || topology.SchemaVersion != sharedconfig.SchemaVersion {
		return sharedconfig.NodeConfig{}, errors.New("deployment topology binding mismatch")
	}
	if topology.Inbounds == nil {
		topology.Inbounds = make([]sharedconfig.Inbound, 0)
	}
	if topology.Edges == nil {
		topology.Edges = make([]sharedconfig.Edge, 0)
	}
	topology.Credentials = nil
	expected, err := sharedconfig.RenderRouting(topology)
	if err != nil || expected.Version != deployment.Version || !bytes.Equal(expected.Content, deployment.Rendered) {
		return sharedconfig.NodeConfig{}, errors.New("deployment snapshot content mismatch")
	}
	return topology, nil
}

func writeConfigDocument(w http.ResponseWriter, document contract.ConfigDocument) {
	metadata, err := json.Marshal(struct {
		SchemaVersion string                  `json:"schemaVersion"`
		Version       string                  `json:"version"`
		SHA256        string                  `json:"sha256"`
		NodeID        int64                   `json:"nodeId"`
		Explicit      bool                    `json:"explicit"`
		Topology      sharedconfig.NodeConfig `json:"topology,omitempty"`
	}{
		SchemaVersion: document.SchemaVersion,
		Version:       document.Version,
		SHA256:        document.SHA256,
		NodeID:        document.NodeID,
		Explicit:      document.Explicit,
		Topology:      document.Topology,
	})
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "配置序列化失败")
		return
	}
	body := make([]byte, 0, len(metadata)+len(document.Singbox)+len(",\"singbox\":"))
	body = append(body, metadata[:len(metadata)-1]...)
	body = append(body, []byte(",\"singbox\":")...)
	body = append(body, document.Singbox...)
	body = append(body, '}')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func sharedNode(node *models.Node, inbounds []models.Inbound, credentialSets ...[]models.UserCredential) (sharedconfig.NodeConfig, error) {
	result := sharedconfig.NodeConfig{
		SchemaVersion: sharedconfig.SchemaVersion,
		NodeID:        node.ID,
		NodeName:      node.Name,
		Inbounds:      make([]sharedconfig.Inbound, 0, len(inbounds)),
		Edges:         []sharedconfig.Edge{},
		Outbound:      "direct",
	}
	if len(credentialSets) > 0 {
		result.Credentials = make([]sharedconfig.UserCredential, 0, len(credentialSets[0]))
		for _, credential := range credentialSets[0] {
			sharedCredential := sharedconfig.UserCredential{
				UserID:    credential.UserID,
				InboundID: credential.InboundID,
				Name:      credential.Username,
				Protocol:  credential.Protocol,
			}
			switch credential.Protocol {
			case "vless-reality":
				sharedCredential.UUID = credential.Credential
			case "shadowsocks", "hysteria2":
				sharedCredential.Password = credential.Credential
			}
			result.Credentials = append(result.Credentials, sharedCredential)
		}
	}
	for _, inbound := range inbounds {
		if inbound.NodeID != node.ID {
			return sharedconfig.NodeConfig{}, errors.New("inbound node binding mismatch")
		}
		params := map[string]string{}
		if len(inbound.Config) > 0 {
			if err := json.Unmarshal(inbound.Config, &params); err != nil {
				return sharedconfig.NodeConfig{}, err
			}
		}
		result.Inbounds = append(result.Inbounds, sharedconfig.Inbound{
			ID:       inbound.ID,
			Name:     inbound.Name,
			Protocol: inbound.Protocol,
			Role:     inbound.Role,
			Listen:   inbound.ListenAddr,
			Port:     inbound.ListenPort,
			Params:   params,
		})
	}
	return result, nil
}

// Heartbeat agent 心跳上报。
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var hb contract.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if hb.NodeID <= 0 || hb.NodeID != nodeID {
		middleware.Err(w, http.StatusForbidden, "node_mismatch", "心跳节点与凭据不匹配")
		return
	}
	if store, ok := h.Nodes.(interface {
		UpdateAgentCapabilities(context.Context, int64, []string, int64) error
	}); ok {
		if err := store.UpdateAgentCapabilities(r.Context(), nodeID, hb.Capabilities, hb.ConfigGeneration); err != nil {
			middleware.Err(w, 422, "invalid_capabilities", "能力上报无效")
			return
		}
	}
	if err := h.Nodes.UpdateStatus(r.Context(), hb.NodeID, "online", hb.CoreVersion); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "状态更新失败")
		return
	}
	var pending *contract.PendingDeployment
	var err error
	if h.DeploymentV2 != nil {
		pending, err = h.DeploymentV2.Pending(r.Context(), nodeID)
		if err != nil {
			middleware.Err(w, http.StatusInternalServerError, "deployment_pending_unavailable", "部署状态查询失败")
			return
		}
	}
	middleware.JSON(w, http.StatusOK, contract.HeartbeatResponse{OK: true, PendingDeployment: pending})
}

// ReportTraffic agent 流量上报（P1 存根：接受但暂不入库，P2 接入 traffic_records）。
func (h *Handler) ReportTraffic(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&raw); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if json.Unmarshal(raw, &header) != nil {
		middleware.Err(w, 400, "bad_request", "请求体无效")
		return
	}
	if header.SchemaVersion == "traffic/v2" {
		var report traffic.Report
		if json.Unmarshal(raw, &report) != nil {
			middleware.Err(w, 422, "traffic_invalid", "流量数据无效")
			return
		}
		if h.Traffic == nil {
			middleware.Err(w, 503, "traffic_unavailable", "流量存储不可用")
			return
		}
		receipt, err := h.Traffic.Ingest(r.Context(), nodeID, report)
		if err != nil {
			status, code := 500, "internal"
			if errors.Is(err, traffic.ErrInvalid) {
				status, code = 422, "traffic_invalid"
			}
			if errors.Is(err, traffic.ErrConflict) {
				status, code = 409, "traffic_sequence_conflict"
			}
			middleware.Err(w, status, code, "流量上报未接受")
			return
		}
		middleware.JSON(w, 200, receipt)
		return
	}
	var tr contract.TrafficReport
	if json.Unmarshal(raw, &tr) != nil {
		middleware.Err(w, 400, "bad_request", "请求体无效")
		return
	}
	if tr.NodeID <= 0 || tr.NodeID != nodeID {
		middleware.Err(w, http.StatusForbidden, "node_mismatch", "流量节点与凭据不匹配")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true, "persisted": false, "note": "v1 不计量，请升级 traffic/v2"})
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (int64, bool) {
	nodeID, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Node-Id")), 10, 64)
	if err != nil || nodeID <= 0 {
		middleware.Err(w, http.StatusUnauthorized, "agent_auth_required", "需要节点认证")
		return 0, false
	}
	credential := strings.TrimSpace(r.Header.Get("X-Agent-Credential"))
	if credential == "" || h.Nodes == nil || h.Nodes.AuthenticateAgent(r.Context(), nodeID, credential) != nil {
		middleware.Err(w, http.StatusUnauthorized, "invalid_agent_credentials", "节点凭据无效")
		return 0, false
	}
	return nodeID, true
}
