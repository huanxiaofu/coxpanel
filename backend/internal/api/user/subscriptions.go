package user

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

func (h *Handler) subscriptionPath(w http.ResponseWriter, r *http.Request) (*auth.Claims, int64, bool) {
	claims := middleware.ClaimsFrom(r.Context())
	if claims == nil || h.Subs == nil {
		middleware.Err(w, http.StatusUnauthorized, "missing_claims", "认证信息缺失")
		return nil, 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "订阅 id 无效")
		return nil, 0, false
	}
	return claims, id, true
}

func decodeJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 512*1024+1))
	if err != nil || len(body) > 512*1024 {
		return errors.New("request body limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("one JSON value required")
	}
	return nil
}

func validateSubscriptionInput(format string, templateID *int64, version *int) error {
	return repo.ValidateSubscriptionSelection(format, templateID, version)
}

func updateSubscription(store SubscriptionStore, ctx context.Context, userID, id, revision int64, name string, groupID, templateID *int64, version *int, format string) (*models.Subscription, error) {
	updater, ok := store.(interface {
		UpdateForUser(context.Context, int64, int64, int64, string, *int64, *int64, *int, string) (*models.Subscription, error)
	})
	if !ok {
		return nil, errors.New("subscription updates unavailable")
	}
	if strings.TrimSpace(name) == "" || len(name) > 128 || strings.ContainsAny(name, "\r\n\x00") {
		return nil, repo.ErrInvalidOverride
	}
	return updater.UpdateForUser(ctx, userID, id, revision, name, groupID, templateID, version, format)
}

func (h *Handler) authorizedProxies(ctx context.Context, subscription *models.Subscription) ([]generator.Proxy, error) {
	if subscription.NodeGroupID == nil || h.Groups == nil || h.Nodes == nil {
		return nil, repo.ErrNodeNotAuthorized
	}
	allowed, err := h.Groups.UserHasGroup(ctx, subscription.UserID, *subscription.NodeGroupID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, repo.ErrNodeNotAuthorized
	}
	nodeIDs, err := h.Groups.AuthorizedNodeIDs(ctx, subscription.UserID, *subscription.NodeGroupID)
	if err != nil {
		return nil, err
	}
	proxies := make([]generator.Proxy, 0)
	for _, nodeID := range nodeIDs {
		node, err := h.Nodes.Get(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		if node.Type == "external" {
			proxies = append(proxies, generator.Proxy{Name: node.Name, Node: node})
			continue
		}
		inbounds, err := h.Nodes.ListInbounds(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		for index := range inbounds {
			if inbounds[index].Role == "entry" {
				proxies = append(proxies, generator.Proxy{Name: node.Name + "-" + inbounds[index].Name, Node: node, Inbound: &inbounds[index]})
			}
		}
	}
	return proxies, nil
}

func (h *Handler) definition(ctx context.Context, subscription *models.Subscription) (json.RawMessage, error) {
	if subscription.TemplateID == nil {
		return nil, nil
	}
	if h.Templates == nil {
		return nil, repo.ErrTemplateNotFound
	}
	version, err := h.Templates.ResolveVersion(ctx, subscription.TemplateID, subscription.TemplateVersion)
	if err != nil {
		return nil, err
	}
	if version == nil || version.Format != "" && version.Format != subscription.Format {
		return nil, repo.ErrTemplateFormatMismatch
	}
	return version.Definition, nil
}

func subscriptionFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrTemplateInUse):
		middleware.Err(w, 409, "template_in_use", "覆写仍引用目标模板不支持的分组或字段")
	case errors.Is(err, generator.ErrOverrideFieldForbidden), errors.Is(err, repo.ErrOverrideFieldForbidden):
		middleware.Err(w, 422, "override_field_forbidden", "不支持该协议或格式的覆写字段")
	case errors.Is(err, generator.ErrInvalidOverride), errors.Is(err, generator.ErrTemplateInvalid), errors.Is(err, generator.ErrTemplateFieldForbidden):
		middleware.Err(w, 422, "invalid_override", "覆写字段值或模板无效")
	case errors.Is(err, repo.ErrNodeNotAuthorized), errors.Is(err, repo.ErrSubscriptionNotFound):
		middleware.Err(w, 404, "not_found", "订阅或条目不存在")
	default:
		if !writeSubscriptionError(w, err) {
			middleware.Err(w, 500, "internal", "订阅处理失败")
		}
	}
}
