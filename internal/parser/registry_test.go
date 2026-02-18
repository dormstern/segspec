package parser

import (
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func dummyParser(deps ...model.NetworkDependency) ParseFunc {
	return func(path string) ([]model.NetworkDependency, error) {
		return deps, nil
	}
}

func TestRegistryMatch(t *testing.T) {
	r := NewRegistry()
	r.Register("application.yml", dummyParser())
	r.Register("docker-compose.yml", dummyParser())
	r.Register("*.properties", dummyParser())

	tests := []struct {
		filename string
		want     int
	}{
		{"application.yml", 1},
		{"docker-compose.yml", 1},
		{"application.properties", 1},
		{"unknown.txt", 0},
		{"application.yaml", 0}, // not registered
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := r.Match(tt.filename)
			if len(got) != tt.want {
				t.Errorf("Match(%q) returned %d parsers, want %d", tt.filename, len(got), tt.want)
			}
		})
	}
}

func TestRegistryMatchMultiple(t *testing.T) {
	r := NewRegistry()
	r.Register("application.yml", dummyParser())
	r.Register("*.yml", dummyParser())

	got := r.Match("application.yml")
	if len(got) != 2 {
		t.Errorf("Match returned %d parsers, want 2 (both should match)", len(got))
	}
}

func TestRegistryPatterns(t *testing.T) {
	r := NewRegistry()
	r.Register("application.yml", dummyParser())
	r.Register("*.env", dummyParser())

	patterns := r.Patterns()
	if len(patterns) != 2 {
		t.Errorf("Patterns() returned %d, want 2", len(patterns))
	}
}

func TestEmptyRegistryMatch(t *testing.T) {
	r := NewRegistry()
	got := r.Match("anything.yml")
	if len(got) != 0 {
		t.Errorf("empty registry matched %d parsers, want 0", len(got))
	}
}
