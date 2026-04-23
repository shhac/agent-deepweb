// Package template stores and renders parameterised HTTP-request templates.
//
// A template is authored by the human (via `tpl import <file>`) and invoked
// by the LLM (via `tpl run <name> --param k=v`). The LLM fills in parameter
// *values* only; it cannot change the URL, method, or credential binding.
// That makes templates the highest-safety mode of agent-deepweb.
package template

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shhac/agent-deepweb/internal/config"
)

// Template is a parameterised request definition.
type Template struct {
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Method       string               `json:"method"`
	URL          string               `json:"url"`
	Query        map[string]string    `json:"query,omitempty"`
	Headers      map[string]string    `json:"headers,omitempty"`
	Auth         string               `json:"auth,omitempty"`        // credential name
	BodyFormat   string               `json:"body_format,omitempty"` // "json" (default) | "form" | "raw"
	BodyTemplate json.RawMessage      `json:"body_template,omitempty"`
	Parameters   map[string]ParamSpec `json:"parameters,omitempty"`
}

// ParamSpec describes a single parameter. Types:
//
//	string | int | number | bool | string-array
//
// Enum (if set) further restricts to a closed set of values.
type ParamSpec struct {
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
}

type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("template %q not found", e.Name) }

func templatesPath() string {
	return filepath.Join(config.ConfigDir(), "templates.json")
}

func readIndex() (map[string]Template, error) {
	data, err := os.ReadFile(templatesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Template{}, nil
		}
		return nil, err
	}
	var m map[string]Template
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]Template{}
	}
	return m, nil
}

func writeIndex(m map[string]Template) error {
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(templatesPath(), append(data, '\n'), 0o644)
}

// Get loads a template by name.
func Get(name string) (*Template, error) {
	idx, err := readIndex()
	if err != nil {
		return nil, err
	}
	t, ok := idx[name]
	if !ok {
		return nil, &NotFoundError{Name: name}
	}
	t.Name = name
	return &t, nil
}

// List returns all templates sorted by name.
func List() ([]Template, error) {
	idx, err := readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(idx))
	for n, t := range idx {
		t.Name = n
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Store writes a template. If a template with the same name exists, it's
// replaced (caller decides intent).
func Store(t Template) error {
	if t.Name == "" {
		return fmt.Errorf("template name required")
	}
	idx, err := readIndex()
	if err != nil {
		return err
	}
	idx[t.Name] = t
	return writeIndex(idx)
}

// Remove deletes a template by name.
func Remove(name string) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	if _, ok := idx[name]; !ok {
		return &NotFoundError{Name: name}
	}
	delete(idx, name)
	return writeIndex(idx)
}

// ImportFile reads a JSON file and stores each template.
//
// Accepts two shapes:
//  1. A single template object: { "name":"...", ... }
//  2. A map of name → template: { "foo.get": { ... }, "bar.list": { ... } }
func ImportFile(path string) (stored []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Try map form first, since a template's JSON-object shape could also
	// be a single template — we disambiguate by looking for "method".
	var peek map[string]json.RawMessage
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, looksLikeSingle := peek["method"]; looksLikeSingle {
		var t Template
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse template: %w", err)
		}
		if t.Name == "" {
			return nil, fmt.Errorf("single-template import requires 'name' field")
		}
		if err := Store(t); err != nil {
			return nil, err
		}
		return []string{t.Name}, nil
	}
	// Map form
	var m map[string]Template
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	for name, t := range m {
		t.Name = name
		if err := Store(t); err != nil {
			return nil, err
		}
		stored = append(stored, name)
	}
	sort.Strings(stored)
	return stored, nil
}
