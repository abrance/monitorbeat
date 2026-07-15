// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package output

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func newHTTPTestEvent(payload string) define.Event {
	return define.NewEvent("test", map[string]any{"data": payload})
}

// validHTTPCfg constructs a config that has a writable fallback path and the
// given URL. Tests override URL/timeout/etc. by passing an `override` fn.
func validHTTPCfg(t *testing.T, override func(*configs.HTTPOutputConfig)) configs.HTTPOutputConfig {
	t.Helper()
	dir := t.TempDir()
	c := configs.HTTPOutputConfig{
		URL:             "http://127.0.0.1:1",
		Timeout:         500 * time.Millisecond,
		RetryMax:        1,
		FallbackPath:    filepath.Join(dir, "fb.jsonl"),
		FallbackMaxSize: 1,
	}
	if override != nil {
		override(&c)
	}
	if err := c.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	return c
}

func newHTTPOutputFromMap(t *testing.T, params map[string]any) *HTTPOutput {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var c configs.HTTPOutputConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := c.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	return NewHTTPOutput(c)
}

func TestHTTPOutput_Name(t *testing.T) {
	o := newHTTPOutputFromMap(t, map[string]any{"url": "http://127.0.0.1:1"})
	if o.Name() != "http" {
		t.Fatalf("Name = %q, want http", o.Name())
	}
}

//  1. Clean requires url (covered in configs test); here we re-assert from the
//     output package's perspective via the constructor.
func TestHTTPOutput_CleanRequiresURL(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{})
	var c configs.HTTPOutputConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := c.Clean(); err == nil {
		t.Fatal("expected error for empty url")
	}
}

// 2. Publish fails when fallback path is missing AND the network call fails.
func TestHTTPOutput_PublishNoFallbackNoNetwork(t *testing.T) {
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = "http://127.0.0.1:1" // unreachable, fails fast
		c.FallbackPath = ""          // no fallback
		c.Timeout = 200 * time.Millisecond
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("x")); err == nil {
		t.Fatal("expected error when post fails and no fallback configured")
	}
}

// 3. Publish 200 OK → fallback file is NOT created.
func TestHTTPOutput_PublishSuccessNoFallback(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("Content-Type = %q, want application/json...", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = srv.URL
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("ok")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
	if _, err := os.Stat(cfg.FallbackPath); !os.IsNotExist(err) {
		t.Fatalf("fallback file should not exist, stat err = %v", err)
	}
}

//  4. Publish with unreachable port + valid fallback → fallback gets 1 JSONL
//     line, Publish returns nil.
func TestHTTPOutput_PublishFallbackOnNetworkError(t *testing.T) {
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = "http://127.0.0.1:1"
		c.Timeout = 100 * time.Millisecond
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("n1")); err != nil {
		t.Fatalf("publish should swallow network error when fallback succeeds, got: %v", err)
	}
	data, err := os.ReadFile(cfg.FallbackPath)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("fallback lines = %d, want 1: %q", len(lines), string(data))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("fallback line not valid JSON: %v / %q", err, lines[0])
	}
	inner, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("fallback payload data field missing or not object: %v", parsed["data"])
	}
	if inner["data"] != "n1" {
		t.Fatalf("fallback payload data.data = %v, want n1", inner["data"])
	}
}

// 5. Publish 500 → fallback written, no error returned.
func TestHTTPOutput_PublishFallbackOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = srv.URL
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("s1")); err != nil {
		t.Fatalf("publish should swallow 500 when fallback succeeds, got: %v", err)
	}
	data, err := os.ReadFile(cfg.FallbackPath)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	if !strings.Contains(string(data), `"s1"`) {
		t.Fatalf("fallback missing s1: %q", string(data))
	}
}

// 6. Publish with context canceled mid-flight → fallback written.
func TestHTTPOutput_PublishFallbackOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = srv.URL
		c.Timeout = 5 * time.Second
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := o.Publish(ctx, newHTTPTestEvent("c1")); err != nil {
		t.Fatalf("publish should swallow ctx error when fallback succeeds, got: %v", err)
	}
	data, err := os.ReadFile(cfg.FallbackPath)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	if !strings.Contains(string(data), `"c1"`) {
		t.Fatalf("fallback missing c1: %q", string(data))
	}
}

// 7. Publish fail + fallback parent dir unwritable → error returned.
func TestHTTPOutput_PublishErrorWhenFallbackUnwritable(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = "http://127.0.0.1:1"
		c.Timeout = 100 * time.Millisecond
		c.RetryMax = 0
		c.FallbackPath = filepath.Join(ro, "fb.jsonl")
	})
	o := NewHTTPOutput(cfg)
	err := o.Publish(context.Background(), newHTTPTestEvent("u1"))
	if err == nil {
		t.Fatal("expected error when fallback path is unwritable")
	}
}

// 8. Publish success then failure → fallback only contains the failed event.
func TestHTTPOutput_SuccessThenFailure(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = srv.URL
		c.RetryMax = 0
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("ok1")); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	fail.Store(true)
	if err := o.Publish(context.Background(), newHTTPTestEvent("bad1")); err != nil {
		t.Fatalf("second publish should fall back, got: %v", err)
	}
	data, err := os.ReadFile(cfg.FallbackPath)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("fallback lines = %d, want 1: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "bad1") {
		t.Fatalf("fallback should contain bad1 only, got %q", lines[0])
	}
}

// 9. Close then Publish → error, no panic.
func TestHTTPOutput_PublishAfterClose(t *testing.T) {
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = "http://127.0.0.1:1"
	})
	o := NewHTTPOutput(cfg)
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Publish after Close panicked: %v", r)
		}
	}()
	if err := o.Publish(context.Background(), newHTTPTestEvent("x")); err == nil {
		t.Fatal("expected error publishing after close")
	}
}

// 10. Bearer auth header is set on outgoing request.
func TestHTTPOutput_BearerAuthHeader(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cfg := validHTTPCfg(t, func(c *configs.HTTPOutputConfig) {
		c.URL = srv.URL
		c.Auth = configs.HTTPAuthConfig{Type: "bearer", Token: "secret-token"}
	})
	o := NewHTTPOutput(cfg)
	if err := o.Publish(context.Background(), newHTTPTestEvent("a1")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if seenAuth != "Bearer secret-token" {
		t.Fatalf("Authorization = %q, want %q", seenAuth, "Bearer secret-token")
	}
}
