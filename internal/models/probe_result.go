package models

import (
	"database/sql"
	"fmt"
)

const (
	StatusUp   = "up"
	StatusDown = "down"
)

type ProbeResult struct {
	ID         int64
	MonitorID  int64
	Status     string
	StatusCode *int
	LatencyMs  *int64
	ErrorMsg   *string
	Timestamp  string
}

const probeResultColumns = "id, monitor_id, status, status_code, latency_ms, error_msg, timestamp"

func scanProbeResult(row interface{ Scan(...any) error }) (*ProbeResult, error) {
	var r ProbeResult
	var statusCode sql.NullInt64
	var latencyMs sql.NullInt64
	var errorMsg sql.NullString
	err := row.Scan(&r.ID, &r.MonitorID, &r.Status, &statusCode, &latencyMs,
		&errorMsg, &r.Timestamp)
	if err != nil {
		return nil, err
	}
	if statusCode.Valid {
		v := int(statusCode.Int64)
		r.StatusCode = &v
	}
	if latencyMs.Valid {
		v := latencyMs.Int64
		r.LatencyMs = &v
	}
	if errorMsg.Valid {
		v := errorMsg.String
		r.ErrorMsg = &v
	}
	return &r, nil
}

func InsertResult(db *sql.DB, r *ProbeResult) error {
	query := `INSERT INTO probe_results (monitor_id, status, status_code, latency_ms, error_msg, timestamp)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		RETURNING id, timestamp`
	var statusCode, latencyMs any
	if r.StatusCode != nil {
		statusCode = *r.StatusCode
	}
	if r.LatencyMs != nil {
		latencyMs = *r.LatencyMs
	}
	err := db.QueryRow(query, r.MonitorID, r.Status, statusCode, latencyMs, r.ErrorMsg).
		Scan(&r.ID, &r.Timestamp)
	if err != nil {
		return fmt.Errorf("insert probe result: %w", err)
	}
	return nil
}

func GetResultsByMonitorID(db *sql.DB, monitorID int64, limit, offset int) ([]ProbeResult, error) {
	query := `SELECT ` + probeResultColumns + ` FROM probe_results
		WHERE monitor_id = ? ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	rows, err := db.Query(query, monitorID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get probe results for monitor %d: %w", monitorID, err)
	}
	defer rows.Close()

	results := []ProbeResult{}
	for rows.Next() {
		r, err := scanProbeResult(rows)
		if err != nil {
			return nil, fmt.Errorf("scan probe result: %w", err)
		}
		results = append(results, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get probe results for monitor %d: %w", monitorID, err)
	}
	return results, nil
}

func GetLatestResultForMonitor(db *sql.DB, monitorID int64) (*ProbeResult, error) {
	query := `SELECT ` + probeResultColumns + ` FROM probe_results
		WHERE monitor_id = ? ORDER BY timestamp DESC, id DESC LIMIT 1`
	r, err := scanProbeResult(db.QueryRow(query, monitorID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest probe result for monitor %d: %w", monitorID, err)
	}
	return r, nil
}

// GetCurrentOutageStart returns the first failed probe in a monitor's current
// run of failures: the earliest "down" result after the most recent "up"
// result that precedes the given result (the one that triggered an alert). It
// returns nil when no such failure run exists, for example when the monitor
// has never failed.
//
// Results are ordered by their autoincrementing id, which increases with time
// for a given monitor because its probes run sequentially.
func GetCurrentOutageStart(db *sql.DB, monitorID int64, beforeID int64) (*ProbeResult, error) {
	query := `SELECT ` + probeResultColumns + ` FROM probe_results
		WHERE monitor_id = ? AND status = ? AND id <= ?
		  AND id > COALESCE((
		      SELECT MAX(id) FROM probe_results
		      WHERE monitor_id = ? AND status = ? AND id < ?
		  ), 0)
		ORDER BY id ASC LIMIT 1`
	r, err := scanProbeResult(db.QueryRow(query, monitorID, StatusDown, beforeID,
		monitorID, StatusUp, beforeID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get current outage start for monitor %d: %w", monitorID, err)
	}
	return r, nil
}

func GetLastNResults(db *sql.DB, monitorID int64, n int) ([]ProbeResult, error) {
	query := `SELECT ` + probeResultColumns + ` FROM probe_results
		WHERE monitor_id = ? ORDER BY timestamp DESC, id DESC LIMIT ?`
	rows, err := db.Query(query, monitorID, n)
	if err != nil {
		return nil, fmt.Errorf("get last %d probe results for monitor %d: %w", n, monitorID, err)
	}
	defer rows.Close()

	results := []ProbeResult{}
	for rows.Next() {
		r, err := scanProbeResult(rows)
		if err != nil {
			return nil, fmt.Errorf("scan probe result: %w", err)
		}
		results = append(results, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get last %d probe results for monitor %d: %w", n, monitorID, err)
	}
	return results, nil
}
