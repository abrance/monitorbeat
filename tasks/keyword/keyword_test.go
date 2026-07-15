package keyword

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestGather_EndToEnd_EmitsRawLogEvents(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "demo.log")
	if err := os.WriteFile(logPath, []byte("ERROR payment_id=1 amount=1.5\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &configs.KeywordConfig{
		BaseTaskParam: configs.BaseTaskParam{TaskID: 7, Ident: "keyword:7", Enabled: true},
		File:          logPath,
		Pattern:       `ERROR payment_id=(\d+) amount=(\d+\.\d+)`,
		Encoding:      "utf-8",
		BufferSize:    1024,
	}
	fb := false
	cfg.FromBegin = &fb
	_ = cfg.Clean()

	g := New(cfg).(*Gather)

	ch := make(chan define.Event, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go g.Run(ctx, ch)

	time.Sleep(100 * time.Millisecond)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("ERROR payment_id=42 amount=99.5\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	select {
	case ev := <-ch:
		data := ev.GetData().(map[string]any)
		if ev.GetType() != RawLogEventType {
			t.Fatalf("event type = %q", ev.GetType())
		}
		fields := data["fields"].(map[string]string)
		if fields["1"] != "42" || fields["2"] != "99.5" {
			t.Fatalf("unexpected fields: %+v", fields)
		}
		dims := data["dimensions"].(map[string]string)
		if dims["file"] != logPath {
			t.Fatalf("dim file = %q", dims["file"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received within timeout")
	}
	cancel()
}

func TestGather_InvalidConfigRejected(t *testing.T) {
	cfg := &configs.KeywordConfig{}
	if _, err := builder(cfg); err == nil {
		t.Fatal("expected error for empty file/pattern")
	}
}

func TestExtractLine_Integration(t *testing.T) {
	re := regexp.MustCompile(`ERROR payment_id=(\d+)`)
	caps, ok := ExtractLine("ERROR payment_id=7", re)
	if !ok || caps["1"] != "7" {
		t.Fatalf("unexpected: ok=%v caps=%+v", ok, caps)
	}
}
