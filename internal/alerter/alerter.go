package alerter

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/confuzeus/minitor/internal/models"
	"github.com/confuzeus/minitor/internal/settings"
)

// timestampFormat is the layout the probe code assigns to a result before it is
// persisted. Reads from the database return RFC 3339 instead (see
// parseTimestamp).
const timestampFormat = "2006-01-02 15:04:05"

// parseTimestamp parses a probe timestamp. Persisted results come back from the
// SQLite driver as RFC 3339, while the plain layout above is accepted for
// results that have not been round-tripped through the database.
func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(timestampFormat, s)
}

// downtimeInfo describes how long a monitor has been down at the time an alert
// is sent. Since is the timestamp of the first failed probe in the current
// outage; Duration is a human-readable rendering of the elapsed time.
type downtimeInfo struct {
	Since    string
	Duration string
	Valid    bool
}

// SendMailFunc delivers an email message to the given recipients over SMTP. It
// mirrors the signature of net/smtp.SendMail and is injectable so tests can
// capture outgoing alert emails without a real SMTP server.
type SendMailFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

type Alerter struct {
	smtp settings.SMTPConfig
	db   *sql.DB

	mu       sync.RWMutex
	sendMail SendMailFunc
}

func New(db *sql.DB, smtp settings.SMTPConfig) *Alerter {
	return &Alerter{db: db, smtp: smtp}
}

// SetSendMail replaces the SMTP delivery function. It is safe to call
// concurrently with sending, though it is intended for tests.
func (a *Alerter) SetSendMail(f SendMailFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sendMail = f
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
		downtime := a.computeDowntime(monitorID, result)
		go a.send(downRecipients, "DOWN: "+monitor.Name, buildEmailBody(*monitor, result, models.StatusDown, downtime))
	}
	if len(recoveryRecipients) > 0 {
		downtime := a.computeDowntime(monitorID, result)
		go a.send(recoveryRecipients, "RECOVERED: "+monitor.Name, buildEmailBody(*monitor, result, models.StatusUp, downtime))
	}
}

// computeDowntime resolves when the monitor's current outage began and how long
// it has lasted up to the triggering result. It returns an invalid downtimeInfo
// when the outage start cannot be determined, so callers omit the fields.
func (a *Alerter) computeDowntime(monitorID int64, result models.ProbeResult) downtimeInfo {
	start, err := models.GetCurrentOutageStart(a.db, monitorID, result.ID)
	if err != nil {
		slog.Error("alerter: failed to load outage start", "error", err, "monitor_id", monitorID)
		return downtimeInfo{}
	}
	if start == nil {
		return downtimeInfo{}
	}
	return formatDowntime(start.Timestamp, result.Timestamp)
}

// formatDowntime builds a downtimeInfo from the outage-start timestamp and the
// timestamp of the result that triggered the alert. It returns an invalid
// downtimeInfo when either timestamp cannot be parsed.
func formatDowntime(since, checkedAt string) downtimeInfo {
	startTime, err := parseTimestamp(since)
	if err != nil {
		return downtimeInfo{}
	}
	endTime, err := parseTimestamp(checkedAt)
	if err != nil {
		return downtimeInfo{}
	}

	duration := endTime.Sub(startTime)
	if duration < 0 {
		duration = 0
	}
	return downtimeInfo{
		Since:    since,
		Duration: duration.Round(time.Second).String(),
		Valid:    true,
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

func buildEmailBody(monitor models.Monitor, result models.ProbeResult, alertType string, downtime downtimeInfo) string {
	var sb strings.Builder
	if alertType == models.StatusDown {
		sb.WriteString("Minitor DOWN alert\n\n")
	} else {
		sb.WriteString("Minitor RECOVERY alert\n\n")
	}
	sb.WriteString("Monitor: " + monitor.Name + "\n")
	sb.WriteString("Target: " + monitor.URL + "\n")
	sb.WriteString("Status: " + strings.ToUpper(result.Status) + "\n")
	sb.WriteString("Checked at: " + result.Timestamp + "\n")
	if downtime.Valid {
		sb.WriteString("Down since: " + downtime.Since + "\n")
		sb.WriteString("Down for: " + downtime.Duration + "\n")
	}
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

	a.mu.RLock()
	sendMail := a.sendMail
	a.mu.RUnlock()
	if sendMail == nil {
		sendMail = smtp.SendMail
	}
	if err := sendMail(addr, auth, a.smtp.From, to, msg); err != nil {
		slog.Error("alerter: failed to send email", "error", err, "to", to, "subject", subject)
		return
	}
	slog.Info("alerter: email sent", "to", to, "subject", subject)
}
