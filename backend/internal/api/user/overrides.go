package user

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

type overrideWrite struct {
	repo.OverridePatch
	ExpectedRevision   *int64 `json:"expectedRevision"`
	ConfirmInsecure    bool   `json:"confirmInsecure"`
	ObfsPasswordAction string `json:"obfsPasswordAction"`
}

func overrideView(row repo.OverrideRow) map[string]any {
	params := map[string]any{}
	_ = json.Unmarshal(row.Params, &params)
	params = generator.SanitizeOverrideParams("", params)
	configured := false
	if _, exists := params["obfsPassword"]; exists {
		configured = true
		delete(params, "obfsPassword")
	}
	var order any
	if row.SortOrderSet {
		order = row.SortOrder
	}
	var name, icon, group any
	if row.DisplayName != "" {
		name = row.DisplayName
	}
	if row.Icon != "" {
		icon = row.Icon
	}
	if row.ProxyGroup != "" {
		group = row.ProxyGroup
	}
	return map[string]any{"displayName": name, "sortOrder": order, "icon": icon, "proxyGroup": group, "params": params, "configured": map[string]bool{"obfsPassword": configured}}
}

func (h *Handler) overrideContext(w http.ResponseWriter, r *http.Request) (*models.Subscription, []generator.Proxy, bool) {
	claims, id, ok := h.subscriptionPath(w, r)
	if !ok {
		return nil, nil, false
	}
	subscription, err := h.Subs.GetByIDForUser(r.Context(), claims.UserID, id)
	if err != nil {
		subscriptionFailure(w, repo.ErrSubscriptionNotFound)
		return nil, nil, false
	}
	proxies, err := h.authorizedProxies(r.Context(), subscription)
	if err != nil {
		subscriptionFailure(w, err)
		return nil, nil, false
	}
	return subscription, proxies, true
}

func proxyIDs(proxy generator.Proxy) (int64, int64) {
	var inboundID int64
	if proxy.Inbound != nil {
		inboundID = proxy.Inbound.ID
	}
	return proxy.Node.ID, inboundID
}

func protocolOf(proxy generator.Proxy) string {
	if proxy.Inbound == nil {
		return "external"
	}
	return proxy.Inbound.Protocol
}

func (h *Handler) ListOverrides(w http.ResponseWriter, r *http.Request) {
	subscription, proxies, ok := h.overrideContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Subs.EffectiveOverrideRows(r.Context(), subscription.UserID, subscription.ID)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	definition, err := h.definition(r.Context(), subscription)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	defaults, err := h.groupDefaults(r, subscription)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	entries := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		nodeID, inboundID := proxyIDs(proxy)
		row, exists := rows[repo.OverrideKey{NodeID: nodeID, InboundID: inboundID}]
		raw := row
		if !exists {
			row = rows[repo.OverrideKey{NodeID: nodeID}]
		}
		resolved, sources := generator.ResolveClientOverride(proxy, definition, defaults, rowPatch(row), subscription.Name)
		if row.Revision > 0 {
			for field, source := range sources {
				if source == "override" {
					if exists && inboundID != 0 {
						sources[field] = "inbound"
					} else {
						sources[field] = "node"
					}
				}
			}
		}
		entry := map[string]any{"nodeId": nodeID, "nodeName": proxy.Node.Name, "inboundId": inboundID, "protocol": protocolOf(proxy), "revision": raw.Revision, "raw": overrideView(raw), "effective": safeEffective(resolved), "sources": sources, "allowedFields": generator.AllowedOverrideFields(subscription.Format, protocolOf(proxy))}
		if proxy.Inbound != nil {
			entry["inboundName"] = proxy.Inbound.Name
		}
		entries = append(entries, entry)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	middleware.JSON(w, 200, map[string]any{"entries": entries, "revision": subscription.Revision})
}

func safeEffective(value generator.OverrideData) map[string]any {
	params, _ := json.Marshal(value.Params)
	return overrideView(repo.OverrideRow{DisplayName: value.DisplayName, SortOrder: value.SortOrder, SortOrderSet: true, Icon: value.Icon, ProxyGroup: value.ProxyGroup, Params: params})
}

func rowPatch(row repo.OverrideRow) generator.ClientOverride {
	patch := generator.ClientOverride{Params: map[string]any{}}
	_ = json.Unmarshal(row.Params, &patch.Params)
	if row.DisplayName != "" {
		patch.DisplayName = &row.DisplayName
	}
	if row.SortOrderSet {
		patch.SortOrder = &row.SortOrder
	}
	if row.Icon != "" {
		patch.Icon = &row.Icon
	}
	if row.ProxyGroup != "" {
		patch.ProxyGroup = &row.ProxyGroup
	}
	return patch
}

func (h *Handler) groupDefaults(r *http.Request, subscription *models.Subscription) (json.RawMessage, error) {
	if store, ok := h.Subs.(interface {
		GroupDefaults(context.Context, int64) (json.RawMessage, error)
	}); ok && subscription.NodeGroupID != nil {
		return store.GroupDefaults(r.Context(), *subscription.NodeGroupID)
	}
	return nil, nil
}

func (h *Handler) SaveOverride(w http.ResponseWriter, r *http.Request) {
	h.writeOverride(w, r, false, false)
}
func (h *Handler) SaveInboundOverride(w http.ResponseWriter, r *http.Request) {
	h.writeOverride(w, r, true, false)
}
func (h *Handler) DeleteOverride(w http.ResponseWriter, r *http.Request) {
	h.writeOverride(w, r, false, true)
}
func (h *Handler) DeleteInboundOverride(w http.ResponseWriter, r *http.Request) {
	h.writeOverride(w, r, true, true)
}

func (h *Handler) writeOverride(w http.ResponseWriter, r *http.Request, inbound, remove bool) {
	subscription, proxies, ok := h.overrideContext(w, r)
	if !ok {
		return
	}
	nodeID, err := strconv.ParseInt(r.PathValue("nodeId"), 10, 64)
	if err != nil || nodeID <= 0 {
		middleware.Err(w, 400, "bad_request", "节点 id 无效")
		return
	}
	var inboundID int64
	if inbound {
		inboundID, err = strconv.ParseInt(r.PathValue("inboundId"), 10, 64)
		if err != nil || inboundID <= 0 {
			middleware.Err(w, 400, "bad_request", "入站 id 无效")
			return
		}
	}
	var request overrideWrite
	if decodeJSON(r, &request) != nil {
		middleware.Err(w, 400, "bad_request", "请求体无效")
		return
	}
	matched := false
	for _, proxy := range proxies {
		proxyNode, proxyInbound := proxyIDs(proxy)
		if proxyNode != nodeID || inbound && proxyInbound != inboundID {
			continue
		}
		matched = true
		if !remove {
			params := map[string]any{}
			if len(request.Params) > 0 && json.Unmarshal(request.Params, &params) != nil {
				subscriptionFailure(w, generator.ErrInvalidOverride)
				return
			}
			if params["insecure"] == true && !request.ConfirmInsecure {
				subscriptionFailure(w, generator.ErrInvalidOverride)
				return
			}
			if err := generator.ValidateOverride(subscription.Format, protocolOf(proxy), request.DisplayName, request.SortOrder, request.Icon, request.ProxyGroup, params); err != nil {
				subscriptionFailure(w, err)
				return
			}
		}
	}
	if !matched {
		subscriptionFailure(w, repo.ErrNodeNotAuthorized)
		return
	}
	if request.ExpectedRevision == nil {
		middleware.Err(w, 400, "bad_request", "expectedRevision 必填")
		return
	}
	if remove {
		if inbound {
			err = h.Subs.DeleteInboundOverrideForUser(r.Context(), subscription.UserID, subscription.ID, nodeID, inboundID, *request.ExpectedRevision)
		} else {
			err = h.Subs.DeleteOverrideForUser(r.Context(), subscription.UserID, subscription.ID, nodeID, *request.ExpectedRevision)
		}
		if err != nil {
			subscriptionFailure(w, err)
			return
		}
		middleware.JSON(w, 200, map[string]any{"ok": true})
		return
	}
	if request.ObfsPasswordAction != "" && request.ObfsPasswordAction != "keep" && request.ObfsPasswordAction != "clear" && request.ObfsPasswordAction != "replace" {
		subscriptionFailure(w, generator.ErrInvalidOverride)
		return
	}
	var secretParams map[string]any
	_ = json.Unmarshal(request.Params, &secretParams)
	if _, exists := secretParams["obfsPassword"]; exists && request.ObfsPasswordAction != "replace" {
		subscriptionFailure(w, generator.ErrInvalidOverride)
		return
	}
	if request.ObfsPasswordAction == "keep" {
		rows, readErr := h.Subs.EffectiveOverrideRows(r.Context(), subscription.UserID, subscription.ID)
		if readErr != nil {
			subscriptionFailure(w, readErr)
			return
		}
		old := rowPatch(rows[repo.OverrideKey{NodeID: nodeID, InboundID: inboundID}])
		params := map[string]any{}
		_ = json.Unmarshal(request.Params, &params)
		if value, exists := old.Params["obfsPassword"]; exists {
			params["obfsPassword"] = value
		}
		request.Params, _ = json.Marshal(params)
	} else if request.ObfsPasswordAction == "clear" {
		params := map[string]any{}
		_ = json.Unmarshal(request.Params, &params)
		delete(params, "obfsPassword")
		request.Params, _ = json.Marshal(params)
	}
	var row *repo.OverrideRow
	if inbound {
		row, err = h.Subs.SaveInboundOverrideForUserAtRevision(r.Context(), subscription.UserID, subscription.ID, nodeID, inboundID, *request.ExpectedRevision, request.OverridePatch)
	} else {
		row, err = h.Subs.SaveOverrideForUserAtRevision(r.Context(), subscription.UserID, subscription.ID, nodeID, *request.ExpectedRevision, request.OverridePatch)
	}
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"revision": row.Revision, "raw": overrideView(*row)})
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	subscription, proxies, ok := h.overrideContext(w, r)
	if !ok {
		return
	}
	var request struct {
		Name            string          `json:"name"`
		NodeGroupID     *int64          `json:"nodeGroupId"`
		TemplateID      json.RawMessage `json:"templateId"`
		TemplateVersion *int            `json:"templateVersion"`
		Format          string          `json:"format"`
		Overrides       []struct {
			NodeID    int64 `json:"nodeId"`
			InboundID int64 `json:"inboundId"`
			overrideWrite
		} `json:"overrides"`
	}
	if decodeJSON(r, &request) != nil {
		middleware.Err(w, 400, "bad_request", "请求体无效")
		return
	}
	if request.Name != "" {
		subscription.Name = request.Name
	}
	if request.NodeGroupID != nil {
		subscription.NodeGroupID = request.NodeGroupID
		var loadErr error
		proxies, loadErr = h.authorizedProxies(r.Context(), subscription)
		if loadErr != nil {
			subscriptionFailure(w, loadErr)
			return
		}
	}
	if len(request.TemplateID) > 0 {
		if json.Unmarshal(request.TemplateID, &subscription.TemplateID) != nil {
			middleware.Err(w, 400, "bad_request", "模板 id 无效")
			return
		}
		subscription.TemplateVersion = request.TemplateVersion
	}
	if request.Format != "" {
		subscription.Format = request.Format
	}
	if err := validateSubscriptionInput(subscription.Format, subscription.TemplateID, subscription.TemplateVersion); err != nil {
		subscriptionFailure(w, err)
		return
	}
	definition, err := h.definition(r.Context(), subscription)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	rows, err := h.Subs.EffectiveOverrideRows(r.Context(), subscription.UserID, subscription.ID)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	defaults, err := h.groupDefaults(r, subscription)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	for _, edit := range request.Overrides {
		matched := false
		for _, proxy := range proxies {
			nodeID, inboundID := proxyIDs(proxy)
			if nodeID != edit.NodeID || edit.InboundID != 0 && inboundID != edit.InboundID {
				continue
			}
			matched = true
			params := map[string]any{}
			if len(edit.Params) > 0 && json.Unmarshal(edit.Params, &params) != nil {
				subscriptionFailure(w, generator.ErrInvalidOverride)
				return
			}
			if params["insecure"] == true && !edit.ConfirmInsecure {
				subscriptionFailure(w, generator.ErrInvalidOverride)
				return
			}
			if err := generator.ValidateOverride(subscription.Format, protocolOf(proxy), edit.DisplayName, edit.SortOrder, edit.Icon, edit.ProxyGroup, params); err != nil {
				subscriptionFailure(w, err)
				return
			}
		}
		if !matched {
			subscriptionFailure(w, repo.ErrNodeNotAuthorized)
			return
		}
		row := repo.OverrideRow{NodeID: edit.NodeID, InboundID: edit.InboundID, Params: edit.Params}
		if edit.DisplayName != nil {
			row.DisplayName = *edit.DisplayName
		}
		if edit.SortOrder != nil {
			row.SortOrder = *edit.SortOrder
			row.SortOrderSet = true
		}
		if edit.Icon != nil {
			row.Icon = *edit.Icon
		}
		if edit.ProxyGroup != nil {
			row.ProxyGroup = *edit.ProxyGroup
		}
		rows[repo.OverrideKey{NodeID: edit.NodeID, InboundID: edit.InboundID}] = row
	}
	entrySources := make([]map[string]any, 0, len(proxies))
	for index := range proxies {
		nodeID, inboundID := proxyIDs(proxies[index])
		row, exists := rows[repo.OverrideKey{NodeID: nodeID, InboundID: inboundID}]
		if !exists {
			row = rows[repo.OverrideKey{NodeID: nodeID}]
		}
		value, sources := generator.ResolveClientOverride(proxies[index], definition, defaults, rowPatch(row), subscription.Name)
		if generator.ValidateClientGroup(definition, value.ProxyGroup) != nil {
			subscriptionFailure(w, repo.ErrTemplateInUse)
			return
		}
		if err := generator.ValidateOverrideParams(subscription.Format, protocolOf(proxies[index]), rowPatch(row).Params); err != nil {
			subscriptionFailure(w, err)
			return
		}
		proxies[index].Override = &value
		entrySources = append(entrySources, map[string]any{"nodeId": nodeID, "inboundId": inboundID, "sources": sources})
	}
	content, err := generator.GenerateSubscriptionPreview(subscription.Format, proxies, subscription.Name, definition)
	if err != nil {
		subscriptionFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	middleware.JSON(w, 200, map[string]any{"config": string(content), "format": subscription.Format, "revision": subscription.Revision, "sources": entrySources, "warnings": []string{"预览不签发凭据、不保存更改；只影响客户端配置"}, "fieldErrors": []any{}, "diff": map[string]any{"changedEntries": len(request.Overrides)}})
}
