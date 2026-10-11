package alerter

import (
	"strings"
	"testing"

	"github.com/confuzeus/minitor/internal/models"
)

func results(statuses ...string) []models.ProbeResult {
	rs := make([]models.ProbeResult, 0, len(statuses))
	for _, s := range statuses {
		rs = append(rs, models.ProbeResult{Status: s})
	}
	return rs
}

func TestDetectDownTransition(t *testing.T) {
	tests := []struct {
		name                string
		history             []models.ProbeResult
		consecutiveFailures int
		want                bool
	}{
		{name: "empty history", history: results(), consecutiveFailures: 1, want: false},
		{name: "single down threshold 1", history: results(models.StatusDown), consecutiveFailures: 1, want: true},
		{name: "single down threshold 2 not enough history", history: results(models.StatusDown), consecutiveFailures: 2, want: false},
		{name: "two down threshold 2", history: results(models.StatusDown, models.StatusDown), consecutiveFailures: 2, want: true},
		{name: "down down up threshold 2", history: results(models.StatusDown, models.StatusDown, models.StatusUp), consecutiveFailures: 2, want: true},
		{name: "down down up threshold 3 not enough consecutive", history: results(models.StatusDown, models.StatusDown, models.StatusUp), consecutiveFailures: 3, want: false},
		{name: "down down down up threshold 3", history: results(models.StatusDown, models.StatusDown, models.StatusDown, models.StatusUp), consecutiveFailures: 3, want: true},
		{name: "down up down down not consecutive at tail", history: results(models.StatusDown, models.StatusUp, models.StatusDown, models.StatusDown), consecutiveFailures: 2, want: false},
		{name: "single up threshold 1", history: results(models.StatusUp), consecutiveFailures: 1, want: false},
		{name: "down up threshold 1", history: results(models.StatusDown, models.StatusUp), consecutiveFailures: 1, want: true},
		{name: "already down does not re-alert", history: results(models.StatusDown, models.StatusDown, models.StatusDown, models.StatusDown), consecutiveFailures: 3, want: false},
		{name: "zero threshold treated as one", history: results(models.StatusDown), consecutiveFailures: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDownTransition(tt.history, tt.consecutiveFailures); got != tt.want {
				t.Errorf("detectDownTransition(%v, %d) = %v, want %v", tt.history, tt.consecutiveFailures, got, tt.want)
			}
		})
	}
}

func TestDetectUpTransition(t *testing.T) {
	tests := []struct {
		name                string
		history             []models.ProbeResult
		consecutiveFailures int
		want                bool
	}{
		{name: "empty history", history: results(), consecutiveFailures: 1, want: false},
		{name: "single up not enough history", history: results(models.StatusUp), consecutiveFailures: 1, want: false},
		{name: "up down threshold 1", history: results(models.StatusUp, models.StatusDown), consecutiveFailures: 1, want: true},
		{name: "up down down threshold 1", history: results(models.StatusUp, models.StatusDown, models.StatusDown), consecutiveFailures: 1, want: true},
		{name: "up down threshold 2 not enough previous failures", history: results(models.StatusUp, models.StatusDown), consecutiveFailures: 2, want: false},
		{name: "up down down threshold 2", history: results(models.StatusUp, models.StatusDown, models.StatusDown), consecutiveFailures: 2, want: true},
		{name: "up down down down threshold 3", history: results(models.StatusUp, models.StatusDown, models.StatusDown, models.StatusDown), consecutiveFailures: 3, want: true},
		{name: "up down down threshold 3 not enough previous failures", history: results(models.StatusUp, models.StatusDown, models.StatusDown), consecutiveFailures: 3, want: false},
		{name: "up down up down not consecutive before recovery", history: results(models.StatusUp, models.StatusDown, models.StatusUp, models.StatusDown), consecutiveFailures: 2, want: false},
		{name: "up up no transition", history: results(models.StatusUp, models.StatusUp), consecutiveFailures: 1, want: false},
		{name: "down up no transition", history: results(models.StatusDown, models.StatusUp), consecutiveFailures: 1, want: false},
		{name: "down down up no transition", history: results(models.StatusDown, models.StatusDown, models.StatusUp), consecutiveFailures: 1, want: false},
		{name: "zero threshold treated as one", history: results(models.StatusUp, models.StatusDown), consecutiveFailures: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectUpTransition(tt.history, tt.consecutiveFailures); got != tt.want {
				t.Errorf("detectUpTransition(%v, %d) = %v, want %v", tt.history, tt.consecutiveFailures, got, tt.want)
			}
		})
	}
}

func TestBuildEmailBody(t *testing.T) {
	monitor := models.Monitor{
		Name: "Example",
		URL:  "https://example.com",
	}

	tests := []struct {
		name      string
		monitor   models.Monitor
		result    models.ProbeResult
		alertType string
		downtime  downtimeInfo
		wantSub   []string
	}{
		{
			name:      "down alert with error",
			monitor:   monitor,
			result:    models.ProbeResult{Status: models.StatusDown, Timestamp: "2026-08-08T12:00:00Z", ErrorMsg: strPtr("connection refused")},
			alertType: models.StatusDown,
			downtime:  downtimeInfo{Since: "2026-08-08T11:58:00Z", Duration: "2m0s", Valid: true},
			wantSub: []string{
				"Minitor DOWN alert",
				"Monitor: Example",
				"Target: https://example.com",
				"Status: DOWN",
				"Checked at: 2026-08-08T12:00:00Z",
				"Down since: 2026-08-08T11:58:00Z",
				"Down for: 2m0s",
				"Error: connection refused",
			},
		},
		{
			name:      "down alert with status code and latency",
			monitor:   monitor,
			result:    models.ProbeResult{Status: models.StatusDown, Timestamp: "2026-08-08T12:00:00Z", StatusCode: intPtr(500), LatencyMs: int64Ptr(250)},
			alertType: models.StatusDown,
			downtime:  downtimeInfo{Since: "2026-08-08T12:00:00Z", Duration: "0s", Valid: true},
			wantSub: []string{
				"Minitor DOWN alert",
				"HTTP status code: 500",
				"Latency: 250 ms",
				"Down since: 2026-08-08T12:00:00Z",
				"Down for: 0s",
			},
		},
		{
			name:      "down alert without optional fields",
			monitor:   monitor,
			result:    models.ProbeResult{Status: models.StatusDown, Timestamp: "2026-08-08T12:00:00Z"},
			alertType: models.StatusDown,
			wantSub: []string{
				"Minitor DOWN alert",
				"Status: DOWN",
			},
		},
		{
			name:      "recovery alert",
			monitor:   monitor,
			result:    models.ProbeResult{Status: models.StatusUp, Timestamp: "2026-08-08T13:00:00Z", StatusCode: intPtr(200), LatencyMs: int64Ptr(120)},
			alertType: models.StatusUp,
			downtime:  downtimeInfo{Since: "2026-08-08T12:30:00Z", Duration: "30m0s", Valid: true},
			wantSub: []string{
				"Minitor RECOVERY alert",
				"Status: UP",
				"HTTP status code: 200",
				"Latency: 120 ms",
				"Down since: 2026-08-08T12:30:00Z",
				"Down for: 30m0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := buildEmailBody(tt.monitor, tt.result, tt.alertType, tt.downtime)
			for _, want := range tt.wantSub {
				if !strings.Contains(body, want) {
					t.Errorf("email body missing %q:\n%s", want, body)
				}
			}
		})
	}
}

func TestBuildEmailBodyExcludesMissingOptionalFields(t *testing.T) {
	body := buildEmailBody(
		models.Monitor{Name: "Example", URL: "https://example.com"},
		models.ProbeResult{Status: models.StatusDown, Timestamp: "2026-08-08 12:00:00"},
		models.StatusDown,
		downtimeInfo{},
	)
	for _, unwanted := range []string{"HTTP status code:", "Latency:", "Error:", "Down since:", "Down for:"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("email body unexpectedly contains %q:\n%s", unwanted, body)
		}
	}
}

func TestFormatDowntime(t *testing.T) {
	tests := []struct {
		name      string
		since     string
		checkedAt string
		wantValid bool
		wantSince string
		wantDur   string
	}{
		{name: "rfc3339 minutes", since: "2026-08-08T12:00:00Z", checkedAt: "2026-08-08T12:12:30Z", wantValid: true, wantSince: "2026-08-08T12:00:00Z", wantDur: "12m30s"},
		{name: "minutes", since: "2026-08-08 12:00:00", checkedAt: "2026-08-08 12:12:30", wantValid: true, wantSince: "2026-08-08 12:00:00", wantDur: "12m30s"},
		{name: "hours and seconds", since: "2026-08-08 10:00:00", checkedAt: "2026-08-08 11:02:03", wantValid: true, wantSince: "2026-08-08 10:00:00", wantDur: "1h2m3s"},
		{name: "immediate down", since: "2026-08-08 12:00:00", checkedAt: "2026-08-08 12:00:00", wantValid: true, wantSince: "2026-08-08 12:00:00", wantDur: "0s"},
		{name: "negative clamps to zero", since: "2026-08-08 12:00:05", checkedAt: "2026-08-08 12:00:00", wantValid: true, wantSince: "2026-08-08 12:00:05", wantDur: "0s"},
		{name: "invalid start", since: "not-a-time", checkedAt: "2026-08-08 12:00:00", wantValid: false},
		{name: "invalid end", since: "2026-08-08 12:00:00", checkedAt: "nope", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDowntime(tt.since, tt.checkedAt)
			if got.Valid != tt.wantValid {
				t.Fatalf("formatDowntime(%q, %q).Valid = %v, want %v", tt.since, tt.checkedAt, got.Valid, tt.wantValid)
			}
			if !tt.wantValid {
				return
			}
			if got.Since != tt.wantSince {
				t.Errorf("Since = %q, want %q", got.Since, tt.wantSince)
			}
			if got.Duration != tt.wantDur {
				t.Errorf("Duration = %q, want %q", got.Duration, tt.wantDur)
			}
		})
	}
}

func TestBuildEmailMessage(t *testing.T) {
	to := []string{"a@example.com", "b@example.com"}
	msg := string(buildEmailMessage("from@example.com", to, "DOWN: Example", "body text"))

	for _, want := range []string{
		"From: from@example.com\r\n",
		"To: a@example.com, b@example.com\r\n",
		"Subject: DOWN: Example\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nbody text",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildEmailMessageSanitizesHeaders(t *testing.T) {
	msg := string(buildEmailMessage("from@example.com\r\nBcc: x@example.com", []string{"a@example.com"}, "DOWN: Foo\r\nBcc: y@example.com", "body text"))

	if strings.Contains(msg, "\r\nBcc:") {
		t.Errorf("header injection created a new header line:\n%s", msg)
	}
	if !strings.Contains(msg, "Subject: DOWN: Foo") {
		t.Errorf("sanitized subject missing:\n%s", msg)
	}
	if !strings.Contains(msg, "From: from@example.com") {
		t.Errorf("sanitized from missing:\n%s", msg)
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }
