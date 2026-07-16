package smtp

import (
	"strings"
	"testing"

	"github.com/abrance/monitorbeat/web/alerts"
)

func TestRenderBody(t *testing.T) {
	s := New(Config{From: "a@b.com", To: []string{"c@d.com"}})
	rule := alerts.AlertRule{
		Name:      "CPU High",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 90,
	}
	body, err := s.renderBody(rule, "web-01", 95.5, "firing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "CPU High") {
		t.Error("email body missing rule name")
	}
	if !strings.Contains(body, "95.5") {
		t.Error("email body missing value")
	}
	if !strings.Contains(body, "web-01") {
		t.Error("email body missing hostname")
	}
	if !strings.Contains(body, "告警中") {
		t.Error("email body missing state label")
	}
}

func TestRenderBodyRecovered(t *testing.T) {
	s := New(Config{From: "a@b.com", To: []string{"c@d.com"}})
	rule := alerts.AlertRule{
		Name:      "Memory Low",
		Metric:    "mem_available_percent",
		Condition: "lt",
		Threshold: 10,
	}
	body, err := s.renderBody(rule, "db-01", 15.2, "recovered")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "已恢复") {
		t.Error("recovery email should show recovered label")
	}
	if !strings.Contains(body, "Memory Low") {
		t.Error("email body missing rule name")
	}
}

func TestResolvePassword(t *testing.T) {
	// Plain text
	if got := resolvePassword("secret123"); got != "secret123" {
		t.Errorf("expected secret123, got %s", got)
	}
	// Env var not set
	if got := resolvePassword("${NONEXISTENT_VAR}"); got != "${NONEXISTENT_VAR}" {
		t.Errorf("expected original string, got %s", got)
	}
	// Env var set
	t.Setenv("SMTP_PASSWORD", "env-secret")
	if got := resolvePassword("${SMTP_PASSWORD}"); got != "env-secret" {
		t.Errorf("expected env-secret, got %s", got)
	}
}

func TestSendAlertNoopWhenNotConfigured(t *testing.T) {
	s := New(Config{}) // empty host, no To
	rule := alerts.AlertRule{Name: "test"}
	err := s.SendAlert(rule, "h", 1, "firing")
	if err != nil {
		t.Errorf("expected no error when SMTP not configured, got %v", err)
	}
}
