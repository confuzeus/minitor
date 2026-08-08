package handlers

import "net/http"

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
