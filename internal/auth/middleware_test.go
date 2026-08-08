package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/confuzeus/minitor/internal/settings"
)

func TestAuthMiddlewareNoPassword(t *testing.T) {
	mw := AuthMiddleware(&settings.Settings{AdminPassword: ""})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()

	mw(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when no admin password configured", rr.Code)
	}
}

func TestAuthMiddlewareNilSettings(t *testing.T) {
	mw := AuthMiddleware(nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()

	mw(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when settings is nil", rr.Code)
	}
}

func TestAuthMiddlewareAPIUnauthorized(t *testing.T) {
	mw := AuthMiddleware(&settings.Settings{
		AdminPassword: "hunter2",
		SecretKey:     "secret",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()

	mw(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/monitors", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for /api/* route", rr.Code)
	}
}

func TestAuthMiddlewarePageRedirect(t *testing.T) {
	mw := AuthMiddleware(&settings.Settings{
		AdminPassword: "hunter2",
		SecretKey:     "secret",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()

	mw(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect for page route", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestAuthMiddlewareAuthenticated(t *testing.T) {
	cfg := &settings.Settings{AdminPassword: "hunter2", SecretKey: "secret"}
	c, err := CreateSessionCookie([]byte(cfg.SecretKey))
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}

	mw := AuthMiddleware(cfg)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, path := range []string{"/dashboard", "/api/monitors"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(c)
			rr := httptest.NewRecorder()
			mw(next).ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 with valid session", rr.Code)
			}
		})
	}
}

func TestAuthMiddlewarePublicRoutes(t *testing.T) {
	mw := AuthMiddleware(&settings.Settings{
		AdminPassword: "hunter2",
		SecretKey:     "secret",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, path := range []string{"/login", "/login/", "/api/status", "/api/status/", "/static/app.js", "/static/"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mw(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for public route %s", rr.Code, path)
			}
		})
	}
}
