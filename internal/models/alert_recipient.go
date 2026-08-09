package models

import (
	"database/sql"
	"fmt"
	"strings"
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

func GetRecipientByID(db *sql.DB, id int64) (*AlertRecipient, error) {
	query := "SELECT " + alertRecipientColumns + " FROM alert_recipients WHERE id = ?"
	r, err := scanAlertRecipient(db.QueryRow(query, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get alert recipient %d: %w", id, err)
	}
	return r, nil
}

func GetAlertsByRecipientID(db *sql.DB, recipientID int64) ([]MonitorAlert, error) {
	query := `SELECT monitor_id, recipient_id, on_down, on_recovery, consecutive_failures
		FROM monitor_alerts
		WHERE recipient_id = ?
		ORDER BY monitor_id`
	rows, err := db.Query(query, recipientID)
	if err != nil {
		return nil, fmt.Errorf("get alerts for recipient %d: %w", recipientID, err)
	}
	defer rows.Close()

	alerts := []MonitorAlert{}
	for rows.Next() {
		var a MonitorAlert
		err := rows.Scan(&a.MonitorID, &a.RecipientID, &a.OnDown, &a.OnRecovery,
			&a.ConsecutiveFailures)
		if err != nil {
			return nil, fmt.Errorf("scan monitor alert: %w", err)
		}
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get alerts for recipient %d: %w", recipientID, err)
	}
	return alerts, nil
}

// UpdateRecipientWithAlerts updates a recipient's details and replaces its
// monitor associations in a single transaction so a failure on any step rolls
// back the whole update.
func UpdateRecipientWithAlerts(db *sql.DB, r *AlertRecipient, alerts []MonitorAlert) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("update alert recipient: %w", err)
	}
	defer tx.Rollback()

	query := `UPDATE alert_recipients SET name = ?, email = ? WHERE id = ? RETURNING id`
	if err := tx.QueryRow(query, r.Name, r.Email, r.ID).Scan(&r.ID); err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("update alert recipient: %w", err)
	}

	// Remove associations that are no longer selected.
	if len(alerts) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(alerts)), ",")
		args := make([]any, 0, len(alerts)+1)
		args = append(args, r.ID)
		for _, a := range alerts {
			args = append(args, a.MonitorID)
		}
		query := "DELETE FROM monitor_alerts WHERE recipient_id = ? AND monitor_id NOT IN (" + placeholders + ")"
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("replace monitor alerts: %w", err)
		}
	} else {
		if _, err := tx.Exec("DELETE FROM monitor_alerts WHERE recipient_id = ?", r.ID); err != nil {
			return fmt.Errorf("replace monitor alerts: %w", err)
		}
	}

	// Upsert the selected associations. On conflict the on_down/on_recovery
	// settings are updated from the form, while an existing consecutive_failures
	// threshold is preserved since it is not part of the form.
	for _, a := range alerts {
		a.RecipientID = r.ID
		query := `INSERT INTO monitor_alerts (monitor_id, recipient_id, on_down, on_recovery, consecutive_failures)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(monitor_id, recipient_id) DO UPDATE SET
				on_down = excluded.on_down,
				on_recovery = excluded.on_recovery`
		if _, err := tx.Exec(query, a.MonitorID, a.RecipientID, a.OnDown, a.OnRecovery,
			a.ConsecutiveFailures); err != nil {
			return fmt.Errorf("upsert monitor alert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update alert recipient: %w", err)
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
