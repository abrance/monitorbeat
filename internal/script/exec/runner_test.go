// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun_Echo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Run(ctx, "echo hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("got %q, want hello", out)
	}
}

func TestRun_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, "sleep 5", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRun_CommandNotFound(t *testing.T) {
	ctx := context.Background()
	_, err := Run(ctx, "nonexistent_command_xyz", nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}
