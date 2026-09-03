// Package api 组装 HTTP 路由。
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/coxpanel/backend/internal/api/admin"
	"github.com/coxpanel/backend/internal/api/agent"
	apimid "github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/api/sub"
	"github.com/coxpanel/backend/internal/api/user"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/repo"
)

// Deps 依赖集合。
type Deps struct {
	AuthSvc *auth.Service
	Users   *repo.UserRepo
	Nodes   *repo.NodeRepo
	Subs    *repo.SubscriptionRepo
	Groups  *repo.GroupRepo
}

// NewRouter 构建路由。
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	authH := &admin.Handler{Auth: d.AuthSvc, Users: d.Users}
	nodeH := &admin.NodeHandler{Nodes: d.Nodes}
	subH := &sub.Handler{Subs: d.Subs, Nodes: d.Nodes, Groups: d.Groups}
	userH := &user.Handler{Subs: d.Subs, Nodes: d.Nodes}

	// 公开端点
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", authH.Login)
		r.Post("/register", authH.Register)
	})
	r.Get("/sub/{token}", subH.Serve) // 公开订阅

	// agent 端点（X-Node-Id + API key 认证，P1 简化）
	agentH := &agent.Handler{Nodes: d.Nodes}
	r.Get("/api/agent/config", agentH.GetConfig)
	r.Post("/api/agent/heartbeat", agentH.Heartbeat)
	r.Post("/api/agent/report-traffic", agentH.ReportTraffic)

	// 登录用户
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc))
		r.Get("/api/me", authH.Me)
		r.Get("/api/my/subscriptions", userH.ListMySubs)
		r.Post("/api/my/subscriptions", userH.CreateSub)
		r.Delete("/api/my/subscriptions/{id}", userH.DeleteSub)
		r.Put("/api/my/subscriptions/{id}/overrides/{nodeId}", userH.SaveOverride)
	})

	// 管理员端点
	r.Group(func(r chi.Router) {
		r.Use(apimid.RequireAuth(d.AuthSvc))
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
	})

	return r
}

// DefaultDeps 便捷构造（main 里用真实值覆盖）。
func DefaultDeps() Deps {
	return Deps{
		AuthSvc: auth.NewService("dev-secret", 24*time.Hour),
	}
}
