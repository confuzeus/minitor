package main

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/confuzeus/minitor/internal/handlers"
	"github.com/confuzeus/minitor/internal/templates"
	"github.com/go-chi/chi/v5"
)

const defaultPort = "8080"

func main() {
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

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	slog.Info("minitor listening", "addr", ":"+port)
	if err := http.ListenAndServe(":"+port, router); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
