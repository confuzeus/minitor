package handlers

import (
	"net/http"

	"github.com/confuzeus/minitor/internal/auth"
)

// LoginPage renders the login form.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Title":          "Login",
		"ShowNav":        false,
		"SMTPConfigured": h.Settings.SMTP.Host != "",
	}
	if err := h.Templates.Render(w, "login", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Login validates the submitted password and, on success, sets a session
// cookie and redirects to the dashboard.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")

	if !auth.ValidatePassword(password, h.Settings.AdminPassword) {
		w.WriteHeader(http.StatusUnauthorized)
		data := map[string]any{
			"Error":          "Invalid password",
			"Title":          "Login",
			"ShowNav":        false,
			"SMTPConfigured": h.Settings.SMTP.Host != "",
		}
		if err := h.Templates.Render(w, "login", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	cookie, err := auth.CreateSessionCookie(
		[]byte(h.Settings.SecretKey),
		auth.WithSecure(h.Settings.SecureCookies),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, cookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout clears the session cookie and redirects to the login page.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := auth.CreateSessionCookie([]byte(h.Settings.SecretKey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cookie.MaxAge = -1

	http.SetCookie(w, cookie)
	http.Redirect(w, r, "/login", http.StatusFound)
}
