package alerter

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/confuzeus/minitor/internal/database"
	"github.com/confuzeus/minitor/internal/models"
)

func insertTestResult(t *testing.T, db *sql.DB, monitorID int64, status, timestamp string) models.ProbeResult {
	t.Helper()
	var r models.ProbeResult
	err := db.QueryRow(
		`INSERT INTO probe_results (monitor_id, status, timestamp) VALUES (?, ?, ?)
		 RETURNING id, monitor_id, status, timestamp`,
		monitorID, status, timestamp,
	).Scan(&r.ID, &r.MonitorID, &r.Status, &r.Timestamp)
	if err != nil {
		t.Fatalf("insert probe result: %v", err)
	}
	return r
}

func TestComputeDowntime(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()

	monitor := models.Monitor{Name: "Example", URL: "https://example.com", Type: "http", Interval: 60, Timeout: 5, Enabled: true}
	if err := models.CreateMonitor(db, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	insertTestResult(t, db, monitor.ID, models.StatusUp, "2026-08-08 10:00:00")
	firstDown := insertTestResult(t, db, monitor.ID, models.StatusDown, "2026-08-08 10:01:00")
	insertTestResult(t, db, monitor.ID, models.StatusDown, "2026-08-08 10:02:00")
	recovery := insertTestResult(t, db, monitor.ID, models.StatusUp, "2026-08-08 10:03:00")

	a := &Alerter{db: db}

	t.Run("recovery reports total outage", func(t *testing.T) {
		got := a.computeDowntime(monitor.ID, recovery)
		if !got.Valid {
			t.Fatal("downtime is invalid, want valid")
		}
		if got.Since != firstDown.Timestamp {
			t.Errorf("Since = %q, want %q", got.Since, firstDown.Timestamp)
		}
		if got.Duration != "2m0s" {
			t.Errorf("Duration = %q, want %q", got.Duration, "2m0s")
		}
	})

	t.Run("down alert reports zero elapsed", func(t *testing.T) {
		got := a.computeDowntime(monitor.ID, firstDown)
		if !got.Valid {
			t.Fatal("downtime is invalid, want valid")
		}
		if got.Since != firstDown.Timestamp {
			t.Errorf("Since = %q, want %q", got.Since, firstDown.Timestamp)
		}
		if got.Duration != "0s" {
			t.Errorf("Duration = %q, want %q", got.Duration, "0s")
		}
	})

	t.Run("no failures yields no downtime", func(t *testing.T) {
		up := insertTestResult(t, db, monitor.ID, models.StatusUp, "2026-08-08 10:04:00")
		got := a.computeDowntime(monitor.ID, up)
		if got.Valid {
			t.Errorf("downtime = %+v, want invalid", got)
		}
	})
}
