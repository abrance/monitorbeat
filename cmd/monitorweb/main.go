// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

// Command monitorweb 是 monitorbeat 的 Web 服务：VictoriaMetrics PromQL 查询代理 + 静态前端托管。
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abrance/monitorbeat/web/api"
	"github.com/abrance/monitorbeat/web/config"
	"github.com/abrance/monitorbeat/web/vm"
)

func main() {
	configPath := "web/configs/web.yaml"
	flag.StringVar(&configPath, "config", configPath, "web config file path")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config failed", "err", err, "path", configPath)
		os.Exit(1)
	}

	vmc := vm.New(cfg.VictoriaMetrics.URL, cfg.VictoriaMetrics.Timeout)
	handler := api.NewServer(cfg, vmc)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		slog.Info("monitorweb listening", "addr", cfg.Listen, "victoriametrics", cfg.VictoriaMetrics.URL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("monitorweb exited", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("monitorweb shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("monitorweb shutdown error", "err", err)
	}
}
