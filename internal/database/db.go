package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	driverName       = "sqlite"
	migrationsDir    = "migrations"
	migrationTimeout = 30 * time.Second
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if necessary) the SQLite database at path, enables WAL
// mode and supporting pragmas, and applies any pending migrations.
//
// The parent directory of path is created if it does not exist. Open returns an
// error if the database cannot be opened or migrations fail to apply.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %s: %w", path, err)
	}

	// SQLite permits a single writer at a time; limiting the pool to one
	// connection avoids SQLITE_BUSY contention from concurrent requests.
	db.SetMaxOpenConns(1)

	if err := configure(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func configure(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -2000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("enable WAL journal mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("enable WAL journal mode: got %q, want %q", journalMode, "wal")
	}

	return nil
}

type migration struct {
	version int64
	name    string
	sql     string
}

func runMigrations(db *sql.DB) error {
	if err := ensureSchemaMigrations(db); err != nil {
		return err
	}

	version, dirty, err := currentVersion(db)
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("database is in a dirty state from an interrupted migration; manual intervention required")
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if err := applyMigration(db, m, version); err != nil {
			return err
		}
		version = m.version
	}

	return nil
}

// ensureSchemaMigrations creates the schema_migrations tracking table if it is
// absent so the runner is usable even before migration 005 has been applied.
func ensureSchemaMigrations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT  PRIMARY KEY,
		dirty   BOOLEAN NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

// currentVersion reads the single tracking row and seeds it to version 0 when
// no migration has ever been applied.
func currentVersion(db *sql.DB) (int64, bool, error) {
	var version int64
	var dirty bool
	err := db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.Exec("INSERT OR IGNORE INTO schema_migrations (version, dirty) VALUES (0, 0)"); err != nil {
			return 0, false, fmt.Errorf("initialize migration state: %w", err)
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read migration state: %w", err)
	}
	return version, dirty, nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var migrations []migration
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := parseMigrationVersion(name)
		if err != nil {
			return nil, err
		}
		data, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", name, err)
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(data)})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func parseMigrationVersion(name string) (int64, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration filename %q: expected NNN_description.sql", name)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid migration filename %q: %w", name, err)
	}
	return version, nil
}

// applyMigration runs a single forward migration. The database is marked dirty
// in its own committed statement before the migration executes so that a crash
// mid-migration is detected on the next startup. The migration and its new
// version are committed atomically; on a clean failure the transaction rolls
// back, leaving the schema untouched, so the dirty flag is cleared to keep the
// database recoverable. Only a crash (which skips that clearing) leaves the
// dirty flag set and requiring manual intervention.
func applyMigration(db *sql.DB, m migration, prev int64) (err error) {
	if _, err := db.Exec("UPDATE schema_migrations SET dirty = 1 WHERE version = ?", prev); err != nil {
		return fmt.Errorf("mark migration %d (%s) dirty: %w", m.version, m.name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		clearDirty(db, m, prev)
		return fmt.Errorf("begin migration %d (%s): %w", m.version, m.name, err)
	}

	defer func() {
		_ = tx.Rollback()
		if err != nil {
			clearDirty(db, m, prev)
		}
	}()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", m.version, m.name, err)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE schema_migrations SET version = ?, dirty = 0 WHERE version = ?", m.version, prev); err != nil {
		return fmt.Errorf("record migration %d (%s): %w", m.version, m.name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d (%s): %w", m.version, m.name, err)
	}

	slog.Info("applied migration", "version", m.version, "name", m.name)
	return nil
}

func clearDirty(db *sql.DB, m migration, prev int64) {
	if _, err := db.Exec("UPDATE schema_migrations SET dirty = 0 WHERE version = ?", prev); err != nil {
		slog.Error("clear dirty migration flag", "version", m.version, "name", m.name, "error", err)
	}
}
