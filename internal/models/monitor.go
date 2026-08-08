package models

import (
	"database/sql"
	"fmt"
)

type Monitor struct {
	ID                 int64
	Name               string
	URL                string
	Type               string
	Interval           int
	Timeout            int
	FollowRedirects    bool
	Enabled            bool
	ExpectedStatusCode *int
	CreatedAt          string
	UpdatedAt          string
}

const monitorColumns = "id, name, url, type, interval, timeout, follow_redirects, enabled, expected_status_code, created_at, updated_at"

func scanMonitor(row interface{ Scan(...any) error }) (*Monitor, error) {
	var m Monitor
	var expectedStatus sql.NullInt64
	err := row.Scan(&m.ID, &m.Name, &m.URL, &m.Type, &m.Interval, &m.Timeout,
		&m.FollowRedirects, &m.Enabled, &expectedStatus, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if expectedStatus.Valid {
		v := int(expectedStatus.Int64)
		m.ExpectedStatusCode = &v
	}
	return &m, nil
}

func CreateMonitor(db *sql.DB, m *Monitor) error {
	if m.Interval < 1 {
		return fmt.Errorf("interval must be at least 1 second")
	}
	if m.Timeout < 1 {
		return fmt.Errorf("timeout must be at least 1 second")
	}
	query := `INSERT INTO monitors (name, url, type, interval, timeout, follow_redirects, enabled, expected_status_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`
	err := db.QueryRow(query, m.Name, m.URL, m.Type, m.Interval, m.Timeout,
		m.FollowRedirects, m.Enabled, m.ExpectedStatusCode).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create monitor: %w", err)
	}
	return nil
}

func ListMonitors(db *sql.DB) ([]Monitor, error) {
	query := "SELECT " + monitorColumns + " FROM monitors ORDER BY name"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	monitors := []Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitor: %w", err)
		}
		monitors = append(monitors, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	return monitors, nil
}

func ListEnabledMonitors(db *sql.DB) ([]Monitor, error) {
	query := "SELECT " + monitorColumns + " FROM monitors WHERE enabled = 1 ORDER BY name"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	defer rows.Close()

	monitors := []Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitor: %w", err)
		}
		monitors = append(monitors, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	return monitors, nil
}

func GetMonitorByID(db *sql.DB, id int64) (*Monitor, error) {
	query := "SELECT " + monitorColumns + " FROM monitors WHERE id = ?"
	m, err := scanMonitor(db.QueryRow(query, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get monitor %d: %w", id, err)
	}
	return m, nil
}

func UpdateMonitor(db *sql.DB, m *Monitor) error {
	if m.Interval < 1 {
		return fmt.Errorf("interval must be at least 1 second")
	}
	if m.Timeout < 1 {
		return fmt.Errorf("timeout must be at least 1 second")
	}
	query := `UPDATE monitors SET
		name = ?, url = ?, type = ?, interval = ?, timeout = ?,
		follow_redirects = ?, enabled = ?, expected_status_code = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING updated_at`
	err := db.QueryRow(query, m.Name, m.URL, m.Type, m.Interval, m.Timeout,
		m.FollowRedirects, m.Enabled, m.ExpectedStatusCode, m.ID).Scan(&m.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("update monitor %d: %w", m.ID, err)
	}
	return nil
}

func DeleteMonitor(db *sql.DB, id int64) error {
	res, err := db.Exec("DELETE FROM monitors WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete monitor %d: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete monitor %d: %w", id, err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
