package probe

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/confuzeus/minitor/internal/models"
	"github.com/prometheus-community/pro-bing"
)

const (
	// pingCount is the number of ICMP echo requests sent per probe. Sending a
	// few requests smooths over transient packet loss while staying short
	// relative to a typical monitor interval.
	pingCount = 3
	// pingInterval is the delay between ICMP echo requests.
	pingInterval = 500 * time.Millisecond
	// maxICMPTimeout caps how long a single ICMP attempt waits before giving
	// up. Any live host answers an echo request in well under a second, so
	// waiting for the full monitor timeout (30s by default) only makes a probe
	// that lost a reply — or a silent host — occupy its scheduler tick for far
	// longer than is useful.
	maxICMPTimeout = 5 * time.Second
)

// tcpFallbackPort is the port a ping probe dials when ICMP sockets are
// unavailable in the current environment. It is a variable so tests can point
// it at an ephemeral listener; production always uses port 80.
var tcpFallbackPort = "80"

// tcpFallbackWarned gates the one-time warning explaining that probes are
// running over TCP because ICMP sockets could not be opened.
var tcpFallbackWarned sync.Once

// RunPingProbe checks whether the monitor's host answers ICMP echo requests,
// degrading to a TCP connection on port 80 when ICMP sockets are unavailable
// (common in containers without raw socket access). The result mirrors
// RunHTTPProbe: latency is recorded in milliseconds on success and failures
// carry an error message.
func RunPingProbe(monitor models.Monitor) models.ProbeResult {
	result := models.ProbeResult{
		MonitorID: monitor.ID,
		Timestamp: time.Now().UTC().Format(timestampFormat),
	}

	timeout := monitor.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	timeoutDur := time.Duration(timeout) * time.Second

	host := normalizePingHost(monitor.URL)
	if host == "" {
		msg := "no ping target configured"
		result.Status = models.StatusDown
		result.ErrorMsg = &msg
		return result
	}

	// Prefer a privileged raw ICMP socket, then an unprivileged one. Both can
	// be unavailable (for example a container without CAP_NET_RAW and without
	// the ping group range granted), so end with a TCP fallback.
	if runICMPPing(&result, host, timeoutDur, true) {
		return result
	}
	slog.Debug("ping: privileged ICMP unavailable, retrying unprivileged", "host", host)
	if runICMPPing(&result, host, timeoutDur, false) {
		return result
	}
	runTCPFallback(&result, host, timeoutDur)
	return result
}

// normalizePingHost extracts a bare hostname or IP from the monitor's url
// field. Ping monitors store a host without a scheme, but the field may also
// hold a pasted URL, a host:port pair, or a trailing path; all are stripped.
func normalizePingHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return raw
		}
		return u.Hostname()
	}
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	// Strip a host:port suffix, but leave a bare IPv6 literal untouched.
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return raw[1 : len(raw)-1]
	}
	return raw
}

// runICMPPing records the outcome of a single ICMP attempt into result and
// returns true when the attempt produced a definitive answer. It returns false
// when the attempt could not be performed at all (the socket could not be
// opened or the host could not be resolved), signalling the caller to try the
// next mechanism.
//
// Zero replies from a socket that opened are treated as a genuine down rather
// than a reason to try the next mechanism: opening an ICMP socket already
// requires raw-socket privileges or membership in the kernel's ping group
// range, so an attempt that is running was permitted to send, and silence
// means the host (or a firewall in front of it) did not answer.
func runICMPPing(result *models.ProbeResult, host string, timeout time.Duration, privileged bool) bool {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return false
	}
	pinger.SetPrivileged(privileged)
	pinger.Count = pingCount
	pinger.Interval = pingInterval
	if timeout > maxICMPTimeout {
		timeout = maxICMPTimeout
	}
	pinger.Timeout = timeout

	if err := pinger.Run(); err != nil {
		pinger.Stop()
		return false
	}

	stats := pinger.Statistics()
	if stats.PacketsRecv == 0 {
		result.Status = models.StatusDown
		result.LatencyMs = nil
		msg := fmt.Sprintf("no reply to %d ICMP echo requests", pingCount)
		result.ErrorMsg = &msg
		return true
	}
	result.Status = models.StatusUp
	result.ErrorMsg = nil
	latency := stats.AvgRtt.Milliseconds()
	result.LatencyMs = &latency
	return true
}

// runTCPFallback records the outcome of a TCP connection check on
// tcpFallbackPort into result. It is used only when ICMP sockets are
// unavailable in the current environment.
func runTCPFallback(result *models.ProbeResult, host string, timeout time.Duration) {
	tcpFallbackWarned.Do(func() {
		slog.Warn("ping: ICMP sockets unavailable in this environment; falling back to TCP checks on port "+tcpFallbackPort+". Grant CAP_NET_RAW or run as root to enable real ICMP pings", "host", host)
	})

	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, tcpFallbackPort), timeout)
	if err != nil {
		result.Status = models.StatusDown
		result.LatencyMs = nil
		msg := err.Error()
		result.ErrorMsg = &msg
		return
	}
	conn.Close()

	result.Status = models.StatusUp
	result.ErrorMsg = nil
	latency := time.Since(start).Milliseconds()
	result.LatencyMs = &latency
}
