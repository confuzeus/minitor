package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Status responds with a JSON health check. It is public, performs no database
// query, and is used by external health checks and uptime probes.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// MonitorCards returns the monitor status cards HTML fragment consumed by the
// dashboard's HTMX polling (GET /api/monitors).
func (h *Handler) MonitorCards(w http.ResponseWriter, r *http.Request) {
	data, err := h.dashboardData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.Templates.ExecuteTemplate(w, "dashboard", "monitor-cards", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// MonitorStats returns the monitor overview and recent probe results HTML
// fragment consumed by the monitor detail page's HTMX polling
// (GET /api/monitors/{id}/stats).
func (h *Handler) MonitorStats(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Templates.ExecuteTemplate(w, "monitor_detail", "monitor-stats", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
