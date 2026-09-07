// Package api 组装 HTTP 路由。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/coxpanel/backend/internal/api/admin"
	"github.com/coxpanel/backend/internal/api/agent"
	apimid "github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/api/sub"
	"github.com/coxpanel/backend/internal/api/user"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/backend/internal/topology"
)

// Deps 依赖集合。
type Deps struct {
	AuthSvc  *auth.Service
	Users    *repo.UserRepo
	Nodes    *repo.NodeRepo
	Subs     *repo.SubscriptionRepo
	Groups   *repo.GroupRepo
	Topology *repo.TopologyRepo
}

// NewRouter 构建路由。
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	authH := &admin.Handler{Auth: d.AuthSvc, Users: d.Users}
	nodeH := &admin.NodeHandler{Nodes: d.Nodes}
	accessH := &admin.AccessHandler{Groups: d.Groups, Users: d.Users}
	topologyH := admin.NewTopologyHandler(topology.NewService(d.Nodes, d.Topology))
	subH := &sub.Handler{Subs: d.Subs, Nodes: d.Nodes, Groups: d.Groups, Auth: d.AuthSvc, Users: d.Users}
	userH := &user.Handler{Subs: d.Subs, Nodes: d.Nodes}
	userGroupH := &user.GroupHandler{Groups: d.Groups}

	// 公开端点
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", authH.Login)
		r.Post("/register", authH.Register)
	})
	r.Get("/sub/{token}", subH.Serve) // 公开订阅

	// agent 端点（X-Node-Id + API key 认证，P1 简化）
	agentH := &agent.Handler{Nodes: d.Nodes, Users: d.Users, Deployments: d.Topology}
	r.Get("/api/agent/config", agentH.GetConfig)
	r.Post("/api/agent/heartbeat", agentH.Heartbeat)
	r.Post("/api/agent/report-traffic", agentH.ReportTraffic)

	// 登录用户
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc, d.Users))
		r.Get("/api/me", authH.Me)
		r.Get("/api/my/groups", userGroupH.ListMyGroups)
		r.Get("/api/my/subscriptions", userH.ListMySubs)
		r.Post("/api/my/subscriptions", userH.CreateSub)
		r.Delete("/api/my/subscriptions/{id}", userH.DeleteSub)
		r.Put("/api/my/subscriptions/{id}/overrides/{nodeId}", userH.SaveOverride)
		r.Put("/api/my/subscriptions/{id}/overrides/{nodeId}/inbounds/{inboundId}", userH.SaveInboundOverride)
	})

	// 管理员端点
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc, d.Users))
		r.Use(apimid.RequireAdmin)

		r.Route("/api/nodes", func(r chi.Router) {
			r.Get("/", nodeH.ListNodes)
			r.Post("/", nodeH.CreateNode)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", nodeH.GetNode)
				r.Put("/", nodeH.UpdateNode)
				r.Delete("/", nodeH.DeleteNode)
				r.Get("/inbounds", nodeH.ListInbounds)
				r.Post("/inbounds", nodeH.CreateInbound)
				r.Delete("/inbounds/{inboundId}", nodeH.DeleteInbound)
			})
		})

		r.Route("/api/invites", func(r chi.Router) {
			r.Get("/", authH.ListInvites)
			r.Post("/", authH.GenInviteCode)
		})
		r.Get("/api/groups", accessH.ListGroups)
		r.Get("/api/groups/", accessH.ListGroups)
		r.Post("/api/groups", accessH.CreateGroup)
		r.Post("/api/groups/", accessH.CreateGroup)
		r.Put("/api/groups/{id}/nodes", accessH.SetGroupNodes)
		r.Get("/api/users", accessH.ListUsers)
		r.Get("/api/users/", accessH.ListUsers)
		r.Put("/api/users/{id}/groups", accessH.SetUserGroups)
		r.Get("/api/topology/{nodeId}", topologyH.GetDraft)
		r.Get("/api/topology/{nodeId}/", topologyH.GetDraft)
		r.Put("/api/topology/{nodeId}", topologyH.SaveDraft)
		r.Put("/api/topology/{nodeId}/", topologyH.SaveDraft)
		r.Post("/api/topology/{nodeId}/preview", topologyH.Preview)
		r.Post("/api/topology/{nodeId}/deploy", topologyH.Deploy)
	})

	return r
}
