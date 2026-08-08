package models

import (
	"database/sql"
	"fmt"
)

type AlertRecipient struct {
	ID        int64
	Name      string
	Email     string
	CreatedAt string
}

const alertRecipientColumns = "id, name, email, created_at"

func scanAlertRecipient(row interface{ Scan(...any) error }) (*AlertRecipient, error) {
	var r AlertRecipient
	err := row.Scan(&r.ID, &r.Name, &r.Email, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func CreateRecipient(db *sql.DB, r *AlertRecipient) error {
	query := `INSERT INTO alert_recipients (name, email, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		RETURNING id, created_at`
	err := db.QueryRow(query, r.Name, r.Email).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return fmt.Errorf("create alert recipient: %w", err)
	}
	return nil
}

// CreateRecipientWithAlerts creates a recipient and its monitor associations
// in a single transaction so a failure on any association rolls back the
// recipient too. Each alert's RecipientID is set to the new recipient's ID.
func CreateRecipientWithAlerts(db *sql.DB, r *AlertRecipient, alerts []MonitorAlert) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("create alert recipient: %w", err)
	}
	defer tx.Rollback()

	query := `INSERT INTO alert_recipients (name, email, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		RETURNING id, created_at`
	if err := tx.QueryRow(query, r.Name, r.Email).Scan(&r.ID, &r.CreatedAt); err != nil {
		return fmt.Errorf("create alert recipient: %w", err)
	}

	for _, a := range alerts {
		a.RecipientID = r.ID
		query := `INSERT INTO monitor_alerts (monitor_id, recipient_id, on_down, on_recovery, consecutive_failures)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(monitor_id, recipient_id) DO UPDATE SET
				on_down = excluded.on_down,
				on_recovery = excluded.on_recovery,
				consecutive_failures = excluded.consecutive_failures`
		if _, err := tx.Exec(query, a.MonitorID, a.RecipientID, a.OnDown, a.OnRecovery,
			a.ConsecutiveFailures); err != nil {
			return fmt.Errorf("create monitor alert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create alert recipient: %w", err)
	}
	return nil
}

func ListRecipients(db *sql.DB) ([]AlertRecipient, error) {
	query := "SELECT " + alertRecipientColumns + " FROM alert_recipients ORDER BY name"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list alert recipients: %w", err)
	}
	defer rows.Close()

	recipients := []AlertRecipient{}
	for rows.Next() {
		r, err := scanAlertRecipient(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert recipient: %w", err)
		}
		recipients = append(recipients, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alert recipients: %w", err)
	}
	return recipients, nil
}

func DeleteRecipient(db *sql.DB, id int64) error {
	res, err := db.Exec("DELETE FROM alert_recipients WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete alert recipient %d: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete alert recipient %d: %w", id, err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
