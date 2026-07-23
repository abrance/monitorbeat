// Package smtp implements alerts.EmailSender via SMTP.
package smtp

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/web/alerts"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string // supports ${ENV_VAR} syntax
	From     string
	To       []string
	Insecure bool // skip TLS cert verification (debug only)
}

// Sender implements alerts.EmailSender.
type Sender struct {
	cfg    Config
	webURL string
}

// New creates an SMTP sender. Password supports ${ENV_VAR} syntax.
func New(cfg Config) *Sender {
	pw := resolvePassword(cfg.Password)
	cfg.Password = pw
	return &Sender{cfg: cfg}
}

// SendAlert sends an alert notification email.
func (s *Sender) SendAlert(rule alerts.AlertRule, hostname string, labels map[string]string, value float64, state string) error {
	if s.cfg.Host == "" || len(s.cfg.To) == 0 {
		return nil // SMTP not configured
	}

	stateLabel := "FIRING"
	if state == "recovered" {
		stateLabel = "RECOVERED"
	}

	subject := fmt.Sprintf("[MONITOR] %s: %s @ %s", stateLabel, rule.Name, hostnameOrUnknown(hostname))

	body, err := s.renderBody(rule, hostname, labels, value, state)
	if err != nil {
		return fmt.Errorf("render email: %w", err)
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, strings.Join(s.cfg.To, ","), subject, body,
	)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	return smtp.SendMail(addr, auth, s.cfg.From, s.cfg.To, []byte(msg))
}

// ---------------------------------------------------------------------------
// email template
// ---------------------------------------------------------------------------

type labelKV struct {
	Key string
	Val string
}

type emailData struct {
	RuleName   string
	Hostname   string
	Expr       string
	Labels     []labelKV
	Value      float64
	StateLabel string
	Time       string
	WebURL     string
}

var tmpl = template.Must(template.New("email").Parse(emailHTML))

const emailHTML = `
<h2>MonitorBeat 告警通知</h2>
<table border="0" cellpadding="6" cellspacing="0" style="border-collapse:collapse;font-family:sans-serif;">
  <tr><td style="font-weight:bold;color:#555;">规则</td><td>{{.RuleName}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">主机</td><td>{{.Hostname}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">表达式</td><td style="font-family:monospace;">{{.Expr}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">实例</td><td>{{template "labels" .Labels}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">值</td><td>{{.Value}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">状态</td><td>{{.StateLabel}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">时间</td><td>{{.Time}}</td></tr>
</table>
{{if .WebURL}}<p style="font-family:sans-serif;font-size:13px;color:#888;">请到 <a href="{{.WebURL}}">{{.WebURL}}</a> 确认此告警。</p>{{end}}
{{define "labels"}}{{range .}}<div style="font-family:monospace;">{{.Key}}={{.Val}}</div>{{end}}{{end}}
`

func (s *Sender) renderBody(rule alerts.AlertRule, hostname string, labels map[string]string, value float64, state string) (string, error) {
	stateLabel := "告警中"
	if state == "recovered" {
		stateLabel = "已恢复"
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lvs := make([]labelKV, 0, len(keys))
	for _, k := range keys {
		lvs = append(lvs, labelKV{Key: k, Val: labels[k]})
	}

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, emailData{
		RuleName:   rule.Name,
		Hostname:   hostnameOrUnknown(hostname),
		Expr:       rule.Expr,
		Labels:     lvs,
		Value:      value,
		StateLabel: stateLabel,
		Time:       time.Now().Format("2006-01-02 15:04:05"),
		WebURL:     s.webURL,
	})
	return buf.String(), err
}

func hostnameOrUnknown(host string) string {
	if host == "" {
		return "(unknown)"
	}
	return host
}

func resolvePassword(pw string) string {
	if strings.HasPrefix(pw, "${") && strings.HasSuffix(pw, "}") {
		key := pw[2 : len(pw)-1]
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return pw
}
