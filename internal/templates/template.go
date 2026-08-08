package templates

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"
)

const (
	templateDir = "internal/templates"
	baseFile    = "base.html"
)

type Templates struct {
	pages map[string]*template.Template
}

func New(assets fs.FS) (*Templates, error) {
	dir, err := fs.Sub(assets, templateDir)
	if err != nil {
		return nil, fmt.Errorf("template sub-filesystem: %w", err)
	}

	entries, err := fs.ReadDir(dir, ".")
	if err != nil {
		return nil, fmt.Errorf("read template dir: %w", err)
	}

	pages := make(map[string]*template.Template)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".html") || name == baseFile {
			continue
		}

		tmpl, err := template.New(name).ParseFS(dir, baseFile, name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		pages[strings.TrimSuffix(name, ".html")] = tmpl
	}

	return &Templates{pages: pages}, nil
}

func (t *Templates) Render(w io.Writer, page string, data any) error {
	return t.ExecuteTemplate(w, page, "base", data)
}

// ExecuteTemplate renders a named template within a page template, bypassing
// the base layout. It is used to return bare HTML fragments (e.g. for HTMX
// partial updates).
func (t *Templates) ExecuteTemplate(w io.Writer, page, name string, data any) error {
	tmpl, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("unknown template %q", page)
	}
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute template %q in %q: %w", name, page, err)
	}
	return nil
}
