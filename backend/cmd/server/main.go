// Coxpanel 面板控制面入口。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coxpanel/backend/internal/api"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/config"
	"github.com/coxpanel/backend/internal/db"
	"github.com/coxpanel/backend/internal/repo"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 数据库
	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}

	// 依赖装配
	authSvc := auth.NewService(cfg.JWTSecret, 24*time.Hour)
	deps := api.Deps{
		AuthSvc: authSvc,
		Users:   repo.NewUserRepo(pool),
		Nodes:   repo.NewNodeRepo(pool),
		Subs:    repo.NewSubscriptionRepo(pool),
		Groups:  repo.NewGroupRepo(pool),
	}

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: api.NewRouter(deps),
	}

	go func() {
		log.Printf("Coxpanel 启动于 %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("收到退出信号，优雅关闭...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("已退出")
}
