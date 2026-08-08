package main

import (
	"database/sql"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/smtp"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confuzeus/minitor/internal/alerter"
	"github.com/confuzeus/minitor/internal/auth"
	"github.com/confuzeus/minitor/internal/database"
	"github.com/confuzeus/minitor/internal/handlers"
	"github.com/confuzeus/minitor/internal/models"
	"github.com/confuzeus/minitor/internal/probe"
	"github.com/confuzeus/minitor/internal/settings"
	"github.com/confuzeus/minitor/internal/templates"
)

// testApp wires the application components together the same way main() does.
type testApp struct {
	router http.Handler
	db     *sql.DB
	sched  *probe.Scheduler
	alert  *alerter.Alerter
}

// newTestApp builds a fully wired application: database, templates, scheduler,
// alerter, and the production router. The scheduler starts immediately and is
// stopped at test cleanup.
func newTestApp(t *testing.T, cfg settings.Settings) *testApp {
	t.Helper()

	db, err := database.Open(filepath.Join(cfg.DataDir, "minitor.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tmpl, err := templates.New(embeddedAssets)
	if err != nil {
		t.Fatalf("initialize templates: %v", err)
	}

	staticFS, err := fs.Sub(embeddedAssets, "static/dist")
	if err != nil {
		t.Fatalf("static sub-filesystem: %v", err)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticFS))

	sched := probe.NewScheduler(db)
	sched.MaxJitter = 0
	sched.Start()
	t.Cleanup(sched.Stop)

	alert := alerter.New(db, cfg.SMTP)
	sched.SetNotifier(alert.Notify)

	h := handlers.New(tmpl, db, &cfg, sched)
	router := newRouter(h, &cfg, staticHandler)

	return &testApp{router: router, db: db, sched: sched, alert: alert}
}

// noRedirectClient returns an http.Client that does not follow redirects so
// tests can assert on redirect status codes and Location headers.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func postForm(t *testing.T, client *http.Client, url string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build POST %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// waitForMonitor polls the database until a monitor with the given name exists.
func waitForMonitor(t *testing.T, db *sql.DB, name string) models.Monitor {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		monitors, err := models.ListMonitors(db)
		if err != nil {
			t.Fatalf("list monitors: %v", err)
		}
		for _, m := range monitors {
			if m.Name == name {
				return m
			}
		}
		select {
		case <-deadline:
			t.Fatalf("monitor %q was not created in time", name)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitForProbeStatus polls the database until the monitor's latest probe
// result has the given status. Only the latest result is considered so callers
// cannot return on stale history after a transition.
func waitForProbeStatus(t *testing.T, db *sql.DB, monitorID int64, want string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		results, err := models.GetResultsByMonitorID(db, monitorID, 1, 0)
		if err != nil {
			t.Fatalf("get probe results: %v", err)
		}
		if len(results) > 0 && results[0].Status == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for probe status %q", want)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// TestFullStartupIntegration exercises the full pipeline over HTTP: server
// startup, monitor creation via the API, scheduler probing, result
// persistence, and alert email delivery.
func TestFullStartupIntegration(t *testing.T) {
	var targetStatus atomic.Int32
	targetStatus.Store(http.StatusOK)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(targetStatus.Load()))
	}))
	defer target.Close()

	cfg := settings.Settings{
		Port:          "0",
		DataDir:       t.TempDir(),
		SecretKey:     "integration-secret-key",
		RetentionDays: 30,
		SMTP: settings.SMTPConfig{
			Host: "127.0.0.1",
			Port: "2525",
			From: "minitor@example.com",
		},
	}

	app := newTestApp(t, cfg)

	emails := make(chan []byte, 16)
	app.alert.SetSendMail(func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		emails <- msg
		return nil
	})

	srv := httptest.NewServer(app.router)
	defer srv.Close()

	client := noRedirectClient()

	// Create a monitor via HTTP pointing at the healthy probe target.
	resp := postForm(t, client, srv.URL+"/monitors", url.Values{
		"name":     {"test-monitor"},
		"url":      {target.URL},
		"type":     {"http"},
		"interval": {"1"},
		"timeout":  {"5"},
		"enabled":  {"on"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("create monitor: got status %d, want %d", resp.StatusCode, http.StatusFound)
	}

	monitor := waitForMonitor(t, app.db, "test-monitor")

	// Attach an alert recipient that fires on the first consecutive failure.
	recipient := models.AlertRecipient{Name: "Ops", Email: "ops@example.com"}
	if err := models.CreateRecipientWithAlerts(app.db, &recipient, []models.MonitorAlert{{
		MonitorID:           monitor.ID,
		OnDown:              true,
		OnRecovery:          true,
		ConsecutiveFailures: 1,
	}}); err != nil {
		t.Fatalf("create alert recipient: %v", err)
	}

	// The healthy target produces an "up" probe first.
	waitForProbeStatus(t, app.db, monitor.ID, models.StatusUp)

	// Take the target down; the next probe should be "down" and trigger an
	// alert email.
	targetStatus.Store(http.StatusInternalServerError)
	waitForProbeStatus(t, app.db, monitor.ID, models.StatusDown)

	select {
	case msg := <-emails:
		s := string(msg)
		for _, want := range []string{
			"From: minitor@example.com",
			"To: ops@example.com",
			"Subject: DOWN: test-monitor",
			"Monitor: test-monitor",
			"URL: " + target.URL,
			"Status: DOWN",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("down alert email missing %q:\n%s", want, s)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for down alert email")
	}

	// Bring the target back up; the next probe should be "up" and trigger a
	// recovery email.
	targetStatus.Store(http.StatusOK)
	waitForProbeStatus(t, app.db, monitor.ID, models.StatusUp)

	select {
	case msg := <-emails:
		s := string(msg)
		if !strings.Contains(s, "Subject: RECOVERED: test-monitor") {
			t.Errorf("recovery alert email missing subject:\n%s", s)
		}
		if !strings.Contains(s, "Status: UP") {
			t.Errorf("recovery alert email missing status:\n%s", s)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for recovery alert email")
	}
}

// TestAuthFlowIntegration verifies the full authentication lifecycle: a
// protected page is denied without a session, login succeeds and grants
// access, and logout revokes access.
func TestAuthFlowIntegration(t *testing.T) {
	const password = "correct-horse-battery-staple"

	cfg := settings.Settings{
		Port:          "0",
		DataDir:       t.TempDir(),
		AdminPassword: password,
		SecretKey:     "auth-integration-secret-key",
		RetentionDays: 30,
	}

	app := newTestApp(t, cfg)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	srv := httptest.NewServer(app.router)
	defer srv.Close()

	// Public endpoints are reachable without a session.
	if resp := get(t, client, srv.URL+"/login"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /login: got status %d, want %d", resp.StatusCode, http.StatusOK)
		resp.Body.Close()
	} else {
		resp.Body.Close()
	}
	if resp := get(t, client, srv.URL+"/api/status"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/status: got status %d, want %d", resp.StatusCode, http.StatusOK)
		resp.Body.Close()
	} else {
		resp.Body.Close()
	}

	// A protected page is denied without a session and redirected to /login.
	resp := get(t, client, srv.URL+"/monitors")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /monitors unauthenticated: got status %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("unauthenticated redirect Location = %q, want %q", loc, "/login")
	}
	resp.Body.Close()

	// A wrong password is rejected.
	resp = postForm(t, client, srv.URL+"/login", url.Values{"password": {"wrong-password"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /login with wrong password: got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	resp.Body.Close()

	// The correct password logs in and stores a session cookie.
	resp = postForm(t, client, srv.URL+"/login", url.Values{"password": {password}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("POST /login: got status %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("login redirect Location = %q, want %q", loc, "/")
	}
	if got := resp.Cookies(); len(got) == 0 {
		t.Error("login did not set any cookies")
	}
	resp.Body.Close()

	// The protected page is now accessible, and the session cookie is stored.
	resp = get(t, client, srv.URL+"/monitors")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /monitors authenticated: got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	srvURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	sessionCookies := jar.Cookies(srvURL)
	if len(sessionCookies) != 1 || sessionCookies[0].Name != auth.CookieName {
		t.Errorf("session jar = %v, want a single %q cookie", sessionCookies, auth.CookieName)
	}

	// Logout clears the session and redirects to /login.
	resp = postForm(t, client, srv.URL+"/logout", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("POST /logout: got status %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("logout redirect Location = %q, want %q", loc, "/login")
	}
	resp.Body.Close()

	// The session cookie is gone from the jar after logout.
	if cookies := jar.Cookies(srvURL); len(cookies) != 0 {
		t.Errorf("session jar after logout = %v, want empty", cookies)
	}

	// The protected page is denied again after logout.
	resp = get(t, client, srv.URL+"/monitors")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /monitors after logout: got status %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("post-logout redirect Location = %q, want %q", loc, "/login")
	}
	resp.Body.Close()
}
