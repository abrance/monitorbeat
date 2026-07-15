package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHarvester_ReadLine_FromBegin(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	writeLines(t, p, []string{"alpha", "beta"})

	h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := h.ReadLine(ctx)
	if err != nil || got != "alpha" {
		t.Fatalf("first line: got=%q err=%v", got, err)
	}
	got, err = h.ReadLine(ctx)
	if err != nil || got != "beta" {
		t.Fatalf("second line: got=%q err=%v", got, err)
	}
}

func TestHarvester_ReadLine_AppendAndCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.log")
	writeLines(t, p, []string{"first"})

	h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if got, err := h.ReadLine(ctx); err != nil || got != "first" {
		t.Fatalf("first: got=%q err=%v", got, err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString("second\n")
	}()

	got, err := h.ReadLine(ctx)
	if err != nil || got != "second" {
		t.Fatalf("after append: got=%q err=%v", got, err)
	}
}

func TestHarvester_ReadLine_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.log")
	writeLines(t, p, nil)

	h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := h.ReadLine(ctx); err == nil {
		t.Fatal("expected error on ctx cancel")
	}
}
