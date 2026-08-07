package handlers

import "net/http"

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.Render(w, "dashboard", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
