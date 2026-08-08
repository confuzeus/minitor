package models

import (
	"database/sql"
	"fmt"
)

type MonitorAlert struct {
	MonitorID           int64
	RecipientID         int64
	OnDown              bool
	OnRecovery          bool
	ConsecutiveFailures int
}

// AlertConfig pairs an alert recipient with the alert settings for a monitor.
type AlertConfig struct {
	Recipient           AlertRecipient
	OnDown              bool
	OnRecovery          bool
	ConsecutiveFailures int
}

func GetAlertConfigs(db *sql.DB, monitorID int64) ([]AlertConfig, error) {
	query := `SELECT r.id, r.name, r.email, r.created_at, ma.on_down, ma.on_recovery, ma.consecutive_failures
		FROM monitor_alerts ma
		JOIN alert_recipients r ON r.id = ma.recipient_id
		WHERE ma.monitor_id = ?
		ORDER BY r.name`
	rows, err := db.Query(query, monitorID)
	if err != nil {
		return nil, fmt.Errorf("get alert configs for monitor %d: %w", monitorID, err)
	}
	defer rows.Close()

	configs := []AlertConfig{}
	for rows.Next() {
		var c AlertConfig
		err := rows.Scan(&c.Recipient.ID, &c.Recipient.Name, &c.Recipient.Email,
			&c.Recipient.CreatedAt, &c.OnDown, &c.OnRecovery, &c.ConsecutiveFailures)
		if err != nil {
			return nil, fmt.Errorf("scan alert config: %w", err)
		}
		configs = append(configs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get alert configs for monitor %d: %w", monitorID, err)
	}
	return configs, nil
}

func GetAllMonitorAlerts(db *sql.DB) ([]MonitorAlert, error) {
	query := `SELECT monitor_id, recipient_id, on_down, on_recovery, consecutive_failures
		FROM monitor_alerts
		ORDER BY monitor_id`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list all monitor alerts: %w", err)
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
		return nil, fmt.Errorf("list all monitor alerts: %w", err)
	}
	return alerts, nil
}

func CreateMonitorAlert(db *sql.DB, ma *MonitorAlert) error {
	query := `INSERT INTO monitor_alerts (monitor_id, recipient_id, on_down, on_recovery, consecutive_failures)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(monitor_id, recipient_id) DO UPDATE SET
			on_down = excluded.on_down,
			on_recovery = excluded.on_recovery,
			consecutive_failures = excluded.consecutive_failures`
	_, err := db.Exec(query, ma.MonitorID, ma.RecipientID, ma.OnDown, ma.OnRecovery,
		ma.ConsecutiveFailures)
	if err != nil {
		return fmt.Errorf("upsert monitor alert: %w", err)
	}
	return nil
}

func ListAlertsByMonitor(db *sql.DB, monitorID int64) ([]AlertRecipient, error) {
	query := `SELECT r.id, r.name, r.email, r.created_at
		FROM monitor_alerts ma
		JOIN alert_recipients r ON r.id = ma.recipient_id
		WHERE ma.monitor_id = ?
		ORDER BY r.name`
	rows, err := db.Query(query, monitorID)
	if err != nil {
		return nil, fmt.Errorf("list alerts for monitor %d: %w", monitorID, err)
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
		return nil, fmt.Errorf("list alerts for monitor %d: %w", monitorID, err)
	}
	return recipients, nil
}

func DeleteAlertsByMonitor(db *sql.DB, monitorID int64) error {
	_, err := db.Exec("DELETE FROM monitor_alerts WHERE monitor_id = ?", monitorID)
	if err != nil {
		return fmt.Errorf("delete alerts for monitor %d: %w", monitorID, err)
	}
	return nil
}

func GetRecipientsForAlert(db *sql.DB, monitorID int64, alertType string) ([]AlertRecipient, error) {
	var column string
	switch alertType {
	case StatusUp:
		column = "on_recovery"
	case StatusDown:
		column = "on_down"
	default:
		return nil, fmt.Errorf("get recipients for alert: unknown alert type %q", alertType)
	}
	query := `SELECT r.id, r.name, r.email, r.created_at
		FROM monitor_alerts ma
		JOIN alert_recipients r ON r.id = ma.recipient_id
		WHERE ma.monitor_id = ? AND ma.` + column + ` = 1
		ORDER BY r.name`
	rows, err := db.Query(query, monitorID)
	if err != nil {
		return nil, fmt.Errorf("get recipients for %s alert on monitor %d: %w", alertType, monitorID, err)
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
		return nil, fmt.Errorf("get recipients for %s alert on monitor %d: %w", alertType, monitorID, err)
	}
	return recipients, nil
}
