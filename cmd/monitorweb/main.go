// Command monitorweb 是 monitorbeat 的 Web 服务：VictoriaMetrics PromQL 查询代理 + 静态前端托管 + Agent Registry。
package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/abrance/monitorbeat/web/alerts"
	"github.com/abrance/monitorbeat/web/api"
	"github.com/abrance/monitorbeat/web/config"
	"github.com/abrance/monitorbeat/web/registry"
	"github.com/abrance/monitorbeat/web/smtp"
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

	// 统一 SQLite：alert + registry 共享同一连接
	db, err := openSQLite(cfg.DBPath)
	if err != nil {
		slog.Error("open database", "err", err, "path", cfg.DBPath)
		os.Exit(1)
	}
	defer db.Close()

	// Init alert store
	alertStore, err := alerts.NewStore(db)
	if err != nil {
		slog.Error("init alert store", "err", err)
		os.Exit(1)
	}

	// Init registry store
	regStore, err := registry.NewStore(db, cfg.Registry.TTL)
	if err != nil {
		slog.Error("init registry store", "err", err)
		os.Exit(1)
	}
	go regStore.CleanLoop(context.Background(), cfg.Registry.CleanInterval)

	// Init SMTP sender (nil if not configured)
	var emailSender alerts.EmailSender
	if cfg.SMTP.Host != "" {
		emailSender = smtp.New(smtp.Config{
			Host:     cfg.SMTP.Host,
			Port:     cfg.SMTP.Port,
			Username: cfg.SMTP.Username,
			Password: cfg.SMTP.Password,
			From:     cfg.SMTP.From,
			To:       cfg.SMTP.To,
			Insecure: cfg.SMTP.Insecure,
		})
	}

	// Build VM client and adapter
	vmc := vm.New(cfg.VictoriaMetrics.URL, cfg.VictoriaMetrics.Timeout)
	vmQuerier := &vmAdapter{client: vmc}

	// Create shared mux so both api and registry can register routes
	mux := http.NewServeMux()
	handler := api.NewServer(cfg, vmc, alertStore, mux)

	// Register registry routes on the same mux
	regHandler := registry.NewHandler(regStore)
	regHandler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start alert evaluator
	evaluator := alerts.NewEvaluator(alertStore, vmQuerier, emailSender, cfg.Alert.EvalInterval)
	evaluator.Start()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		slog.Info("monitorweb listening",
			"addr", cfg.Listen,
			"victoriametrics", cfg.VictoriaMetrics.URL,
			"db", cfg.DBPath,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("monitorweb exited", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("monitorweb shutting down")

	evaluator.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("monitorweb shutdown error", "err", err)
	}
}

// openSQLite 打开或创建 SQLite 数据库，设置 WAL 模式和 busy_timeout。
func openSQLite(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, err
	}
	return db, nil
}

// vmAdapter bridges vm.Client to alerts.VMQuerier.
type vmAdapter struct {
	client *vm.Client
}

func (a *vmAdapter) Query(ctx context.Context, expr string) ([]alerts.VectorResult, error) {
	vec, err := a.client.Query(ctx, expr)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.VectorResult, len(vec))
	for i, v := range vec {
		out[i] = alerts.VectorResult{Metric: v.Metric, Value: v.Value}
	}
	return out, nil
}
