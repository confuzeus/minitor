package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/confuzeus/minitor/internal/alerter"
	"github.com/confuzeus/minitor/internal/auth"
	"github.com/confuzeus/minitor/internal/database"
	"github.com/confuzeus/minitor/internal/handlers"
	"github.com/confuzeus/minitor/internal/probe"
	"github.com/confuzeus/minitor/internal/settings"
	"github.com/confuzeus/minitor/internal/templates"
	"github.com/go-chi/chi/v5"
)

// version is injected at build time via -ldflags "-X main.version=<ver>".
var version = "dev"

func parseConfig() (cfg settings.Settings, dbPath string, migrateOnly bool) {
	port := flag.String("port", "", "HTTP listen port")
	dataDir := flag.String("data-dir", "", "directory for persistent data")
	dbPathFlag := flag.String("db-path", "", "path to the sqlite database (default <data-dir>/minitor.db)")
	migrateFlag := flag.Bool("migrate", false, "run database migrations and exit")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}

	var err error
	cfg, err = settings.Parse()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	if *port != "" {
		cfg.Port = *port
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	dbPath = *dbPathFlag
	if dbPath == "" {
		dbPath = filepath.Join(cfg.DataDir, "minitor.db")
	}
	return cfg, dbPath, *migrateFlag
}

// newRouter builds the HTTP handler with its public and protected route groups.
// It is shared by main and the integration tests so tests exercise the same
// routing as production.
func newRouter(h *handlers.Handler, cfg *settings.Settings, staticHandler http.Handler) http.Handler {
	router := chi.NewRouter()

	// Public routes: accessible without authentication.
	router.Group(func(r chi.Router) {
		r.Handle("/static/*", staticHandler)
		r.Get("/static", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/static/", http.StatusMovedPermanently)
		})
		r.Get("/login", h.LoginPage)
		r.Post("/login", h.Login)
		r.Post("/logout", h.Logout)
		r.Get("/api/status", h.Status)
	})

	// Protected routes: require a valid session when ADMIN_PASSWORD is set.
	router.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(cfg))

		r.Get("/", h.Dashboard)
		r.Get("/api/monitors", h.MonitorCards)
		r.Get("/api/monitors/{id}/stats", h.MonitorStats)

		r.Get("/monitors", h.ListMonitors)
		r.Get("/monitors/new", h.NewMonitor)
		r.Post("/monitors", h.CreateMonitor)
		r.Get("/monitors/{id}", h.MonitorDetail)
		r.Get("/monitors/{id}/edit", h.EditMonitor)
		r.Put("/monitors/{id}", h.UpdateMonitor)
		r.Delete("/monitors/{id}", h.DeleteMonitor)

		// No-JS fallbacks for htmx forms, which can only issue POST natively.
		r.Post("/monitors/{id}", h.UpdateMonitor)
		r.Post("/monitors/{id}/delete", h.DeleteMonitor)

		r.Get("/alerts", h.ListAlerts)
		r.Post("/alerts", h.CreateAlertRecipient)
		r.Delete("/alerts/{id}", h.DeleteAlertRecipient)
		r.Delete("/alerts/{id}/delete", h.DeleteAlertRecipient)
		r.Post("/alerts/{id}/delete", h.DeleteAlertRecipient)
	})

	return router
}

func main() {
	cfg, dbPath, migrateOnly := parseConfig()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		slog.Error("create data directory", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(dbPath)
	if err != nil {
		slog.Error("initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if migrateOnly {
		slog.Info("migrations complete", "db", dbPath)
		return
	}

	tmpl, err := templates.New(embeddedAssets)
	if err != nil {
		slog.Error("initialize templates", "error", err)
		os.Exit(1)
	}

	staticFS, err := fs.Sub(embeddedAssets, "static/dist")
	if err != nil {
		slog.Error("static sub-filesystem", "error", err)
		os.Exit(1)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticFS))

	sched := probe.NewScheduler(db)
	alert := alerter.New(db, cfg.SMTP)
	sched.SetNotifier(alert.Notify)
	sched.Start()
	defer sched.Stop()

	h := handlers.New(tmpl, db, &cfg, sched)

	router := newRouter(h, &cfg, staticHandler)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("minitor starting", "version", version, "port", cfg.Port, "data_dir", cfg.DataDir, "db", dbPath)

	go func() {
		slog.Info("minitor listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()
	slog.Info("shutdown signal received, stopping gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown", "error", err)
	}
	slog.Info("minitor stopped")
}
