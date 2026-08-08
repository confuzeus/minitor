package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/confuzeus/minitor/internal/models"
	"github.com/go-chi/chi/v5"
)

const defaultConsecutiveFailures = 3

// AssignedMonitor pairs a monitor with the alert settings for a recipient.
type AssignedMonitor struct {
	Monitor    models.Monitor
	OnDown     bool
	OnRecovery bool
}

// RecipientWithMonitors pairs an alert recipient with the monitors it is
// assigned to.
type RecipientWithMonitors struct {
	Recipient        models.AlertRecipient
	AssignedMonitors []AssignedMonitor
}

// alertFormState carries the submitted recipient form values so they can be
// re-rendered without losing input when validation fails.
type alertFormState struct {
	Name       string
	Email      string
	OnDown     bool
	OnRecovery bool
	Selected   map[int64]bool
}

// ListAlerts renders the alert recipients page with the add-recipient form.
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	h.renderAlerts(w, r, alertFormState{
		OnDown:     true,
		OnRecovery: true,
	}, "")
}

// CreateAlertRecipient parses and validates the submitted form, creates the
// recipient, and links it to the selected monitors.
func (h *Handler) CreateAlertRecipient(w http.ResponseWriter, r *http.Request) {
	form, err := parseAlertForm(r)
	if err != nil {
		h.renderAlerts(w, r, form, err.Error())
		return
	}

	recipient := models.AlertRecipient{
		Name:  form.Name,
		Email: form.Email,
	}
	alerts := make([]models.MonitorAlert, 0, len(form.Selected))
	for id := range form.Selected {
		alerts = append(alerts, models.MonitorAlert{
			MonitorID:           id,
			OnDown:              form.OnDown,
			OnRecovery:          form.OnRecovery,
			ConsecutiveFailures: defaultConsecutiveFailures,
		})
	}
	if err := models.CreateRecipientWithAlerts(h.DB, &recipient, alerts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/alerts")
}

// DeleteAlertRecipient removes a recipient. Its monitor associations are
// cleaned up by the foreign key cascade.
func (h *Handler) DeleteAlertRecipient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid recipient id", http.StatusBadRequest)
		return
	}

	if err := models.DeleteRecipient(h.DB, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/alerts")
}

// parseAlertForm reads and validates the recipient fields from a submitted
// form. The parsed form is always returned so it can be re-rendered on error.
func parseAlertForm(r *http.Request) (alertFormState, error) {
	form := alertFormState{
		Name:       strings.TrimSpace(r.FormValue("name")),
		Email:      strings.TrimSpace(r.FormValue("email")),
		OnDown:     r.FormValue("on_down") == "on",
		OnRecovery: r.FormValue("on_recovery") == "on",
		Selected:   make(map[int64]bool),
	}

	for _, idStr := range r.Form["monitor_ids"] {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		form.Selected[id] = true
	}

	if form.Name == "" {
		return form, errors.New("name is required")
	}
	if form.Email == "" {
		return form, errors.New("email is required")
	}
	// ParseAddress accepts display-name forms like "Bob <bob@example.com>";
	// reject them so only a bare address is stored and used as the SMTP
	// recipient.
	addr, err := mail.ParseAddress(form.Email)
	if err != nil || addr.Name != "" || addr.Address != form.Email {
		return form, errors.New("email must be a valid address")
	}

	return form, nil
}

// renderAlerts loads the recipients, monitors, and their associations and
// renders the alerts page.
func (h *Handler) renderAlerts(w http.ResponseWriter, r *http.Request, form alertFormState, errMsg string) {
	recipients, err := models.ListRecipients(h.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	monitors, err := models.ListMonitors(h.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	alerts, err := models.GetAllMonitorAlerts(h.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	alertsByRecipient := make(map[int64][]models.MonitorAlert)
	for _, a := range alerts {
		alertsByRecipient[a.RecipientID] = append(alertsByRecipient[a.RecipientID], a)
	}
	monitorByID := make(map[int64]models.Monitor, len(monitors))
	for _, m := range monitors {
		monitorByID[m.ID] = m
	}

	cards := make([]RecipientWithMonitors, 0, len(recipients))
	for _, rec := range recipients {
		assigned := []AssignedMonitor{}
		for _, a := range alertsByRecipient[rec.ID] {
			m, ok := monitorByID[a.MonitorID]
			if !ok {
				continue
			}
			assigned = append(assigned, AssignedMonitor{
				Monitor:    m,
				OnDown:     a.OnDown,
				OnRecovery: a.OnRecovery,
			})
		}
		cards = append(cards, RecipientWithMonitors{Recipient: rec, AssignedMonitors: assigned})
	}

	data := map[string]any{
		"Title":          "Alerts",
		"ShowNav":        true,
		"Authenticated":  h.Settings.AdminPassword != "",
		"SMTPConfigured": h.Settings.SMTP.Host != "",
		"Recipients":     cards,
		"Monitors":       monitors,
		"Form":           form,
		"Error":          errMsg,
	}
	if err := h.Templates.Render(w, "alerts", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
