// Package config 从环境变量加载面板配置。
package config

import (
	"fmt"
	"os"

	"github.com/coxpanel/backend/internal/auth"
)

// Config 面板运行配置。
type Config struct {
	Addr      string // HTTP 监听地址
	DBURL     string // Postgres DSN
	JWTSecret string
	// 密钥加密主密钥（AES-GCM，32 字节 hex）
	EncryptKey string
	// SMTP
	SMTPHost                 string
	SMTPPort                 int
	SMTPUser                 string
	SMTPPassword             string
	SMTPFrom                 string
	RequireEmailVerification bool
	// 订阅公开地址前缀
	SubBaseURL  string
	StatsListen string
}

// Load 从环境变量加载，缺关键项报错。
func Load() (*Config, error) {
	c := &Config{
		Addr:                     getenv("COXPANEL_ADDR", ":8080"),
		DBURL:                    os.Getenv("COXPANEL_DB_URL"),
		JWTSecret:                os.Getenv("COXPANEL_JWT_SECRET"),
		EncryptKey:               os.Getenv("COXPANEL_ENCRYPT_KEY"),
		SMTPHost:                 os.Getenv("COXPANEL_SMTP_HOST"),
		SMTPPort:                 getenvInt("COXPANEL_SMTP_PORT", 587),
		SMTPUser:                 os.Getenv("COXPANEL_SMTP_USER"),
		SMTPPassword:             os.Getenv("COXPANEL_SMTP_PASSWORD"),
		SMTPFrom:                 os.Getenv("COXPANEL_SMTP_FROM"),
		RequireEmailVerification: os.Getenv("COXPANEL_REQUIRE_EMAIL_VERIFICATION") == "true",
		SubBaseURL:               getenv("COXPANEL_SUB_BASE_URL", "http://localhost:8080"),
		StatsListen:              os.Getenv("COXPANEL_STATS_LISTEN"),
	}
	if c.DBURL == "" {
		return nil, fmt.Errorf("COXPANEL_DB_URL 未设置")
	}
	if c.JWTSecret == "" {
		return nil, fmt.Errorf("COXPANEL_JWT_SECRET 未设置")
	}
	if err := auth.ValidateSigningSecret(c.JWTSecret); err != nil {
		return nil, fmt.Errorf("COXPANEL_JWT_SECRET 配置无效")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
		return n
	}
	return def
}
