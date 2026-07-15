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
	"sync"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

const httpOutputName = "http"

// HTTPOutput posts events as JSON to a configured HTTP endpoint, falling back
// to a local JSONL file on any failure (network error, timeout, non-2xx).
//
// P1.4: prerequisite for VM / OTEL receiver integration. The fallback is
// owned by the http output itself; the global `file` output is not used.
type HTTPOutput struct {
	cfg    configs.HTTPOutputConfig
	client *http.Client

	fbMu   sync.Mutex
	fbFile *os.File
	fbSize int64
	closed bool
}

// NewHTTPOutput constructs a fully-configured HTTPOutput. The caller is
// expected to call Init(map[string]any) after this (for parity with the
// Console/File outputs, which also expose Init), but no extra config is
// required at Init time.
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

func (h *HTTPOutput) Name() string { return httpOutputName }

// Init is a no-op for the http output: all config is applied at construction
// time. Kept to satisfy the Output interface.
func (h *HTTPOutput) Init(_ map[string]any) error { return nil }

// Publish serializes the event to JSON, attempts POST with retries, and on
// failure appends the same body to the fallback file. Returns an error only
// when both the POST and the fallback write fail.
func (h *HTTPOutput) Publish(ctx context.Context, event define.Event) error {
	h.fbMu.Lock()
	defer h.fbMu.Unlock()
	if h.closed {
		return errors.New("http output: closed")
	}
	payload := map[string]any{
		"timestamp": event.GetTimestamp(),
		"type":      event.GetType(),
		"data":      event.GetData(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("http output marshal: %w", err)
	}
	if h.tryPost(ctx, body) {
		return nil
	}
	return h.writeFallback(body)
}

func (h *HTTPOutput) tryPost(ctx context.Context, body []byte) bool {
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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.URL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
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
		slog.Warn("http output: post failed, will fall back", "err", lastErr, "url", h.cfg.URL)
	}
	return false
}

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

func (h *HTTPOutput) writeFallback(body []byte) error {
	if h.cfg.FallbackPath == "" {
		return errors.New("http output: post failed and no fallback_path configured")
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

// Close releases the fallback file handle. Publish after Close returns an
// error and does not panic.
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
