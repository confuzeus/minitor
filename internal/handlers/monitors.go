package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/confuzeus/minitor/internal/models"
	"github.com/go-chi/chi/v5"
)

const defaultInterval = 60
const defaultTimeout = 30

// ListMonitors renders the monitors overview page with each monitor's latest
// probe result.
func (h *Handler) ListMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := models.ListMonitors(h.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cards := make([]MonitorCard, 0, len(monitors))
	for _, m := range monitors {
		result, err := models.GetLatestResultForMonitor(h.DB, m.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cards = append(cards, MonitorCard{Monitor: m, LatestResult: result})
	}

	data := map[string]any{
		"Title":          "Monitors",
		"CurrentPage":    "monitors",
		"ShowNav":        true,
		"Authenticated":  h.Settings.AdminPassword != "",
		"SMTPConfigured": h.Settings.SMTP.Host != "",
		"Monitors":       cards,
	}
	if err := h.Templates.Render(w, "monitor_list", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// NewMonitor renders the create-monitor form with default values.
func (h *Handler) NewMonitor(w http.ResponseWriter, r *http.Request) {
	m := models.Monitor{
		Type:            "http",
		Interval:        defaultInterval,
		Timeout:         defaultTimeout,
		FollowRedirects: false,
		Enabled:         true,
	}
	h.renderMonitorForm(w, r, true, m, "")
}

// CreateMonitor parses and validates the submitted form, inserts the monitor,
// and starts its ticker in the scheduler.
func (h *Handler) CreateMonitor(w http.ResponseWriter, r *http.Request) {
	m, err := parseMonitorForm(r)
	if err != nil {
		h.renderMonitorForm(w, r, true, m, err.Error())
		return
	}
	if err := models.CreateMonitor(h.DB, &m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Scheduler.AddMonitor(m)
	h.redirect(w, r, "/monitors")
}

// MonitorDetail renders a single monitor with its recent probe results.
func (h *Handler) MonitorDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}

	data, err := h.monitorDetailData(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if data == nil {
		http.NotFound(w, r)
		return
	}

	data["Title"] = data["Monitor"].(models.Monitor).Name
	data["CurrentPage"] = "monitors"
	data["ShowNav"] = true
	data["Authenticated"] = h.Settings.AdminPassword != ""
	data["SMTPConfigured"] = h.Settings.SMTP.Host != ""

	if err := h.Templates.Render(w, "monitor_detail", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// monitorDetailData loads a monitor and its recent probe results for the
// monitor detail page and its HTMX polling fragment. It returns nil data when
// the monitor does not exist.
func (h *Handler) monitorDetailData(id int64) (map[string]any, error) {
	m, err := models.GetMonitorByID(h.DB, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}

	results, err := models.GetLastNResults(h.DB, id, 50)
	if err != nil {
		return nil, err
	}

	latest, err := models.GetLatestResultForMonitor(h.DB, id)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"Monitor": *m,
		"Latest":  latest,
		"Results": results,
	}, nil
}

// EditMonitor renders the edit form pre-populated with the monitor's values.
func (h *Handler) EditMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}

	m, err := models.GetMonitorByID(h.DB, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if m == nil {
		http.NotFound(w, r)
		return
	}

	h.renderMonitorForm(w, r, false, *m, "")
}

// UpdateMonitor parses and validates the submitted form, updates the monitor
// in the database, and restarts its ticker in the scheduler.
func (h *Handler) UpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}

	m, err := parseMonitorForm(r)
	if err != nil {
		m.ID = id
		h.renderMonitorForm(w, r, false, m, err.Error())
		return
	}
	m.ID = id

	if err := models.UpdateMonitor(h.DB, &m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Scheduler.AddMonitor(m)
	h.redirect(w, r, "/monitors/"+strconv.FormatInt(id, 10))
}

// DeleteMonitor removes the monitor from the database and stops its ticker in
// the scheduler.
func (h *Handler) DeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}

	if err := models.DeleteMonitor(h.DB, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Scheduler.RemoveMonitor(id)
	h.redirect(w, r, "/monitors")
}

// parseMonitorForm reads and validates the monitor fields from a submitted
// form. All fields are parsed even when one is invalid so the returned monitor
// can be re-rendered without losing the user's input; the first validation
// error is returned.
func parseMonitorForm(r *http.Request) (models.Monitor, error) {
	m := models.Monitor{
		Name:            strings.TrimSpace(r.FormValue("name")),
		URL:             strings.TrimSpace(r.FormValue("url")),
		Type:            r.FormValue("type"),
		FollowRedirects: r.FormValue("follow_redirects") == "on",
		Enabled:         r.FormValue("enabled") == "on",
	}
	if m.Type == "" {
		m.Type = "http"
	}

	var firstErr error
	require := func(msg string, ok bool) {
		if !ok && firstErr == nil {
			firstErr = errors.New(msg)
		}
	}

	require("name is required", m.Name != "")
	require("name must be 100 characters or fewer", len([]rune(m.Name)) <= 100)
	require("url is required", m.URL != "")

	if interval, ok := parsePositiveInt(r.FormValue("interval")); ok {
		m.Interval = interval
	} else {
		require("interval must be a positive integer (seconds)", false)
	}
	if timeout, ok := parsePositiveInt(r.FormValue("timeout")); ok {
		m.Timeout = timeout
	} else {
		require("timeout must be a positive integer (seconds)", false)
	}
	if expected := strings.TrimSpace(r.FormValue("expected_status_code")); expected != "" {
		code, err := strconv.Atoi(expected)
		if err != nil || code < 100 || code > 599 {
			require("expected status code must be a number between 100 and 599", false)
		} else {
			m.ExpectedStatusCode = &code
		}
	}

	return m, firstErr
}

func parsePositiveInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return 0, false
	}
	return v, true
}

// redirect responds to an htmx request with an HX-Redirect header (a full-page
// client-side navigation) and falls back to a standard 302 redirect for plain
// HTTP clients.
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, url string) {
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", url)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// renderMonitorForm renders the monitor form, either for creation (isNew) or
// editing (pre-populated with m).
func (h *Handler) renderMonitorForm(w http.ResponseWriter, r *http.Request, isNew bool, m models.Monitor, errMsg string) {
	title := "Edit Monitor"
	if isNew {
		title = "New Monitor"
	}
	data := map[string]any{
		"Title":          title,
		"CurrentPage":    "monitors",
		"ShowNav":        true,
		"Authenticated":  h.Settings.AdminPassword != "",
		"SMTPConfigured": h.Settings.SMTP.Host != "",
		"IsNew":          isNew,
		"Monitor":        m,
		"Error":          errMsg,
	}
	if err := h.Templates.Render(w, "monitor_form", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
