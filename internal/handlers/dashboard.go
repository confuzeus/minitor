package handlers

import (
	"net/http"

	"github.com/confuzeus/minitor/internal/models"
)

// MonitorCard pairs a monitor with its most recent probe result. LatestResult
// is nil when the monitor has never been checked.
type MonitorCard struct {
	Monitor      models.Monitor
	LatestResult *models.ProbeResult
}

// Dashboard renders the overview page listing all monitors with their latest
// probe result.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.dashboardData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.Templates.Render(w, "dashboard", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// dashboardData loads all monitors and their latest probe result. It is shared
// by the full page render and the HTMX polling fragment.
func (h *Handler) dashboardData() (map[string]any, error) {
	monitors, err := models.ListMonitors(h.DB)
	if err != nil {
		return nil, err
	}

	cards := make([]MonitorCard, 0, len(monitors))
	for _, m := range monitors {
		result, err := models.GetLatestResultForMonitor(h.DB, m.ID)
		if err != nil {
			return nil, err
		}
		cards = append(cards, MonitorCard{Monitor: m, LatestResult: result})
	}

	return map[string]any{
		"Title":          "Dashboard",
		"ShowNav":        true,
		"Authenticated":  h.Settings.AdminPassword != "",
		"SMTPConfigured": h.Settings.SMTP.Host != "",
		"Monitors":       cards,
	}, nil
}
