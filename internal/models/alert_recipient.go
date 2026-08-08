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
