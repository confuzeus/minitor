package handlers

import (
	"database/sql"

	"github.com/confuzeus/minitor/internal/probe"
	"github.com/confuzeus/minitor/internal/settings"
	"github.com/confuzeus/minitor/internal/templates"
)

type Handler struct {
	Templates *templates.Templates
	DB        *sql.DB
	Settings  *settings.Settings
	Scheduler *probe.Scheduler
}

func New(t *templates.Templates, db *sql.DB, s *settings.Settings, sched *probe.Scheduler) *Handler {
	return &Handler{Templates: t, DB: db, Settings: s, Scheduler: sched}
}
