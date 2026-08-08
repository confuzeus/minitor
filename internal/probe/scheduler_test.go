package probe

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/confuzeus/minitor/internal/database"
	"github.com/confuzeus/minitor/internal/models"
)

func TestSchedulerIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	m := models.Monitor{
		Name:     "test-monitor",
		URL:      server.URL,
		Type:     "http",
		Interval: 1,
		Timeout:  5,
		Enabled:  true,
	}
	if err := models.CreateMonitor(db, &m); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	resultCh := make(chan models.ProbeResult, 1)
	sched := NewScheduler(db)
	sched.MaxJitter = 100 * time.Millisecond
	sched.SetNotifier(func(monitorID int64, result models.ProbeResult) {
		if monitorID != m.ID {
			t.Errorf("notified for monitor %d, want %d", monitorID, m.ID)
		}
		resultCh <- result
	})

	sched.Start()
	defer sched.Stop()

	var result models.ProbeResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for probe result")
	}

	if result.Status != models.StatusUp {
		t.Errorf("probe status = %q, want %q", result.Status, models.StatusUp)
	}
	if result.StatusCode == nil || *result.StatusCode != http.StatusOK {
		t.Errorf("probe status code = %v, want %d", result.StatusCode, http.StatusOK)
	}

	results, err := models.GetResultsByMonitorID(db, m.ID, 10, 0)
	if err != nil {
		t.Fatalf("get results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d probe results in db, want 1", len(results))
	}
	if results[0].Status != models.StatusUp {
		t.Errorf("db status = %q, want %q", results[0].Status, models.StatusUp)
	}
}

func TestSchedulerAddDisabledMonitorIsNoop(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	sched := NewScheduler(db)
	sched.MaxJitter = time.Millisecond
	sched.Start()
	defer sched.Stop()

	notified := false
	sched.SetNotifier(func(monitorID int64, result models.ProbeResult) {
		notified = true
	})

	m := models.Monitor{
		Name:     "disabled",
		URL:      "http://example.com",
		Type:     "http",
		Interval: 1,
		Timeout:  5,
		Enabled:  false,
	}
	if err := models.CreateMonitor(db, &m); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	sched.AddMonitor(m)

	time.Sleep(300 * time.Millisecond)
	if notified {
		t.Error("disabled monitor was probed")
	}

	results, err := models.GetResultsByMonitorID(db, m.ID, 10, 0)
	if err != nil {
		t.Fatalf("get results: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d probe results for disabled monitor, want 0", len(results))
	}
}

func TestSchedulerRemoveMonitorStopsProbing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	m := models.Monitor{
		Name:     "remove-me",
		URL:      server.URL,
		Type:     "http",
		Interval: 1,
		Timeout:  5,
		Enabled:  true,
	}
	if err := models.CreateMonitor(db, &m); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	resultCh := make(chan models.ProbeResult, 100)
	sched := NewScheduler(db)
	sched.MaxJitter = time.Millisecond
	sched.SetNotifier(func(monitorID int64, result models.ProbeResult) {
		resultCh <- result
	})

	sched.Start()
	defer sched.Stop()

	select {
	case <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first probe")
	}

	sched.RemoveMonitor(m.ID)

	time.Sleep(300 * time.Millisecond)
	before := len(resultCh)
	time.Sleep(1200 * time.Millisecond)
	after := len(resultCh)

	if after != before {
		t.Errorf("probes continued after removal: %d before, %d after", before, after)
	}

	sched.RemoveMonitor(m.ID)
}

func TestSchedulerDisableMonitorStopsExistingTicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	m := models.Monitor{
		Name:     "disable-me",
		URL:      server.URL,
		Type:     "http",
		Interval: 1,
		Timeout:  5,
		Enabled:  true,
	}
	if err := models.CreateMonitor(db, &m); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	var mu sync.Mutex
	count := 0
	sched := NewScheduler(db)
	sched.MaxJitter = time.Millisecond
	sched.SetNotifier(func(monitorID int64, result models.ProbeResult) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	sched.Start()
	defer sched.Stop()

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		c := count
		mu.Unlock()
		if c > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first probe")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	disabled := m
	disabled.Enabled = false
	sched.AddMonitor(disabled)

	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	before := count
	mu.Unlock()
	time.Sleep(1200 * time.Millisecond)
	mu.Lock()
	after := count
	mu.Unlock()

	if after != before {
		t.Errorf("probes continued after disabling: %d before, %d after", before, after)
	}
}

func TestSchedulerStopIdempotent(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	sched := NewScheduler(db)
	sched.Start()
	sched.Stop()
	sched.Stop()

	sched2 := NewScheduler(db)
	sched2.Stop()
}

func TestSchedulerStopGracefully(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	m := models.Monitor{
		Name:     "stop-me",
		URL:      server.URL,
		Type:     "http",
		Interval: 1,
		Timeout:  5,
		Enabled:  true,
	}
	if err := models.CreateMonitor(db, &m); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	resultCh := make(chan models.ProbeResult, 100)
	sched := NewScheduler(db)
	sched.MaxJitter = time.Millisecond
	sched.SetNotifier(func(monitorID int64, result models.ProbeResult) {
		resultCh <- result
	})

	sched.Start()

	select {
	case <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first probe")
	}

	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s")
	}
}
