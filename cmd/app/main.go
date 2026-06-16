// Command app 是 s2astats 的入口：装配只读 store、stats 服务与 HTTP 服务并优雅关停。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata" // 内嵌时区库，兼容精简容器

	"s2astats/internal/cache"
	"s2astats/internal/config"
	"s2astats/internal/httpapi"
	"s2astats/internal/stats"
	"s2astats/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.Sub2APIDSN, cfg.DBTimeout)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := stats.New(st, cache.New(), cfg.BillingLoc, cfg.CacheTTL)
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpapi.New(svc, cfg.PublicToken).Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
