// Package smtp implements alerts.EmailSender via SMTP.
package smtp

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
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
func (s *Sender) SendAlert(rule alerts.AlertRule, hostname string, value float64, state string) error {
	if s.cfg.Host == "" || len(s.cfg.To) == 0 {
		return nil // SMTP not configured
	}

	subject := fmt.Sprintf("[MONITOR] %s: %s @ %s - %.1f",
		map[string]string{"firing": "FIRING", "recovered": "RECOVERED"}[state],
		rule.Name, hostname, value,
	)

	body, err := s.renderBody(rule, hostname, value, state)
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

type emailData struct {
	RuleName   string
	Hostname   string
	Metric     string
	Value      float64
	Condition  string
	Threshold  float64
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
  <tr><td style="font-weight:bold;color:#555;">指标</td><td>{{.Metric}} = {{.Value}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">条件</td><td>{{.Condition}} {{.Threshold}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">状态</td><td>{{.StateLabel}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">时间</td><td>{{.Time}}</td></tr>
</table>
{{if .WebURL}}<p style="font-family:sans-serif;font-size:13px;color:#888;">请到 <a href="{{.WebURL}}">{{.WebURL}}</a> 确认此告警。</p>{{end}}
`

func (s *Sender) renderBody(rule alerts.AlertRule, hostname string, value float64, state string) (string, error) {
	stateLabel := "告警中"
	if state == "recovered" {
		stateLabel = "已恢复"
	}
	condLabel := ">"
	if rule.Condition == "lt" {
		condLabel = "<"
	}
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, emailData{
		RuleName:   rule.Name,
		Hostname:   hostname,
		Metric:     rule.Metric,
		Value:      value,
		Condition:  condLabel,
		Threshold:  rule.Threshold,
		StateLabel: stateLabel,
		Time:       time.Now().Format("2006-01-02 15:04:05"),
		WebURL:     s.webURL,
	})
	return buf.String(), err
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
