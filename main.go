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
	"github.com/confuzeus/minitor/internal/templates"
	"github.com/go-chi/chi/v5"
)

const defaultPort = "8080"

type config struct {
	port    string
	dataDir string
	dbPath  string
}

func parseConfig() config {
	cfg := config{
		port:    envOr("PORT", defaultPort),
		dataDir: envOr("DATA_DIR", "data"),
	}

	flag.StringVar(&cfg.port, "port", cfg.port, "HTTP listen port")
	flag.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "directory for persistent data")
	flag.StringVar(&cfg.dbPath, "db-path", "", "path to the sqlite database (default <data-dir>/minitor.db)")
	flag.Parse()

	if cfg.dbPath == "" {
		cfg.dbPath = filepath.Join(cfg.dataDir, "minitor.db")
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg := parseConfig()

	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
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

	slog.Info("minitor starting", "port", cfg.port, "data_dir", cfg.dataDir, "db", cfg.dbPath)
	slog.Info("minitor listening", "addr", ":"+cfg.port)
	if err := http.ListenAndServe(":"+cfg.port, router); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
