package probe

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/confuzeus/minitor/internal/models"
)

func TestNormalizePingHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "example.com", want: "example.com"},
		{in: "  example.com  ", want: "example.com"},
		{in: "https://example.com", want: "example.com"},
		{in: "https://example.com/path?x=1", want: "example.com"},
		{in: "http://[::1]:8443/x", want: "::1"},
		{in: "example.com:8080", want: "example.com"},
		{in: "10.0.0.1", want: "10.0.0.1"},
		{in: "10.0.0.1/health", want: "10.0.0.1"},
		{in: "::1", want: "::1"},
		{in: "[::1]", want: "::1"},
		{in: "[::1]:80", want: "::1"},
		{in: "", want: ""},
		{in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizePingHost(tt.in); got != tt.want {
				t.Errorf("normalizePingHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRunTCPFallbackSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	oldPort := tcpFallbackPort
	tcpFallbackPort = fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	defer func() { tcpFallbackPort = oldPort }()

	var result models.ProbeResult
	runTCPFallback(&result, "127.0.0.1", time.Second)

	if result.Status != models.StatusUp {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusUp)
	}
	if result.LatencyMs == nil || *result.LatencyMs < 0 {
		t.Errorf("LatencyMs = %v, want non-negative value", result.LatencyMs)
	}
	if result.ErrorMsg != nil {
		t.Errorf("ErrorMsg = %q, want nil", *result.ErrorMsg)
	}
	if result.StatusCode != nil {
		t.Errorf("StatusCode = %v, want nil for TCP fallback", *result.StatusCode)
	}
}

func TestRunTCPFallbackConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	ln.Close()

	oldPort := tcpFallbackPort
	tcpFallbackPort = port
	defer func() { tcpFallbackPort = oldPort }()

	var result models.ProbeResult
	runTCPFallback(&result, "127.0.0.1", time.Second)

	if result.Status != models.StatusDown {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusDown)
	}
	if result.ErrorMsg == nil || !strings.Contains(*result.ErrorMsg, "refused") {
		t.Errorf("ErrorMsg = %v, want connection refused error", result.ErrorMsg)
	}
	if result.LatencyMs != nil {
		t.Errorf("LatencyMs = %v, want nil on failure", result.LatencyMs)
	}
}

func TestRunPingProbeNoTarget(t *testing.T) {
	monitor := models.Monitor{ID: 42, URL: "   "}
	result := RunPingProbe(monitor)

	if result.Status != models.StatusDown {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusDown)
	}
	if result.ErrorMsg == nil || !strings.Contains(*result.ErrorMsg, "no ping target") {
		t.Errorf("ErrorMsg = %v, want no-ping-target error", result.ErrorMsg)
	}
	if result.LatencyMs != nil {
		t.Errorf("LatencyMs = %v, want nil", result.LatencyMs)
	}
	if result.MonitorID != 42 {
		t.Errorf("MonitorID = %d, want 42", result.MonitorID)
	}
	if _, err := time.Parse(timestampFormat, result.Timestamp); err != nil {
		t.Errorf("Timestamp = %q, want format %q: %v", result.Timestamp, timestampFormat, err)
	}
}
