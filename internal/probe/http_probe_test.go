package probe

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/confuzeus/minitor/internal/models"
)

func testMonitor(url string) models.Monitor {
	return models.Monitor{
		URL:             url,
		Timeout:         5,
		FollowRedirects: true,
	}
}

func TestRunHTTPProbeStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus string
	}{
		{name: "ok", statusCode: http.StatusOK, wantStatus: models.StatusUp},
		{name: "created", statusCode: http.StatusCreated, wantStatus: models.StatusUp},
		{name: "not found", statusCode: http.StatusNotFound, wantStatus: models.StatusDown},
		{name: "server error", statusCode: http.StatusInternalServerError, wantStatus: models.StatusDown},
		{name: "temporary redirect", statusCode: http.StatusTemporaryRedirect, wantStatus: models.StatusDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			result := RunHTTPProbe(testMonitor(srv.URL))

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if result.StatusCode == nil || *result.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %v, want %d", result.StatusCode, tt.statusCode)
			}
			if result.LatencyMs == nil || *result.LatencyMs < 0 {
				t.Errorf("LatencyMs = %v, want non-negative value", result.LatencyMs)
			}
			if result.ErrorMsg != nil {
				t.Errorf("ErrorMsg = %q, want nil", *result.ErrorMsg)
			}
			if result.MonitorID != 0 {
				t.Errorf("MonitorID = %d, want 0", result.MonitorID)
			}
		})
	}
}

func TestRunHTTPProbeExpectedStatus(t *testing.T) {
	tests := []struct {
		name             string
		statusCode       int
		expectedStatus   int
		wantStatus       string
	}{
		{name: "matches", statusCode: http.StatusOK, expectedStatus: http.StatusOK, wantStatus: models.StatusUp},
		{name: "mismatch down", statusCode: http.StatusOK, expectedStatus: http.StatusCreated, wantStatus: models.StatusDown},
		{name: "mismatch up overrides 2xx", statusCode: http.StatusOK, expectedStatus: http.StatusNotFound, wantStatus: models.StatusDown},
		{name: "matching non-2xx", statusCode: http.StatusNotFound, expectedStatus: http.StatusNotFound, wantStatus: models.StatusUp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			monitor := testMonitor(srv.URL)
			monitor.ExpectedStatusCode = &tt.expectedStatus

			result := RunHTTPProbe(monitor)

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if result.StatusCode == nil || *result.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %v, want %d", result.StatusCode, tt.statusCode)
			}
		})
	}
}

func TestRunHTTPProbeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	monitor := testMonitor(srv.URL)
	monitor.Timeout = 1

	start := time.Now()
	result := RunHTTPProbe(monitor)
	elapsed := time.Since(start)

	if result.Status != models.StatusDown {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusDown)
	}
	if result.ErrorMsg == nil || !strings.Contains(*result.ErrorMsg, "Client.Timeout") {
		t.Errorf("ErrorMsg = %v, want timeout error", result.ErrorMsg)
	}
	if result.StatusCode != nil {
		t.Errorf("StatusCode = %v, want nil on timeout", result.StatusCode)
	}
	if result.LatencyMs != nil {
		t.Errorf("LatencyMs = %v, want nil on timeout", result.LatencyMs)
	}
	if elapsed > 5*time.Second {
		t.Errorf("probe took %v, want < 5s", elapsed)
	}
}

func TestRunHTTPProbeConnectionRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	result := RunHTTPProbe(testMonitor("http://" + addr))

	if result.Status != models.StatusDown {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusDown)
	}
	if result.ErrorMsg == nil || !strings.Contains(*result.ErrorMsg, "refused") {
		t.Errorf("ErrorMsg = %v, want connection refused error", result.ErrorMsg)
	}
	if result.StatusCode != nil {
		t.Errorf("StatusCode = %v, want nil on connection error", result.StatusCode)
	}
}

func TestRunHTTPProbeFollowRedirects(t *testing.T) {
	tests := []struct {
		name            string
		followRedirects bool
		wantStatus      string
		wantStatusCode  int
	}{
		{name: "follow", followRedirects: true, wantStatus: models.StatusUp, wantStatusCode: http.StatusOK},
		{name: "don't follow", followRedirects: false, wantStatus: models.StatusDown, wantStatusCode: http.StatusFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()

			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusFound)
			}))
			defer redirect.Close()

			monitor := testMonitor(redirect.URL)
			monitor.FollowRedirects = tt.followRedirects

			result := RunHTTPProbe(monitor)

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if result.StatusCode == nil || *result.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusCode = %v, want %d", result.StatusCode, tt.wantStatusCode)
			}
			if result.ErrorMsg != nil {
				t.Errorf("ErrorMsg = %q, want nil", *result.ErrorMsg)
			}
		})
	}
}

func TestRunHTTPProbeZeroTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	monitor := testMonitor(srv.URL)
	monitor.Timeout = 0

	result := RunHTTPProbe(monitor)

	if result.Status != models.StatusUp {
		t.Errorf("Status = %q, want %q", result.Status, models.StatusUp)
	}
	if result.StatusCode == nil || *result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %v, want %d", result.StatusCode, http.StatusOK)
	}
}

func TestRunHTTPProbeTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := RunHTTPProbe(testMonitor(srv.URL))

	if _, err := time.Parse(timestampFormat, result.Timestamp); err != nil {
		t.Errorf("Timestamp = %q, want format %q: %v", result.Timestamp, timestampFormat, err)
	}
}
