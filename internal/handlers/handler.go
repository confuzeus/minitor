package handlers

import (
	"github.com/confuzeus/minitor/internal/templates"
)

type Handler struct {
	Templates *templates.Templates
}

func New(t *templates.Templates) *Handler {
	return &Handler{Templates: t}
}
