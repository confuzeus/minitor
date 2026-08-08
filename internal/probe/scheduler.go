package probe

import (
	"database/sql"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/confuzeus/minitor/internal/models"
)

const defaultMaxJitter = 10 * time.Second

// NotifyFunc is called after a probe result is persisted. The alerter
// registers itself here to be notified of new results.
type NotifyFunc func(monitorID int64, result models.ProbeResult)

// Scheduler manages per-monitor tickers. All map access happens on a single
// goroutine driven by the add/remove/stop channels, avoiding data races.
// MaxJitter must be set before Start is called.
type Scheduler struct {
	db        *sql.DB
	MaxJitter time.Duration

	mu     sync.RWMutex
	notify NotifyFunc

	tickers map[int64]*time.Ticker
	stops   map[int64]chan struct{}

	addCh    chan models.Monitor
	removeCh chan int64
	stopCh   chan struct{}
	doneCh   chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
	started   atomic.Bool
}

func NewScheduler(db *sql.DB) *Scheduler {
	return &Scheduler{
		db:        db,
		MaxJitter: defaultMaxJitter,
		tickers:   make(map[int64]*time.Ticker),
		stops:     make(map[int64]chan struct{}),
		addCh:     make(chan models.Monitor),
		removeCh:  make(chan int64),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// SetNotifier registers the callback invoked with each new probe result.
// It may be called after Start.
func (s *Scheduler) SetNotifier(f NotifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = f
}

func (s *Scheduler) notifier() NotifyFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notify
}

// Start loads all enabled monitors and begins scheduling them. It returns
// immediately; the scheduling loop runs on a background goroutine. Calling
// Start more than once is a no-op.
func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		monitors, err := models.ListEnabledMonitors(s.db)
		if err != nil {
			slog.Error("scheduler: failed to load monitors", "error", err)
		} else {
			for _, m := range monitors {
				s.startTicker(m)
			}
			slog.Info("scheduler: started", "monitors", len(monitors))
		}
		s.started.Store(true)
		go s.loop()
	})
}

// Stop gracefully shuts the scheduler down: tickers are stopped and no new
// probes fire. It blocks until the scheduling loop has exited. Calling Stop
// before Start, or more than once, is a no-op.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		if !s.started.Load() {
			return
		}
		<-s.doneCh
	})
}

// AddMonitor schedules a monitor. If a ticker already exists for the monitor,
// it is replaced with the new config. A disabled monitor stops any existing
// ticker and is not scheduled. After Stop, this is a no-op. Calling this
// before Start is a no-op so it never blocks on the unstarted loop.
func (s *Scheduler) AddMonitor(m models.Monitor) {
	if !s.started.Load() {
		slog.Warn("scheduler: AddMonitor called before Start, ignoring", "monitor_id", m.ID)
		return
	}
	select {
	case s.addCh <- m:
	case <-s.stopCh:
	}
}

// RemoveMonitor stops and removes the ticker for the given monitor. After
// Stop, this is a no-op. Calling this before Start is a no-op so it never
// blocks on the unstarted loop.
func (s *Scheduler) RemoveMonitor(monitorID int64) {
	if !s.started.Load() {
		slog.Warn("scheduler: RemoveMonitor called before Start, ignoring", "monitor_id", monitorID)
		return
	}
	select {
	case s.removeCh <- monitorID:
	case <-s.stopCh:
	}
}

func (s *Scheduler) loop() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			for id, ticker := range s.tickers {
				ticker.Stop()
				close(s.stops[id])
			}
			slog.Info("scheduler: stopped")
			return
		case m := <-s.addCh:
			if existing, ok := s.tickers[m.ID]; ok {
				existing.Stop()
				close(s.stops[m.ID])
				delete(s.tickers, m.ID)
				delete(s.stops, m.ID)
			}
			if !m.Enabled {
				slog.Warn("scheduler: monitor disabled, removing ticker", "id", m.ID, "name", m.Name)
				continue
			}
			s.startTicker(m)
			slog.Info("scheduler: added monitor", "id", m.ID, "name", m.Name)
		case id := <-s.removeCh:
			if ticker, ok := s.tickers[id]; ok {
				ticker.Stop()
				close(s.stops[id])
				delete(s.tickers, id)
				delete(s.stops, id)
				slog.Info("scheduler: removed monitor", "id", id)
			}
		}
	}
}

// startTicker creates a ticker for the monitor and spawns a goroutine that
// runs the first probe after a short jitter, then once per interval. Probes
// for a monitor run serially so a slow probe drops the next tick rather than
// interleaving results out of order.
func (s *Scheduler) startTicker(m models.Monitor) {
	if m.Interval <= 0 {
		slog.Warn("scheduler: invalid interval, skipping monitor", "id", m.ID, "interval", m.Interval)
		return
	}

	interval := time.Duration(m.Interval) * time.Second
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	s.tickers[m.ID] = ticker
	s.stops[m.ID] = stop

	var jitter time.Duration
	if s.MaxJitter > 0 {
		jitter = time.Duration(rand.Int63n(int64(s.MaxJitter)))
	}

	go func(jitter time.Duration) {
		defer ticker.Stop()

		select {
		case <-time.After(jitter):
		case <-stop:
			return
		case <-s.stopCh:
			return
		}

		s.runProbe(m)

		for {
			select {
			case <-ticker.C:
				s.runProbe(m)
			case <-stop:
				return
			case <-s.stopCh:
				return
			}
		}
	}(jitter)
}

// runProbe executes the monitor's probe, persists the result, and notifies
// the alerter. It is safe to call concurrently from multiple goroutines.
func (s *Scheduler) runProbe(m models.Monitor) {
	var result models.ProbeResult

	switch m.Type {
	case "http":
		result = RunHTTPProbe(m)
	case "ping":
		slog.Warn("scheduler: ping probe not yet implemented", "monitor_id", m.ID, "name", m.Name)
		return
	default:
		slog.Warn("scheduler: unknown probe type", "type", m.Type, "monitor_id", m.ID)
		return
	}

	if err := models.InsertResult(s.db, &result); err != nil {
		slog.Error("scheduler: failed to insert probe result", "error", err, "monitor_id", m.ID)
		return
	}

	if notify := s.notifier(); notify != nil {
		notify(m.ID, result)
	}
}
