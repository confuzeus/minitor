package models

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/confuzeus/minitor/internal/database"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "minitor.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertRawResult(t *testing.T, db *sql.DB, monitorID int64, status, timestamp string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(
		`INSERT INTO probe_results (monitor_id, status, timestamp) VALUES (?, ?, ?) RETURNING id`,
		monitorID, status, timestamp,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert probe result: %v", err)
	}
	return id
}

func TestGetCurrentOutageStart(t *testing.T) {
	db := newTestDB(t)

	monitor := Monitor{Name: "Example", URL: "https://example.com", Type: "http", Interval: 60, Timeout: 5, Enabled: true}
	if err := CreateMonitor(db, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	up1 := insertRawResult(t, db, monitor.ID, StatusUp, "2026-08-08 10:00:00")
	down1 := insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:01:00")
	insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:02:00")
	insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:03:00")
	up2 := insertRawResult(t, db, monitor.ID, StatusUp, "2026-08-08 10:04:00")
	down4 := insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:05:00")

	tests := []struct {
		name      string
		beforeID  int64
		wantID    int64
		wantFound bool
	}{
		{name: "long outage at recovery", beforeID: up2, wantID: down1, wantFound: true},
		{name: "first failure at down alert", beforeID: down1, wantID: down1, wantFound: true},
		{name: "current run excludes earlier outage", beforeID: down4, wantID: down4, wantFound: true},
		{name: "no failures yet", beforeID: up1, wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetCurrentOutageStart(db, monitor.ID, tt.beforeID)
			if err != nil {
				t.Fatalf("GetCurrentOutageStart: %v", err)
			}
			if !tt.wantFound {
				if got != nil {
					t.Fatalf("got result %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a result")
			}
			if got.ID != tt.wantID {
				t.Errorf("outage start id = %d, want %d", got.ID, tt.wantID)
			}
		})
	}
}

func TestGetCurrentOutageStartWithoutPriorUp(t *testing.T) {
	db := newTestDB(t)

	monitor := Monitor{Name: "Example", URL: "https://example.com", Type: "http", Interval: 60, Timeout: 5, Enabled: true}
	if err := CreateMonitor(db, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	first := insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:00:00")
	insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:01:00")

	got, err := GetCurrentOutageStart(db, monitor.ID, insertRawResult(t, db, monitor.ID, StatusDown, "2026-08-08 10:02:00"))
	if err != nil {
		t.Fatalf("GetCurrentOutageStart: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want the earliest failure")
	}
	if got.ID != first {
		t.Errorf("outage start id = %d, want %d", got.ID, first)
	}
}
