// Package sub 提供公开订阅端点。
package sub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

// Handler 订阅端点依赖。
type Handler struct {
	Subs   *repo.SubscriptionRepo
	Nodes  *repo.NodeRepo
	Groups *repo.GroupRepo
}

// Serve 处理 GET /sub/:token。
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")
	s, err := h.Subs.GetByToken(ctx, token)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}

	nodeIDs, err := h.nodeIDs(ctx, s)
	if err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		return
	}

	// 构建 Proxy 列表
	var proxies []generator.Proxy
	for _, nid := range nodeIDs {
		n, err := h.Nodes.Get(ctx, nid)
		if err != nil {
			continue
		}
		switch n.Type {
		case "external":
			proxies = append(proxies, generator.Proxy{Name: n.Name, Node: n})
		case "managed":
			ibs, err := h.Nodes.ListInbounds(ctx, nid)
			if err != nil {
				continue
			}
			for i := range ibs {
				if ibs[i].Role == "entry" {
					proxies = append(proxies, generator.Proxy{
						Name:    n.Name + "-" + ibs[i].Name,
						Node:    n,
						Inbound: &ibs[i],
					})
				}
			}
		}
	}

	// 加载并应用覆写
	if err := h.applyOverrides(ctx, s.ID, proxies); err != nil {
		middleware.Err(w, http.StatusInternalServerError, "internal", "覆写加载失败")
		return
	}

	var body []byte
	switch s.Format {
	case "mihomo", "":
		body, err = generator.GenerateMihomo(proxies, s.Name)
		if err != nil {
			middleware.Err(w, http.StatusInternalServerError, "internal", "生成失败: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	case "base64":
		raw, gerr := generator.GenerateMihomo(proxies, s.Name)
		if gerr != nil {
			middleware.Err(w, http.StatusInternalServerError, "internal", "生成失败")
			return
		}
		body = []byte(base64.StdEncoding.EncodeToString(raw))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		middleware.Err(w, http.StatusNotImplemented, "not_implemented", "该格式暂不支持")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=subscription")
	w.Write(body)
}

// nodeIDs 计算订阅可见节点：绑定组 → 组内节点；未绑定 → 全部节点。
func (h *Handler) nodeIDs(ctx context.Context, s *models.Subscription) ([]int64, error) {
	if s.NodeGroupID != nil {
		return h.Groups.NodeIDs(ctx, *s.NodeGroupID)
	}
	nodes, err := h.Nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids, nil
}

// applyOverrides 读取订阅覆写并应用到 proxies（按 node_id 匹配）。
func (h *Handler) applyOverrides(ctx context.Context, subID int64, proxies []generator.Proxy) error {
	ovs, err := h.Subs.ListOverrides(ctx, subID)
	if err != nil {
		return err
	}
	for i := range proxies {
		if ov, ok := ovs[proxies[i].Node.ID]; ok {
			var params map[string]any
			if len(ov.Params) > 0 {
				_ = json.Unmarshal(ov.Params, &params)
			}
			proxies[i].Override = &generator.OverrideData{
				DisplayName: ov.DisplayName,
				SortOrder:   ov.SortOrder,
				Icon:        ov.Icon,
				Params:      params,
				ProxyGroup:  ov.ProxyGroup,
			}
		}
	}
	return nil
}
