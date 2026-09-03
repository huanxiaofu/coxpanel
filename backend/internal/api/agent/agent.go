// Package agent 提供 agent 通信端点（配置拉取、心跳、流量上报）。
package agent

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/backend/internal/topology"
	"github.com/coxpanel/shared/contract"
)

// Handler agent 端点依赖。
type Handler struct {
	Nodes *repo.NodeRepo
	// P2: 拓扑引擎 + 流量入库
}

// GetConfig agent 拉取本节点完整配置。
// 认证：X-Node-Id + API key（P1 简化：仅校验 X-Node-Id 存在）。
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	nodeID, err := strconv.ParseInt(r.Header.Get("X-Node-Id"), 10, 64)
	if err != nil || nodeID <= 0 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "缺少 X-Node-Id")
		return
	}
	n, err := h.Nodes.Get(r.Context(), nodeID)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	ibs, err := h.Nodes.ListInbounds(r.Context(), nodeID)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "入站查询失败")
		return
	}

	// 用拓扑引擎翻译成 sing-box 配置（P1：只含入站，edges 后续接 DB）
	cfg, err := topology.Build(n, ibs, nil)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "配置生成失败: "+err.Error())
		return
	}
	raw, err := cfg.Marshal()
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "序列化失败")
		return
	}
	resp := map[string]any{
		"version":   "v1",
		"nodeId":    nodeID,
		"nodeName":  n.Name,
		"singbox":   json.RawMessage(raw),
	}
	middleware.JSON(w, http.StatusOK, resp)
}

// Heartbeat agent 心跳上报。
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var hb contract.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if hb.NodeID <= 0 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "nodeId 无效")
		return
	}
	if err := h.Nodes.UpdateStatus(r.Context(), hb.NodeID, "online", hb.CoreVersion); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "状态更新失败")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ReportTraffic agent 流量上报（P1 存根：接受但暂不入库，P2 接入 traffic_records）。
func (h *Handler) ReportTraffic(w http.ResponseWriter, r *http.Request) {
	var tr contract.TrafficReport
	if err := json.NewDecoder(r.Body).Decode(&tr); err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true, "note": "P2 接入流量统计"})
}
