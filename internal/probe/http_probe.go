package probe

import (
	"io"
	"net/http"
	"time"

	"github.com/confuzeus/minitor/internal/models"
)

const (
	timestampFormat    = "2006-01-02 15:04:05"
	defaultHTTPTimeout = 30
	// maxProbeBodyBytes bounds how much of the response body is consumed so a
	// 2xx endpoint streaming a large or slow body isn't classified DOWN after
	// exhausting the request timeout mid-drain.
	maxProbeBodyBytes = 4096
)

func RunHTTPProbe(monitor models.Monitor) models.ProbeResult {
	result := models.ProbeResult{
		MonitorID: monitor.ID,
		Timestamp: time.Now().UTC().Format(timestampFormat),
	}

	timeout := monitor.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	if !monitor.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	start := time.Now()
	resp, err := client.Get(monitor.URL)
	if err != nil {
		result.Status = models.StatusDown
		message := err.Error()
		result.ErrorMsg = &message
		return result
	}
	defer resp.Body.Close()

	latencyMs := time.Since(start).Milliseconds()

	if _, err := io.CopyN(io.Discard, resp.Body, maxProbeBodyBytes); err != nil && err != io.EOF {
		result.Status = models.StatusDown
		message := err.Error()
		result.ErrorMsg = &message
		return result
	}

	statusCode := resp.StatusCode
	result.StatusCode = &statusCode
	result.LatencyMs = &latencyMs

	up := statusCode >= 200 && statusCode < 300
	if expected := monitor.ExpectedStatusCode; expected != nil {
		up = statusCode == *expected
	}
	if up {
		result.Status = models.StatusUp
	} else {
		result.Status = models.StatusDown
	}

	return result
}
