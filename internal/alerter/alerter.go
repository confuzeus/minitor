package alerter

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"

	"github.com/confuzeus/minitor/internal/models"
	"github.com/confuzeus/minitor/internal/settings"
)

type Alerter struct {
	smtp settings.SMTPConfig
	db   *sql.DB
}

func New(db *sql.DB, smtp settings.SMTPConfig) *Alerter {
	return &Alerter{db: db, smtp: smtp}
}

// Notify is called by the probe scheduler after a probe result is persisted.
// It inspects the recent result history for the monitor and emails recipients
// when the monitor transitions from up to down or down to up.
func (a *Alerter) Notify(monitorID int64, result models.ProbeResult) {
	monitor, err := models.GetMonitorByID(a.db, monitorID)
	if err != nil {
		slog.Error("alerter: failed to load monitor", "error", err, "monitor_id", monitorID)
		return
	}
	if monitor == nil {
		slog.Warn("alerter: monitor not found", "monitor_id", monitorID)
		return
	}

	configs, err := models.GetAlertConfigs(a.db, monitorID)
	if err != nil {
		slog.Error("alerter: failed to load alert configs", "error", err, "monitor_id", monitorID)
		return
	}
	if len(configs) == 0 {
		return
	}

	threshold := maxConsecutiveFailures(configs)
	history, err := models.GetLastNResults(a.db, monitorID, threshold+1)
	if err != nil {
		slog.Error("alerter: failed to load probe history", "error", err, "monitor_id", monitorID)
		return
	}

	var downRecipients, recoveryRecipients []models.AlertRecipient
	for _, c := range configs {
		if c.OnDown && detectDownTransition(history, c.ConsecutiveFailures) {
			downRecipients = append(downRecipients, c.Recipient)
		}
		if c.OnRecovery && detectUpTransition(history, c.ConsecutiveFailures) {
			recoveryRecipients = append(recoveryRecipients, c.Recipient)
		}
	}

	if len(downRecipients) > 0 {
		go a.send(downRecipients, "DOWN: "+monitor.Name, buildEmailBody(*monitor, result, models.StatusDown))
	}
	if len(recoveryRecipients) > 0 {
		go a.send(recoveryRecipients, "RECOVERED: "+monitor.Name, buildEmailBody(*monitor, result, models.StatusUp))
	}
}

func maxConsecutiveFailures(configs []models.AlertConfig) int {
	max := 1
	for _, c := range configs {
		if c.ConsecutiveFailures > max {
			max = c.ConsecutiveFailures
		}
	}
	return max
}

// detectDownTransition reports whether the monitor just transitioned from up
// to down: the newest consecutiveFailures results are all down and, if history
// extends further back, the result before that run is up.
func detectDownTransition(history []models.ProbeResult, consecutiveFailures int) bool {
	if consecutiveFailures < 1 {
		consecutiveFailures = 1
	}
	if len(history) < consecutiveFailures {
		return false
	}
	for i := 0; i < consecutiveFailures; i++ {
		if history[i].Status != models.StatusDown {
			return false
		}
	}
	if len(history) > consecutiveFailures {
		return history[consecutiveFailures].Status == models.StatusUp
	}
	return true
}

// detectUpTransition reports whether the monitor just transitioned from down
// to up: the newest result is up and the previous consecutiveFailures results
// are all down. Requiring the same consecutive-failure threshold as down
// alerts keeps recovery emails in sync with the down emails that preceded
// them, so a recovery is never reported without a matching down alert.
func detectUpTransition(history []models.ProbeResult, consecutiveFailures int) bool {
	if consecutiveFailures < 1 {
		consecutiveFailures = 1
	}
	if len(history) < consecutiveFailures+1 {
		return false
	}
	if history[0].Status != models.StatusUp {
		return false
	}
	for i := 1; i <= consecutiveFailures; i++ {
		if history[i].Status != models.StatusDown {
			return false
		}
	}
	return true
}

func buildEmailBody(monitor models.Monitor, result models.ProbeResult, alertType string) string {
	var sb strings.Builder
	if alertType == models.StatusDown {
		sb.WriteString("Minitor DOWN alert\n\n")
	} else {
		sb.WriteString("Minitor RECOVERY alert\n\n")
	}
	sb.WriteString("Monitor: " + monitor.Name + "\n")
	sb.WriteString("URL: " + monitor.URL + "\n")
	sb.WriteString("Status: " + strings.ToUpper(result.Status) + "\n")
	sb.WriteString("Checked at: " + result.Timestamp + "\n")
	if result.StatusCode != nil {
		fmt.Fprintf(&sb, "HTTP status code: %d\n", *result.StatusCode)
	}
	if result.LatencyMs != nil {
		fmt.Fprintf(&sb, "Latency: %d ms\n", *result.LatencyMs)
	}
	if result.ErrorMsg != nil {
		sb.WriteString("Error: " + *result.ErrorMsg + "\n")
	}
	return sb.String()
}

func buildEmailMessage(from string, to []string, subject, body string) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + sanitizeHeader(from) + "\r\n")
	sb.WriteString("To: " + sanitizeHeader(strings.Join(to, ", ")) + "\r\n")
	sb.WriteString("Subject: " + sanitizeHeader(subject) + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return []byte(sb.String())
}

// sanitizeHeader strips CR and LF from header values so operator-controlled
// input such as a monitor name cannot inject extra headers into the message.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", "")
}

// send delivers one plain-text email to all recipients using a single SMTP
// connection. Without SMTP credentials, an unauthenticated connection is used.
func (a *Alerter) send(recipients []models.AlertRecipient, subject, body string) {
	if a.smtp.Host == "" {
		slog.Warn("alerter: SMTP not configured, skipping alert email", "subject", subject)
		return
	}

	port := a.smtp.Port
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(a.smtp.Host, port)

	to := make([]string, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, r.Email)
	}

	var auth smtp.Auth
	if a.smtp.Username != "" {
		auth = smtp.PlainAuth("", a.smtp.Username, a.smtp.Password, a.smtp.Host)
	}

	msg := buildEmailMessage(a.smtp.From, to, subject, body)
	if err := smtp.SendMail(addr, auth, a.smtp.From, to, msg); err != nil {
		slog.Error("alerter: failed to send email", "error", err, "to", to, "subject", subject)
		return
	}
	slog.Info("alerter: email sent", "to", to, "subject", subject)
}
