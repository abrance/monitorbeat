// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

// Package api 提供 monitorweb 的 HTTP 路由与 VM PromQL 查询代理。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/web/alerts"
	"github.com/abrance/monitorbeat/web/config"
	"github.com/abrance/monitorbeat/web/vm"
)

// Server 持有配置与 VM 客户端。
type Server struct {
	cfg   *config.WebConfig
	vm    *vm.Client
	store *alerts.Store
}

// NewServer 构造 monitorweb 的 HTTP handler。
//
// 路由：/api/v1/* 全部优先匹配，最后注册 "/" 托管前端静态资源。
// mux 由调用方提供（允许外部追加路由，如 registry）。
func NewServer(cfg *config.WebConfig, client *vm.Client, store *alerts.Store, mux *http.ServeMux) http.Handler {
	s := &Server{cfg: cfg, vm: client, store: store}
	ah := &alertHandler{store: store}

	mux.HandleFunc("/api/v1/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/hosts", s.handleHosts)
	mux.HandleFunc("/api/v1/host/", s.handleHost) // /host/:host/summary
	mux.HandleFunc("/api/v1/query/range", s.handleRange)
	mux.HandleFunc("/api/v1/metrics/names", s.handleMetricNames)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/probes", s.handleProbes)

	mux.HandleFunc("/api/v1/alerts/rules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ah.listRules(w, r)
		case http.MethodPost:
			ah.createRule(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/alerts/rules/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/rules/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			ah.updateRule(w, r, id)
		case http.MethodDelete:
			ah.deleteRule(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/alerts/acknowledge", ah.acknowledge)
	mux.HandleFunc("/api/v1/alerts/history", ah.listHistory)
	mux.HandleFunc("/api/v1/alerts/status", ah.status)

	mux.Handle("/", http.FileServer(http.Dir(cfg.UIDir)))
	return recovery(mux)
}

// ---------------- 响应辅助 ----------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) vmError(w http.ResponseWriter, err error) {
	slog.Error("vm query failed", "err", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}

func (s *Server) badRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler", "err", rec, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// labelMatch 生成安全的 PromQL label matcher：name="escapedVal"。
func labelMatch(name, val string) string {
	val = strings.ReplaceAll(val, `\`, `\\`)
	val = strings.ReplaceAll(val, `"`, `\"`)
	return fmt.Sprintf(`%s="%s"`, name, val)
}

func scalarOf(v []vm.Vector) (float64, bool) {
	if len(v) == 0 {
		return 0, false
	}
	return v[0].Value[1], true
}

// ---------------- handlers ----------------

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"status":          "ok",
		"victoriametrics": s.cfg.VictoriaMetrics.URL,
	})
}

type hostOut struct {
	Hostname string `json:"hostname"`
	LastSeen int64  `json:"last_seen"`
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 一次查询聚合所有主机的 hostname 与 last_seen。
	hvec, err := s.vm.Query(ctx, `max by (hostname) (cpu_usage)`)
	if err != nil {
		s.vmError(w, err)
		return
	}
	tsVec, err := s.vm.Query(ctx, `max by (hostname) (timestamp(cpu_usage))`)
	if err != nil {
		s.vmError(w, err)
		return
	}
	tsMap := make(map[string]float64, len(tsVec))
	for _, v := range tsVec {
		tsMap[v.Metric["hostname"]] = v.Value[1]
	}
	out := make([]hostOut, 0, len(hvec))
	for _, v := range hvec {
		h := v.Metric["hostname"]
		if h == "" {
			continue
		}
		out = append(out, hostOut{
			Hostname: h,
			LastSeen: int64(tsMap[h]),
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/host/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "summary" {
		http.NotFound(w, r)
		return
	}
	s.handleSummary(w, r, parts[0])
}

// Summary 是主机当前指标快照。
type Summary struct {
	Hostname            string   `json:"hostname"`
	TS                  int64    `json:"ts"`
	CPUUsage            *float64 `json:"cpu_usage,omitempty"`
	MemUsedPercent      *float64 `json:"mem_used_percent,omitempty"`
	DiskRootUsedPercent *float64 `json:"disk_root_used_percent,omitempty"`
	Load1               *float64 `json:"load1,omitempty"`
	NetBytesRecv        *float64 `json:"net_bytes_recv,omitempty"`
	NetBytesSent        *float64 `json:"net_bytes_sent,omitempty"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request, host string) {
	ctx := r.Context()
	m := labelMatch("hostname", host)
	get := func(metric string) *float64 {
		v, err := s.vm.Query(ctx, fmt.Sprintf("%s{%s}", metric, m))
		if err != nil || len(v) == 0 {
			return nil
		}
		f := v[0].Value[1]
		return &f
	}
	writeJSON(w, Summary{
		Hostname:            host,
		TS:                  time.Now().Unix(),
		CPUUsage:            get("cpu_usage"),
		MemUsedPercent:      get("mem_used_percent"),
		DiskRootUsedPercent: get("disk_root_used_percent"),
		Load1:               get("load1"),
		NetBytesRecv:        get("net_bytes_recv"),
		NetBytesSent:        get("net_bytes_sent"),
	})
}

// MetricSeries 是单指标区间序列。
type MetricSeries struct {
	Metric string     `json:"metric"`
	Unit   string     `json:"unit"`
	Points []vm.Point `json:"points"`
}

func unitOf(m string) string {
	if strings.Contains(m, "percent") {
		return "%"
	}
	if strings.HasSuffix(m, "_bytes") {
		return "B"
	}
	return ""
}

func (s *Server) handleRange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	host := q.Get("host")
	metrics := q["metric"]
	if host == "" || len(metrics) == 0 {
		s.badRequest(w, "host and metric required")
		return
	}
	from, to, step := q.Get("from"), q.Get("to"), q.Get("step")
	if from == "" || to == "" {
		s.badRequest(w, "from and to required")
		return
	}
	if step == "" {
		step = "60"
	}
	m := labelMatch("hostname", host)
	out := make([]MetricSeries, 0, len(metrics))
	for _, metric := range metrics {
		series, err := s.vm.QueryRange(ctx, fmt.Sprintf("%s{%s}", metric, m), from, to, step)
		if err != nil {
			s.vmError(w, err)
			return
		}
		pts := []vm.Point{}
		if len(series) > 0 {
			pts = series[0].Values
		}
		out = append(out, MetricSeries{Metric: metric, Unit: unitOf(metric), Points: pts})
	}
	writeJSON(w, out)
}

func (s *Server) handleMetricNames(w http.ResponseWriter, r *http.Request) {
	names, err := s.vm.LabelValues(r.Context(), "__name__")
	if err != nil {
		s.vmError(w, err)
		return
	}
	writeJSON(w, names)
}

// EventsResult 是异常/事件计数区间。
type EventsResult struct {
	Type   string     `json:"type"`
	Points []vm.Point `json:"points"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	host, typ := q.Get("host"), q.Get("type")
	from, to, step := q.Get("from"), q.Get("to"), q.Get("step")
	if host == "" || typ == "" {
		s.badRequest(w, "host and type required")
		return
	}
	if from == "" || to == "" {
		s.badRequest(w, "from and to required")
		return
	}
	if step == "" {
		step = "60"
	}
	m := labelMatch("hostname", host)
	expr := fmt.Sprintf("sum(count_over_time(%s{%s}[%s]))", typ, m, step)
	series, err := s.vm.QueryRange(ctx, expr, from, to, step)
	if err != nil {
		s.vmError(w, err)
		return
	}
	pts := []vm.Point{}
	if len(series) > 0 {
		pts = series[0].Values
	}
	writeJSON(w, EventsResult{Type: typ, Points: pts})
}

// ProbeSeries 是单类探测的 up 与延迟序列。
type ProbeSeries struct {
	Up       []vm.Point `json:"up"`
	Duration []vm.Point `json:"duration"`
}

// ProbeResult 是三类探测聚合。
type ProbeResult struct {
	Ping ProbeSeries `json:"ping"`
	TCP  ProbeSeries `json:"tcp"`
	HTTP ProbeSeries `json:"http"`
}

func probeSeries(ctx context.Context, c *vm.Client, host, kind, from, to, step string) ProbeSeries {
	m := labelMatch("hostname", host)
	upExpr := fmt.Sprintf(`avg(success{%s,probe_type="%s"})`, m, kind)
	durExpr := fmt.Sprintf(`avg(duration_ms{%s,probe_type="%s"})`, m, kind)
	ps := ProbeSeries{
		Up:       []vm.Point{},
		Duration: []vm.Point{},
	}
	if up, err := c.QueryRange(ctx, upExpr, from, to, step); err == nil && len(up) > 0 {
		ps.Up = up[0].Values
	}
	if dur, err := c.QueryRange(ctx, durExpr, from, to, step); err == nil && len(dur) > 0 {
		ps.Duration = dur[0].Values
	}
	return ps
}

func (s *Server) handleProbes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	host := q.Get("host")
	from, to, step := q.Get("from"), q.Get("to"), q.Get("step")
	if host == "" {
		s.badRequest(w, "host required")
		return
	}
	if from == "" || to == "" {
		s.badRequest(w, "from and to required")
		return
	}
	if step == "" {
		step = "60"
	}
	out := ProbeResult{
		Ping: probeSeries(ctx, s.vm, host, "ping", from, to, step),
		TCP:  probeSeries(ctx, s.vm, host, "tcp", from, to, step),
		HTTP: probeSeries(ctx, s.vm, host, "http", from, to, step),
	}
	writeJSON(w, out)
}
