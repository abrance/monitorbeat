// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package output

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

const httpOutputName = "http"

// HTTPOutput posts events to a configured HTTP endpoint with format support.
//
// Supported formats:
//   - json (default): {"timestamp":"...","type":"...","data":{...}}
//   - victoriametrics: /api/v1/import JSON line format
//   - doris: Stream Load JSON array format
//
// Fallback to local JSONL file on failure.
type HTTPOutput struct {
	cfg    configs.HTTPOutputConfig
	client *http.Client

	fbMu   sync.Mutex
	fbFile *os.File
	fbSize int64
	closed bool
}

func NewHTTPOutput(cfg configs.HTTPOutputConfig) *HTTPOutput {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}
	return &HTTPOutput{
		cfg: cfg,
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

func (h *HTTPOutput) Name() string                { return httpOutputName }
func (h *HTTPOutput) Init(_ map[string]any) error { return nil }

// Publish formats event according to cfg.Format, sends HTTP request, falls
// back to local file on failure.
func (h *HTTPOutput) Publish(ctx context.Context, event define.Event) error {
	h.fbMu.Lock()
	defer h.fbMu.Unlock()
	if h.closed {
		return errors.New("http output: closed")
	}

	body, contentType, err := h.formatBody(event)
	if err != nil {
		return fmt.Errorf("http output format: %w", err)
	}
	if h.trySend(ctx, body, contentType) {
		return nil
	}
	return h.writeFallback(body)
}

// formatBody converts event to HTTP body bytes per configured format.
func (h *HTTPOutput) formatBody(event define.Event) ([]byte, string, error) {
	switch h.cfg.Format {
	case "victoriametrics":
		return formatVictoriaMetrics(event)
	case "doris":
		return formatDoris(event)
	default: // "json"
		return formatJSON(event)
	}
}

// trySend does HTTP request with retry, using configured Method.
func (h *HTTPOutput) trySend(ctx context.Context, body []byte, contentType string) bool {
	var lastErr error
	for attempt := 0; attempt <= h.cfg.RetryMax; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100*(1<<(attempt-1))) * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return false
			}
		}
		method := h.cfg.Method
		if method == "" {
			method = http.MethodPost
		}
		req, err := http.NewRequestWithContext(ctx, method, h.cfg.URL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range h.cfg.Headers {
			req.Header.Set(k, v)
		}
		applyHTTPAuth(req, h.cfg.Auth)
		resp, err := h.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return true
		}
		lastErr = fmt.Errorf("http output: status %d", resp.StatusCode)
	}
	if lastErr != nil {
		slog.Warn("http output: send failed, will fall back", "err", lastErr, "url", h.cfg.URL)
	}
	return false
}

// ---------- format helpers ----------

func formatJSON(event define.Event) ([]byte, string, error) {
	payload := map[string]any{
		"timestamp": event.GetTimestamp(),
		"type":      event.GetType(),
		"data":      event.GetData(),
	}
	body, err := json.Marshal(payload)
	return body, "application/json; charset=utf-8", err
}

// formatVictoriaMetrics converts event to VM /api/v1/import JSON line format.
// Extracts metrics and dimensions from event data.
func formatVictoriaMetrics(event define.Event) ([]byte, string, error) {
	data, ok := event.GetData().(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("victoriametrics: event data is not map")
	}

	metrics, _ := data["metrics"].(map[string]float64)
	dims, _ := data["dimensions"].(map[string]string)

	// Build VM import objects — one per metric
	var lines []vmMetric
	timestamp := event.GetTimestamp().UnixMilli()

	if len(metrics) > 0 && len(dims) > 0 {
		// Per-metric: one line per metric name, same dimensions + timestamp
		keys := make([]string, 0, len(metrics))
		for k := range metrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := metrics[key]
			metric := copyMap(dims)
			metric["__name__"] = key
			lines = append(lines, vmMetric{
				Metric:     metric,
				Values:     []float64{value},
				Timestamps: []int64{timestamp},
			})
		}
	} else if len(metrics) > 0 {
		// No dimensions: all metrics in one object
		metric := copyMap(dims)
		if metric == nil {
			metric = map[string]string{}
		}
		for key, value := range metrics {
			metric["__name__"] = key
			lines = append(lines, vmMetric{
				Metric:     cloneStringMap(metric),
				Values:     []float64{value},
				Timestamps: []int64{timestamp},
			})
			// subsequent metrics need their own __name__
			metric["__name__"] = ""
		}
	}

	if len(lines) == 0 {
		// No metrics: emit event type as metric with value 1
		dimCopy := copyMap(dims)
		if dimCopy == nil {
			dimCopy = map[string]string{}
		}
		dimCopy["__name__"] = event.GetType()
		lines = append(lines, vmMetric{
			Metric:     dimCopy,
			Values:     []float64{1},
			Timestamps: []int64{timestamp},
		})
	}

	// VM import expects newline-delimited JSON. VM 的 /api/v1/import 对
	// application/json 期望 JSON 数组，对 application/streamed 才接受逐行 JSON。
	// 此处 body 为逐行 JSON，故必须用 streamed 类型，否则真实 VM 返回 400。
	var buf bytes.Buffer
	for _, line := range lines {
		b, err := json.Marshal(line)
		if err != nil {
			return nil, "", err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), "application/streamed; charset=utf-8", nil
}

type vmMetric struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// formatDoris converts event to Doris Stream Load JSON array format.
// Flattens event data into a record array.
func formatDoris(event define.Event) ([]byte, string, error) {
	data, ok := event.GetData().(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("doris: event data is not map")
	}

	record := make(map[string]any)
	record["__type"] = event.GetType()
	record["__timestamp"] = event.GetTimestamp().UnixMilli()

	metrics, _ := data["metrics"].(map[string]float64)
	dims, _ := data["dimensions"].(map[string]string)
	for k, v := range dims {
		record[k] = v
	}
	for k, v := range metrics {
		record[k] = v
	}

	// Also include nested fields as raw JSON strings
	if processes, ok := data["processes"]; ok {
		record["processes"] = processes
	}
	if connections, ok := data["connections"]; ok {
		record["connections"] = connections
	}
	if exceptions, ok := data["exceptions"]; ok {
		record["exceptions"] = exceptions
	}

	// Doris Stream Load expects JSON array
	body, err := json.Marshal([]map[string]any{record})
	return body, "application/json", err
}

func copyMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneStringMap(m map[string]string) map[string]string {
	c := make(map[string]string, len(m))
	for k, v := range m {
		if k != "" {
			c[k] = v
		}
	}
	return c
}

// ---------- HTTP helpers ----------

func applyHTTPAuth(req *http.Request, a configs.HTTPAuthConfig) {
	switch a.Type {
	case "bearer":
		if a.Token != "" {
			req.Header.Set("Authorization", "Bearer "+a.Token)
		}
	case "basic":
		if a.User != "" {
			req.SetBasicAuth(a.User, a.Passwd)
		}
	}
}

// ---------- fallback ----------

func (h *HTTPOutput) writeFallback(body []byte) error {
	if h.cfg.FallbackPath == "" {
		return errors.New("http output: send failed and no fallback_path configured")
	}
	if h.fbFile == nil {
		f, err := os.OpenFile(h.cfg.FallbackPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("http output open fallback %s: %w", h.cfg.FallbackPath, err)
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("http output stat fallback: %w", err)
		}
		h.fbFile = f
		h.fbSize = info.Size()
	}
	n, err := h.fbFile.Write(append(body, '\n'))
	if err != nil {
		return fmt.Errorf("http output write fallback: %w", err)
	}
	h.fbSize += int64(n)
	if h.cfg.FallbackMaxSize > 0 {
		maxBytes := int64(h.cfg.FallbackMaxSize) * 1024 * 1024
		if h.fbSize >= maxBytes {
			if err := h.rotateFallback(); err != nil {
				return fmt.Errorf("http output rotate fallback: %w", err)
			}
		}
	}
	return nil
}

func (h *HTTPOutput) rotateFallback() error {
	if h.fbFile != nil {
		_ = h.fbFile.Close()
		h.fbFile = nil
	}
	for i := h.cfg.FallbackMaxBackups; i > 0; i-- {
		src := fmt.Sprintf("%s.%d", h.cfg.FallbackPath, i)
		dst := fmt.Sprintf("%s.%d", h.cfg.FallbackPath, i+1)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(h.cfg.FallbackPath, h.cfg.FallbackPath+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	h.fbSize = 0
	return nil
}

func (h *HTTPOutput) Close() error {
	h.fbMu.Lock()
	defer h.fbMu.Unlock()
	h.closed = true
	if h.fbFile == nil {
		return nil
	}
	err := h.fbFile.Close()
	h.fbFile = nil
	return err
}
