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
	tmpl, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("unknown template %q", page)
	}
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		return fmt.Errorf("execute template %q: %w", page, err)
	}
	return nil
}
