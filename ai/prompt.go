/*
Package ai provides prompt management and template composition for GoStack applications.
*/
package ai

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// DefaultFuncMap provides zero-dependency helper functions for prompt templates.
var DefaultFuncMap = template.FuncMap{
	"trim": strings.TrimSpace,
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"truncate": func(length int, s string) string {
		if len(s) <= length {
			return s
		}
		return s[:length] + "..."
	},
	"indent": func(spaces int, s string) string {
		pad := strings.Repeat(" ", spaces)
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				lines[i] = pad + line
			}
		}
		return strings.Join(lines, "\n")
	},
	"default": func(defVal, val string) string {
		if strings.TrimSpace(val) == "" {
			return defVal
		}
		return val
	},
}

// PromptTemplate represents a reusable, named prompt definition.
type PromptTemplate struct {
	name        string
	version     string
	description string
	raw         string
	tmpl        *template.Template
}

// TemplateOption configures a PromptTemplate during creation.
type TemplateOption func(*PromptTemplate)

// WithVersion sets the version string for the prompt template.
func WithVersion(v string) TemplateOption {
	return func(pt *PromptTemplate) {
		pt.version = v
	}
}

// WithDescription sets the human-readable description for the prompt template.
func WithDescription(d string) TemplateOption {
	return func(pt *PromptTemplate) {
		pt.description = d
	}
}

// WithFuncMap allows injecting custom helper functions into the prompt template.
func WithFuncMap(fm template.FuncMap) TemplateOption {
	return func(pt *PromptTemplate) {
		pt.tmpl.Funcs(fm)
	}
}

// NewPromptTemplate parses and compiles a new named prompt template.
func NewPromptTemplate(name, templateText string, opts ...TemplateOption) (*PromptTemplate, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("ai: prompt template name cannot be empty")
	}

	tmpl := template.New(name).Funcs(DefaultFuncMap)

	pt := &PromptTemplate{
		name:    name,
		version: "v1.0.0",
		raw:     templateText,
		tmpl:    tmpl,
	}

	for _, opt := range opts {
		opt(pt)
	}

	parsed, err := pt.tmpl.Parse(templateText)
	if err != nil {
		return nil, fmt.Errorf("ai: failed to parse prompt template %q: %w", name, err)
	}
	pt.tmpl = parsed

	return pt, nil
}

// MustPromptTemplate constructs a PromptTemplate or panics if compilation fails.
func MustPromptTemplate(name, templateText string, opts ...TemplateOption) *PromptTemplate {
	pt, err := NewPromptTemplate(name, templateText, opts...)
	if err != nil {
		panic(err)
	}
	return pt
}

// Name returns the unique name of the prompt template.
func (pt *PromptTemplate) Name() string {
	return pt.name
}

// Version returns the version tag of the prompt template.
func (pt *PromptTemplate) Version() string {
	return pt.version
}

// Description returns the optional description of the prompt template.
func (pt *PromptTemplate) Description() string {
	return pt.description
}

// Raw returns the uncompiled raw template string.
func (pt *PromptTemplate) Raw() string {
	return pt.raw
}

// Execute renders the template with the provided data payload.
func (pt *PromptTemplate) Execute(data any) (string, error) {
	var buf bytes.Buffer
	if err := pt.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("ai: failed to render prompt %q: %w", pt.name, err)
	}
	return buf.String(), nil
}

// PromptRegistry provides a thread-safe repository of registered prompt templates.
type PromptRegistry struct {
	mu        sync.RWMutex
	templates map[string]*PromptTemplate
}

// NewPromptRegistry creates a fresh prompt template registry.
func NewPromptRegistry() *PromptRegistry {
	return &PromptRegistry{
		templates: make(map[string]*PromptTemplate),
	}
}

// Register stores a prompt template in the registry.
func (r *PromptRegistry) Register(pt *PromptTemplate) error {
	if pt == nil {
		return fmt.Errorf("ai: cannot register nil prompt template")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[pt.name] = pt
	return nil
}

// Get retrieves a prompt template by name.
func (r *PromptRegistry) Get(name string) (*PromptTemplate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pt, ok := r.templates[name]
	return pt, ok
}

// List returns all registered prompt templates.
func (r *PromptRegistry) List() []*PromptTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*PromptTemplate, 0, len(r.templates))
	for _, pt := range r.templates {
		list = append(list, pt)
	}
	return list
}
