package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "minitor.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"monitors", "probe_results", "alert_recipients", "monitor_alerts", "schema_migrations"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q does not exist after Open", table)
		}
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var version int64
	var dirty bool
	if err := db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration state: %v", err)
	}
	if version != 5 {
		t.Errorf("migration version = %d, want 5", version)
	}
	if dirty {
		t.Error("migration state is dirty, want clean")
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	path := filepath.Join(dir, "minitor.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}

func TestOpenIdempotentMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "minitor.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("first Open returned error: %v", err)
	}
	db.Close()

	db, err = Open(path)
	if err != nil {
		t.Fatalf("second Open returned error: %v", err)
	}
	defer db.Close()

	var version int64
	if err := db.QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 5 {
		t.Errorf("migration version = %d, want 5 (no migration should re-run)", version)
	}
}

func TestForeignKeyEnforcement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "minitor.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("INSERT INTO probe_results (monitor_id, status) VALUES (999, 'up')")
	if err == nil {
		t.Fatal("inserting probe_results with nonexistent monitor_id succeeded, want foreign key violation")
	}
}

func TestFailedMigrationClearsDirtyFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "minitor.db")

	// Pre-create a table that conflicts with migration 001 so that applying
	// migrations fails cleanly.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE monitors (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create conflicting table: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err == nil {
		db.Close()
		t.Fatal("Open succeeded despite conflicting schema, want migration error")
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database for inspection: %v", err)
	}
	defer check.Close()

	var dirty int
	if err := check.QueryRow("SELECT dirty FROM schema_migrations").Scan(&dirty); err != nil {
		t.Fatalf("read dirty flag: %v", err)
	}
	if dirty != 0 {
		t.Errorf("dirty = %d after failed migration, want 0 (clean failure must not brick the database)", dirty)
	}

	// Resolve the conflict and confirm migrations apply on the next Open.
	if _, err := check.Exec("DROP TABLE monitors"); err != nil {
		t.Fatalf("drop conflicting table: %v", err)
	}
	check.Close()

	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open after resolving conflict returned error: %v", err)
	}
	defer db.Close()

	var version int64
	if err := db.QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 5 {
		t.Errorf("migration version = %d, want 5", version)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master for %q: %v", name, err)
	}
	return count == 1
}
