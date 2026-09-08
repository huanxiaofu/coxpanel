// Package api 组装 HTTP 路由。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/coxpanel/backend/internal/api/admin"
	"github.com/coxpanel/backend/internal/api/agent"
	mailapi "github.com/coxpanel/backend/internal/api/mail"
	apimid "github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/api/sub"
	templateapi "github.com/coxpanel/backend/internal/api/templates"
	trafficapi "github.com/coxpanel/backend/internal/api/traffic"
	"github.com/coxpanel/backend/internal/api/user"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/mail"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/coxpanel/backend/internal/topology"
	"github.com/coxpanel/backend/internal/traffic"
)

// Deps 依赖集合。
type Deps struct {
	AuthSvc     *auth.Service
	Users       *repo.UserRepo
	Nodes       *repo.NodeRepo
	Subs        *repo.SubscriptionRepo
	Groups      *repo.GroupRepo
	Topology    *repo.TopologyRepo
	Traffic     *traffic.Service
	Mail        *mail.Service
	Templates   *repo.TemplateRepo
	StatsListen string
}

// NewRouter 构建路由。
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	authH := &admin.Handler{Auth: d.AuthSvc, Users: d.Users, Mail: d.Mail}
	trafficH := &trafficapi.Handler{Service: d.Traffic}
	mailH := &mailapi.Handler{Service: d.Mail}
	nodeH := &admin.NodeHandler{Nodes: d.Nodes}
	accessH := &admin.AccessHandler{Groups: d.Groups, Users: d.Users}
	topologyH := admin.NewTopologyHandler(topology.NewService(d.Nodes, d.Topology))
	subH := &sub.Handler{Subs: d.Subs, Nodes: d.Nodes, Groups: d.Groups, Auth: d.AuthSvc, Users: d.Users, Templates: d.Templates}
	userH := &user.Handler{Subs: d.Subs, Nodes: d.Nodes, Groups: d.Groups, Users: d.Users, Templates: d.Templates}
	templateH := &templateapi.Handler{Templates: d.Templates}
	userGroupH := &user.GroupHandler{Groups: d.Groups}

	// 公开端点
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", authH.Login)
		r.Post("/register", authH.Register)
		if d.Mail != nil {
			r.Get("/email-verifications/status", mailH.Status)
			r.Post("/email-verifications", mailH.Challenge)
			r.Post("/email-verifications/confirm", mailH.Confirm)
		}
	})
	r.Get("/sub/{token}", subH.Serve) // 公开订阅

	// agent 端点（X-Node-Id + API key 认证，P1 简化）
	agentH := &agent.Handler{Nodes: d.Nodes, Users: d.Users, Deployments: d.Topology, Traffic: d.Traffic, StatsListen: d.StatsListen}
	r.Get("/api/agent/config", agentH.GetConfig)
	r.Post("/api/agent/heartbeat", agentH.Heartbeat)
	r.Post("/api/agent/report-traffic", agentH.ReportTraffic)
	deploymentH := agent.NewDeploymentHandler(d.Nodes, d.Topology)
	agentH.DeploymentV2 = deploymentH
	r.Post("/api/agent/deployment-acks", deploymentH.Acknowledge)

	// 登录用户
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc, d.Users))
		r.Get("/api/me", authH.Me)
		if d.Traffic != nil {
			r.Get("/api/my/traffic", trafficH.Mine)
			r.Get("/api/traffic/{userId}", trafficH.User)
		}
		if d.Mail != nil {
			r.Post("/api/my/email-verifications", mailH.Existing)
			r.Get("/api/my/notification-preferences", mailH.Preferences)
			r.Put("/api/my/notification-preferences", mailH.SavePreferences)
			r.Get("/api/my/notifications", mailH.Mine)
		}
		r.Get("/api/my/groups", userGroupH.ListMyGroups)
		r.Get("/api/my/subscriptions", userH.ListMySubs)
		r.Post("/api/my/subscriptions", userH.CreateSub)
		r.Get("/api/my/subscriptions/{id}", userH.GetSub)
		r.Put("/api/my/subscriptions/{id}", userH.UpdateSub)
		r.Get("/api/my/subscriptions/{id}/overrides", userH.ListOverrides)
		r.Post("/api/my/subscriptions/{id}/preview", userH.Preview)
		r.Delete("/api/my/subscriptions/{id}/overrides/{nodeId}", userH.DeleteOverride)
		r.Delete("/api/my/subscriptions/{id}/overrides/{nodeId}/inbounds/{inboundId}", userH.DeleteInboundOverride)
		r.Get("/api/templates", templateH.List)
		r.Get("/api/templates/{id}", templateH.Get)
		r.Get("/api/templates/{id}/versions", templateH.Versions)
		r.With(apimid.RequireOwner).Post("/api/templates", templateH.Create)
		r.With(apimid.RequireOwner).Put("/api/templates/{id}", templateH.Update)
		r.With(apimid.RequireOwner).Delete("/api/templates/{id}", templateH.Delete)
		r.With(apimid.RequireOwner).Post("/api/templates/{id}/validate", templateH.Validate)
		r.With(apimid.RequireOwner).Post("/api/templates/{id}/publish", templateH.Publish)
		r.Delete("/api/my/subscriptions/{id}", userH.DeleteSub)
		r.Put("/api/my/subscriptions/{id}/overrides/{nodeId}", userH.SaveOverride)
		r.Put("/api/my/subscriptions/{id}/overrides/{nodeId}/inbounds/{inboundId}", userH.SaveInboundOverride)
	})

	// 管理员端点
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc, d.Users))
		r.Use(apimid.RequireAdmin)
		r.Put("/api/groups/{id}/subscription-defaults", templateH.SaveGroupDefaults)
		if d.Traffic != nil {
			r.Get("/api/nodes/{id}/traffic", trafficH.Node)
			r.Get("/api/nodes/{id}/stats", trafficH.Stats)
			r.Put("/api/users/{id}/quota", trafficH.Quota)
			r.Post("/api/users/{id}/traffic-reset", trafficH.Reset)
		}
		if d.Mail != nil {
			r.Post("/api/invites/{id}/send-email", mailH.Invite)
		}

		r.Route("/api/nodes", func(r chi.Router) {
			r.Get("/", nodeH.ListNodes)
			r.Post("/", nodeH.CreateNode)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", nodeH.GetNode)
				r.Put("/", nodeH.UpdateNode)
				r.Delete("/", nodeH.DeleteNode)
				r.Get("/inbounds", nodeH.ListInbounds)
				r.Post("/inbounds", nodeH.CreateInbound)
				r.Put("/inbounds/{inboundId}", nodeH.UpdateInbound)
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
		r.Get("/api/topology/graph", topologyH.Graph)
		r.Put("/api/topology/graph", topologyH.SaveGraph)
		r.Get("/api/topology/releases/{id}", topologyH.Release)
		r.Post("/api/topology/releases/{id}/rollback", topologyH.Rollback)
		r.Get("/api/topology/{nodeId}/", topologyH.GetDraft)
		r.Put("/api/topology/{nodeId}", topologyH.SaveDraft)
		r.Put("/api/topology/{nodeId}/", topologyH.SaveDraft)
		r.Post("/api/topology/{nodeId}/preview", topologyH.Preview)
		r.Post("/api/topology/{nodeId}/deploy", topologyH.Deploy)
	})
	if d.Mail != nil {
		r.Group(func(r chi.Router) {
			r.Use(apimid.RequireAuth(d.AuthSvc, d.Users))
			r.Use(apimid.RequireOwner)
			r.Get("/api/settings/smtp", mailH.SMTP)
			r.Post("/api/settings/smtp/test", mailH.Test)
			r.Get("/api/alert-rules", mailH.Rules)
			r.Post("/api/alert-rules", mailH.SaveRule)
			r.Get("/api/alert-rules/{id}", mailH.Rule)
			r.Put("/api/alert-rules/{id}", mailH.SaveRule)
			r.Delete("/api/alert-rules/{id}", mailH.DeleteRule)
			r.Get("/api/notifications", mailH.Notifications)
			r.Post("/api/notifications/{id}/retry", mailH.Retry)
		})
	}

	return r
}
