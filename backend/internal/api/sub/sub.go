// Package sub 提供公开订阅端点。
package sub

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

type NodeStore interface {
	List(context.Context) ([]models.Node, error)
	Get(context.Context, int64) (*models.Node, error)
	ListInbounds(context.Context, int64) ([]models.Inbound, error)
}

type GroupStore interface {
	Exists(context.Context, int64) (bool, error)
	UserHasGroup(context.Context, int64, int64) (bool, error)
	AuthorizedNodeIDs(context.Context, int64, int64) ([]int64, error)
}

type SubscriptionStore interface {
	GetByToken(context.Context, string) (*models.Subscription, error)
	ListOverrides(context.Context, int64) (map[int64]repo.OverrideRow, error)
}

type inboundOverrideStore interface {
	ListInboundOverrides(context.Context, int64) (map[repo.OverrideKey]repo.OverrideRow, error)
}

type UserStore interface {
	GetUser(context.Context, int64) (*models.User, error)
}

type userCredentialStore interface {
	GetUserCredential(int64, int64) (string, error)
}

type userCredentialContextStore interface {
	GetUserCredentialContext(context.Context, int64, int64) (string, error)
}

type userCredentialIssuer interface {
	EnsureUserCredential(context.Context, int64, int64, string) (string, error)
}

var (
	errSubscriptionOwnerUnavailable = errors.New("subscription owner unavailable")
	errSubscriptionOwnerInactive    = errors.New("subscription owner inactive")
	errOptionalAuthInvalid          = errors.New("optional authentication invalid")
	errOptionalAuthMismatch         = errors.New("optional authentication mismatch")
)

// Handler 订阅端点依赖。
type Handler struct {
	Subs   SubscriptionStore
	Nodes  NodeStore
	Groups GroupStore
	Auth   *auth.Service
	Users  UserStore
}

// Serve 处理 GET /sub/:token。
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")
	if token == "" {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	s, err := h.Subs.GetByToken(ctx, token)
	if err != nil {
		middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		return
	}
	if err := h.authorizeSubscription(ctx, r, s); err != nil {
		switch {
		case errors.Is(err, errOptionalAuthInvalid):
			middleware.Err(w, http.StatusUnauthorized, "invalid_token", "认证信息无效")
		case errors.Is(err, errOptionalAuthMismatch):
			middleware.Err(w, http.StatusForbidden, "forbidden", "认证用户无权访问该订阅")
		case errors.Is(err, errSubscriptionOwnerUnavailable), errors.Is(err, errSubscriptionOwnerInactive):
			middleware.Err(w, http.StatusNotFound, "not_found", "订阅不存在")
		default:
			middleware.Err(w, http.StatusInternalServerError, "internal", "订阅校验失败")
		}
		return
	}

	nodeIDs, err := h.nodeIDs(ctx, s)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrNodeGroupNotFound):
			middleware.Err(w, http.StatusNotFound, "group_not_found", "节点组不存在")
		case errors.Is(err, repo.ErrNodeNotAuthorized), errors.Is(err, repo.ErrNoAuthorizedNodes):
			middleware.Err(w, http.StatusForbidden, "group_not_authorized", "订阅节点组未授权")
		default:
			middleware.Err(w, http.StatusInternalServerError, "internal", "查询失败")
		}
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
					credential := h.credential(ctx, s.UserID, &ibs[i])
					proxies = append(proxies, generator.Proxy{
						Name:       n.Name + "-" + ibs[i].Name,
						Node:       n,
						Inbound:    &ibs[i],
						Credential: credential,
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
		raw, gerr := generator.GenerateURIList(proxies)
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

func (h *Handler) credential(ctx context.Context, userID int64, inbound *models.Inbound) string {
	if h.Users == nil || inbound == nil {
		return ""
	}
	var (
		credential string
		err        error
	)
	if store, ok := h.Users.(userCredentialContextStore); ok {
		credential, err = store.GetUserCredentialContext(ctx, userID, inbound.ID)
	} else if store, ok := h.Users.(userCredentialStore); ok {
		credential, err = store.GetUserCredential(userID, inbound.ID)
	} else {
		return ""
	}
	if err == nil && strings.TrimSpace(credential) != "" {
		return credential
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	issuer, ok := h.Users.(userCredentialIssuer)
	if !ok {
		return ""
	}
	credential, err = issuer.EnsureUserCredential(ctx, userID, inbound.ID, inbound.Protocol)
	if err != nil || strings.TrimSpace(credential) == "" {
		return ""
	}
	return credential
}

func (h *Handler) authorizeSubscription(ctx context.Context, r *http.Request, s *models.Subscription) error {
	if h.Users == nil || s == nil {
		return errSubscriptionOwnerUnavailable
	}
	owner, err := h.Users.GetUser(ctx, s.UserID)
	if err != nil || owner == nil {
		return errSubscriptionOwnerUnavailable
	}
	if !owner.Active || (owner.ExpireAt != nil && !time.Now().Before(*owner.ExpireAt)) {
		return errSubscriptionOwnerInactive
	}

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return nil
	}
	if h.Auth == nil || !strings.HasPrefix(header, "Bearer ") {
		return errOptionalAuthInvalid
	}
	claims, err := h.Auth.ParseToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil {
		return errOptionalAuthInvalid
	}
	if claims.UserID != s.UserID {
		return errOptionalAuthMismatch
	}
	return nil
}

// nodeIDs 计算订阅可见节点：只有显式绑定组才有节点授权。
func (h *Handler) nodeIDs(ctx context.Context, s *models.Subscription) ([]int64, error) {
	if s == nil || s.NodeGroupID == nil || h.Groups == nil {
		return nil, repo.ErrNodeNotAuthorized
	}
	groupID := *s.NodeGroupID
	exists, err := h.Groups.Exists(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, repo.ErrNodeGroupNotFound
	}
	authorized, err := h.Groups.UserHasGroup(ctx, s.UserID, groupID)
	if err != nil {
		return nil, err
	}
	if !authorized {
		return nil, repo.ErrNodeNotAuthorized
	}
	nodeIDs, err := h.Groups.AuthorizedNodeIDs(ctx, s.UserID, groupID)
	if err != nil {
		return nil, err
	}
	if len(nodeIDs) == 0 {
		return nil, repo.ErrNoAuthorizedNodes
	}
	return nodeIDs, nil
}

// applyOverrides 读取订阅覆写并应用到 proxies（按 node_id/inbound_id 匹配，旧 node 覆写作回退）。
func (h *Handler) applyOverrides(ctx context.Context, subID int64, proxies []generator.Proxy) error {
	ovs, err := h.Subs.ListOverrides(ctx, subID)
	if err != nil {
		return err
	}
	var inboundOverrides map[repo.OverrideKey]repo.OverrideRow
	if store, ok := h.Subs.(inboundOverrideStore); ok {
		inboundOverrides, err = store.ListInboundOverrides(ctx, subID)
		if err != nil {
			return err
		}
	}
	for i := range proxies {
		var ov repo.OverrideRow
		var ok bool
		if proxies[i].Node != nil && proxies[i].Inbound != nil {
			ov, ok = inboundOverrides[repo.OverrideKey{NodeID: proxies[i].Node.ID, InboundID: proxies[i].Inbound.ID}]
		}
		if !ok && proxies[i].Node != nil {
			ov, ok = ovs[proxies[i].Node.ID]
		}
		if ok {
			var params map[string]any
			if len(ov.Params) > 0 {
				_ = json.Unmarshal(ov.Params, &params)
			}
			params = generator.SanitizeOverrideParams("", params)
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
