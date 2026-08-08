package auth

import (
	"net/http"
	"strings"

	"github.com/confuzeus/minitor/internal/settings"
)

// AuthMiddleware protects all routes behind a valid session cookie unless no
// admin password is configured. When ADMIN_PASSWORD is empty, every request is
// allowed through untouched. Otherwise, unauthenticated requests receive a 401
// for /api/* routes and are redirected to /login for page routes. The /login,
// /api/status, and /static/* routes are always public.
func AuthMiddleware(settings *settings.Settings) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if settings == nil || settings.AdminPassword == "" {
			return next
		}

		signingKey := []byte(settings.SecretKey)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if !ValidateSessionCookie(r, signingKey) {
				if isAPIRoute(r.URL.Path) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isPublicRoute(path string) bool {
	if path == "/static" || strings.HasPrefix(path, "/static/") {
		return true
	}
	path = strings.TrimSuffix(path, "/")
	return path == "/login" || path == "/api/status"
}

func isAPIRoute(path string) bool {
	return strings.HasPrefix(path, "/api/")
}
