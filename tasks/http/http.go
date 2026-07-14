// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
	"github.com/abrance/monitorbeat/tasks/probe"
)

func init() {
	tasks.RegisterBuilder(define.ModuleHTTP, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.HTTPConfig)
		if !ok {
			return nil, fmt.Errorf("http: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

type Gather struct {
	tasks.BaseTask
	cfg    *configs.HTTPConfig
	client *stdhttp.Client
}

type traceTimes struct {
	dnsStart     time.Time
	dnsDone      time.Time
	connectStart time.Time
	connectDone  time.Time
	tlsStart     time.Time
	tlsDone      time.Time
	firstByte    time.Time
}

func New(cfg *configs.HTTPConfig) define.Task {
	return NewWithClient(cfg, &stdhttp.Client{})
}

func NewWithClient(cfg *configs.HTTPConfig, client *stdhttp.Client) define.Task {
	g := &Gather{cfg: cfg, client: client}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	result := g.probe(ctx)
	select {
	case e <- probe.BuildEvent("http", g.cfg.URL, g.GetTaskID(), result):
	case <-ctx.Done():
	}
}

func (g *Gather) probe(ctx context.Context) probe.Result {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, g.cfg.GetTimeout())
	defer cancel()

	times := &traceTimes{}
	trace := &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { times.dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { times.dnsDone = time.Now() },
		ConnectStart:         func(_, _ string) { times.connectStart = time.Now() },
		ConnectDone:          func(_, _ string, _ error) { times.connectDone = time.Now() },
		TLSHandshakeStart:    func() { times.tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { times.tlsDone = time.Now() },
		GotFirstResponseByte: func() { times.firstByte = time.Now() },
	}

	req, err := stdhttp.NewRequestWithContext(httptrace.WithClientTrace(probeCtx, trace), g.cfg.Method, g.cfg.URL, strings.NewReader(g.cfg.Body))
	if err != nil {
		return failure(start, err, nil)
	}
	for k, v := range g.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return failure(start, err, times.metrics(start, 0, 0))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	duration := time.Since(start)
	metrics := times.metrics(start, resp.StatusCode, len(body))
	metrics["total_ms"] = probe.DurationMillis(duration)
	if err != nil {
		return probe.Result{Duration: duration, Error: err.Error(), Metrics: metrics}
	}
	if resp.StatusCode != g.cfg.ExpectedCode {
		return probe.Result{Duration: duration, Error: fmt.Sprintf("unexpected status: got %d want %d", resp.StatusCode, g.cfg.ExpectedCode), Metrics: metrics}
	}
	return probe.Result{Success: true, Duration: duration, Metrics: metrics}
}

func failure(start time.Time, err error, metrics map[string]float64) probe.Result {
	duration := time.Since(start)
	if metrics == nil {
		metrics = map[string]float64{}
	}
	metrics["total_ms"] = probe.DurationMillis(duration)
	return probe.Result{Duration: duration, Error: err.Error(), Metrics: metrics}
}

func (t *traceTimes) metrics(start time.Time, statusCode int, contentLength int) map[string]float64 {
	metrics := map[string]float64{
		"status_code":    float64(statusCode),
		"content_length": float64(contentLength),
	}
	if !t.dnsStart.IsZero() && !t.dnsDone.IsZero() {
		metrics["dns_ms"] = probe.DurationMillis(t.dnsDone.Sub(t.dnsStart))
	}
	if !t.connectStart.IsZero() && !t.connectDone.IsZero() {
		metrics["connect_ms"] = probe.DurationMillis(t.connectDone.Sub(t.connectStart))
	}
	if !t.tlsStart.IsZero() && !t.tlsDone.IsZero() {
		metrics["tls_ms"] = probe.DurationMillis(t.tlsDone.Sub(t.tlsStart))
	}
	if !t.firstByte.IsZero() {
		metrics["ttfb_ms"] = probe.DurationMillis(t.firstByte.Sub(start))
	}
	return metrics
}
