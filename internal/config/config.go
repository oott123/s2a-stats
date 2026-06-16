// Package config 从环境变量读取 s2astats 的运行配置。
package config

import (
	"fmt"
	"os"
	"time"
)

// Config 是 s2astats 的全部运行配置。
type Config struct {
	ListenAddr  string         // S2A_LISTEN_ADDR
	Sub2APIDSN  string         // S2A_SUB2API_DSN（必填）
	PublicToken string         // S2A_PUBLIC_TOKEN（必填）
	BillingLoc  *time.Location // 由 S2A_BILLING_TIMEZONE 解析
	CacheTTL    time.Duration  // S2A_CACHE_TTL
	DBTimeout   time.Duration  // S2A_DB_TIMEOUT
}

// Load 从环境变量读取配置。必填项缺失或字段非法时返回错误，不做容错补全。
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:  envOr("S2A_LISTEN_ADDR", ":8080"),
		Sub2APIDSN:  os.Getenv("S2A_SUB2API_DSN"),
		PublicToken: os.Getenv("S2A_PUBLIC_TOKEN"),
	}

	if cfg.Sub2APIDSN == "" {
		return nil, fmt.Errorf("S2A_SUB2API_DSN is required")
	}
	if cfg.PublicToken == "" {
		return nil, fmt.Errorf("S2A_PUBLIC_TOKEN is required")
	}

	loc, err := time.LoadLocation(envOr("S2A_BILLING_TIMEZONE", "Asia/Singapore"))
	if err != nil {
		return nil, fmt.Errorf("invalid S2A_BILLING_TIMEZONE: %w", err)
	}
	cfg.BillingLoc = loc

	cfg.CacheTTL, err = time.ParseDuration(envOr("S2A_CACHE_TTL", "60s"))
	if err != nil {
		return nil, fmt.Errorf("invalid S2A_CACHE_TTL: %w", err)
	}
	cfg.DBTimeout, err = time.ParseDuration(envOr("S2A_DB_TIMEOUT", "15s"))
	if err != nil {
		return nil, fmt.Errorf("invalid S2A_DB_TIMEOUT: %w", err)
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
