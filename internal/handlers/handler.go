package handlers

import (
	"database/sql"

	"github.com/confuzeus/minitor/internal/templates"
)

type Handler struct {
	Templates *templates.Templates
	DB        *sql.DB
}

func New(t *templates.Templates, db *sql.DB) *Handler {
	return &Handler{Templates: t, DB: db}
}
