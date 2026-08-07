package main

import (
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/confuzeus/minitor/internal/handlers"
	"github.com/confuzeus/minitor/internal/settings"
	"github.com/confuzeus/minitor/internal/templates"
	"github.com/go-chi/chi/v5"
)

func parseConfig() (cfg settings.Settings, dbPath string) {
	var err error
	cfg, err = settings.Parse()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	port := flag.String("port", cfg.Port, "HTTP listen port")
	dataDir := flag.String("data-dir", cfg.DataDir, "directory for persistent data")
	dbPathFlag := flag.String("db-path", "", "path to the sqlite database (default <data-dir>/minitor.db)")
	flag.Parse()

	cfg.Port = *port
	cfg.DataDir = *dataDir
	dbPath = *dbPathFlag
	if dbPath == "" {
		dbPath = filepath.Join(cfg.DataDir, "minitor.db")
	}
	return cfg, dbPath
}

func main() {
	cfg, dbPath := parseConfig()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		slog.Error("create data directory", "error", err)
		os.Exit(1)
	}

	tmpl, err := templates.New(embeddedAssets)
	if err != nil {
		slog.Error("initialize templates", "error", err)
		os.Exit(1)
	}

	router := chi.NewRouter()

	staticFS, err := fs.Sub(embeddedAssets, "static/dist")
	if err != nil {
		slog.Error("static sub-filesystem", "error", err)
		os.Exit(1)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticFS))
	router.Handle("/static/*", staticHandler)
	router.Get("/static", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/", http.StatusMovedPermanently)
	})

	h := handlers.New(tmpl)
	router.Get("/", h.Dashboard)

	slog.Info("minitor starting", "port", cfg.Port, "data_dir", cfg.DataDir, "db", dbPath)
	slog.Info("minitor listening", "addr", ":"+cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
