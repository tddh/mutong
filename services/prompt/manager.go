package prompt

import (
	"path/filepath"
	"strings"
	"text/template"
)

type Manager struct {
	templates map[string]*template.Template
}

func NewManager(dir string) (*Manager, error) {
	m := &Manager{templates: make(map[string]*template.Template)}
	files, _ := filepath.Glob(filepath.Join(dir, "diagnosis_*.md"))
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "diagnosis_"), ".md")
		tmpl, err := template.ParseFiles(f)
		if err != nil {
			continue
		}
		m.templates[name] = tmpl
	}
	if tmpl, err := template.ParseFiles(filepath.Join(dir, "postmortem.md")); err == nil {
		m.templates["postmortem"] = tmpl
	}
	return m, nil
}

func (m *Manager) Render(resourceType string) string {
	tmpl := m.templates[strings.ToLower(resourceType)]
	if tmpl == nil {
		tmpl = m.templates["default"]
	}
	if tmpl == nil {
		return ""
	}
	var buf strings.Builder
	_ = tmpl.Execute(&buf, nil)
	return buf.String()
}

func (m *Manager) RenderPostmortem() string {
	tmpl := m.templates["postmortem"]
	if tmpl == nil {
		return ""
	}
	var buf strings.Builder
	_ = tmpl.Execute(&buf, nil)
	return buf.String()
}
