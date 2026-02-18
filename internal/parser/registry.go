package parser

import (
	"path/filepath"

	"github.com/dormstern/segspec/internal/model"
)

// ParseFunc analyzes a file and returns discovered network dependencies.
type ParseFunc func(path string) ([]model.NetworkDependency, error)

type entry struct {
	pattern string
	fn      ParseFunc
}

// Registry maps file glob patterns to parser functions.
type Registry struct {
	entries []entry
}

// NewRegistry creates an empty parser registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a parser for files matching the given glob pattern.
func (r *Registry) Register(pattern string, fn ParseFunc) {
	r.entries = append(r.entries, entry{pattern: pattern, fn: fn})
}

// Match returns all parser functions whose pattern matches the given filename.
func (r *Registry) Match(filename string) []ParseFunc {
	base := filepath.Base(filename)
	var matches []ParseFunc
	for _, e := range r.entries {
		if matched, _ := filepath.Match(e.pattern, base); matched {
			matches = append(matches, e.fn)
		}
	}
	return matches
}

// Patterns returns all registered glob patterns (for diagnostics).
func (r *Registry) Patterns() []string {
	patterns := make([]string, len(r.entries))
	for i, e := range r.entries {
		patterns[i] = e.pattern
	}
	return patterns
}

// defaultRegistry is populated by parser init() functions.
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the registry with all built-in parsers registered.
func DefaultRegistry() *Registry {
	return defaultRegistry
}
